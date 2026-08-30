// orchestr_netgrip_test.go — tests de delegación de apply a NetGrip (#339).
package httpapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

func TestApplyDelegatesToNetGrip(t *testing.T) {
	ts := makeTestServer(t)

	var receivedOps []executor.Op
	netgripMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/executor/apply" {
			http.NotFound(w, r)
			return
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer netgrip-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			Ops []executor.Op `json:"ops"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		receivedOps = body.Ops
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer netgripMock.Close()

	// Insertar router apuntando al mock de NetGrip (host:puerto).
	host := netgripMock.Listener.Addr().String()
	_, err := routerstore.AddRouter(ts.db.DB, routerstore.AddInput{
		Name:      "Flint 2",
		Host:      host,
		Type:      "openwrt",
		IsGateway: true,
	})
	if err != nil {
		t.Fatalf("add router: %v", err)
	}
	// Forzar ID conocido para el plan.
	_, err = ts.db.Exec("UPDATE routers SET id = ? WHERE host = ?", "flint2", host)
	if err != nil {
		t.Fatalf("update router id: %v", err)
	}

	// Guardar token de agente y executor token de NetGrip.
	sum := sha256.Sum256([]byte("agent-token"))
	_, err = ts.db.Exec("INSERT INTO kv (key, value) VALUES (?, ?)", "agent.token.flint2", hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatalf("insert agent token: %v", err)
	}
	_, err = ts.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?)",
		"netgrip.executor_token.flint2", "netgrip-secret")
	if err != nil {
		t.Fatalf("insert executor token: %v", err)
	}

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	// Crear plan directamente (evita sondeo SSH).
	planID := "plan-" + hex.EncodeToString(sum[:])[:8]
	ops := []executor.Op{{Kind: "uci_set", Args: map[string]string{"key": "guestwifi.guest.enabled"}, Desc: "enable guest"}}
	opsJSON, _ := json.Marshal(ops)
	_, err = ts.db.Exec(
		`INSERT INTO orchestr_plans (id, router_id, resource, desired, diff, status, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, 'pending', 'admin', ?)`,
		planID, "flint2", "guestwifi", `{}`, string(opsJSON), time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	// Aplicar plan.
	req2, _ := http.NewRequest("POST", ts.URL+"/api/plans/"+planID+"/apply", nil)
	req2.Header.Set("Cookie", "session="+cookie)
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(res2.Body)
		t.Fatalf("apply: %d %s", res2.StatusCode, string(b))
	}

	if len(receivedOps) != 1 || receivedOps[0].Kind != "uci_set" {
		t.Fatalf("NetGrip no recibió la op esperada: %+v", receivedOps)
	}

	var status string
	if err := ts.db.QueryRow("SELECT status FROM orchestr_plans WHERE id = ?", planID).Scan(&status); err != nil {
		t.Fatalf("select plan status: %v", err)
	}
	if status != "applied" {
		t.Fatalf("plan status: esperaba applied, got %s", status)
	}
}
