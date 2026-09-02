// orchestr_apply_test.go — tests del endpoint /api/orchestr/apply (#451).
package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

func TestOrchestrApplyCreatesAndAppliesPlan(t *testing.T) {
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
	_, err = ts.db.Exec("UPDATE routers SET id = ? WHERE host = ?", "flint2", host)
	if err != nil {
		t.Fatalf("update router id: %v", err)
	}

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

	ops := []executor.Op{{
		Kind: "uci_set",
		Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "1.1.1.1"},
		Desc: "set dns",
	}}
	body, _ := json.Marshal(map[string]any{
		"routerId": "flint2",
		"resource": "guestwifi",
		"diff":     ops,
	})

	req, _ := http.NewRequest("POST", ts.URL+"/api/orchestr/apply", bytes.NewReader(body))
	req.Header.Set("Cookie", "session="+cookie)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("apply: esperaba 202, got %d: %s", res.StatusCode, string(b))
	}

	// Verificar que el plan se creó y que NetGrip recibió ownership.
	if len(receivedOps) < 3 {
		t.Fatalf("NetGrip debería recibir al menos 3 ops, got %d", len(receivedOps))
	}
	if receivedOps[0].Kind != "ownership_mode" || receivedOps[0].Args["enforce"] != "true" {
		t.Errorf("op[0] debería ser ownership_mode enforce=true, got %+v", receivedOps[0])
	}
	if receivedOps[1].Kind != "uci_set_managed" {
		t.Errorf("op[1] debería ser uci_set_managed, got %+v", receivedOps[1])
	}

	var planID string
	if err := ts.db.QueryRow("SELECT id FROM orchestr_plans ORDER BY created_at DESC LIMIT 1").Scan(&planID); err != nil {
		t.Fatalf("select plan: %v", err)
	}
	var status string
	if err := ts.db.QueryRow("SELECT status FROM orchestr_plans WHERE id = ?", planID).Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != "applied" {
		t.Fatalf("plan status: esperaba applied, got %s", status)
	}
}
