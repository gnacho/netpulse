// orchestr_netgrip_rollback_test.go — regresión #397: el rollback delega al
// executor de NetGrip cuando el router tiene executor token, y el plan queda
// rolled_back (SetRollingBack + SetResult).
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/apitoken"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/configbackup"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// fakeProbeSSH responde al probe de guestwifi con secciones vacías (sin guest
// configurado) y registra las llamadas.
type fakeProbeSSH struct{}

func (fakeProbeSSH) Run(host, cmd string, _ time.Duration) (string, error) {
	if strings.Contains(cmd, "===WIRELESS===") {
		return "===WIRELESS===\n===NETWORK===\n===FIREWALL===\n===END===", nil
	}
	return "", nil
}

func TestRollbackDelegatesToNetGrip(t *testing.T) {
	// Server a medida: igual que makeTestServer pero con Pool (fakeProbeSSH)
	// para que computeModuleDiff pueda sondear el escenario sin SSH real.
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
	}, dataDir)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatalf("users: %v", err)
	}
	if err := apitoken.EnsureSchema(d); err != nil {
		t.Fatalf("api_tokens schema: %v", err)
	}
	configBackup, err := configbackup.NewStore(d)
	if err != nil {
		t.Fatalf("config backup store: %v", err)
	}
	reg := adapters.NewAgentRegistry(90 * time.Second)
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Hub: hub, Secret: secret,
		Agents: reg, Pool: fakeProbeSSH{}, Started: time.Now(),
		TokenStore:   apitoken.NewStore(d, secret),
		ConfigBackup: configBackup,
		Orchestr:     orchestr.New(d),
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// Mock de NetGrip: registra las ops recibidas.
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

	// Router gateway apuntando al mock + executor token en kv.
	host := netgripMock.Listener.Addr().String()
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "Flint 2", Host: host, Type: "openwrt", IsGateway: true,
	}); err != nil {
		t.Fatalf("add router: %v", err)
	}
	if _, err := d.Exec("UPDATE routers SET id = ? WHERE host = ?", "flint2", host); err != nil {
		t.Fatalf("update router id: %v", err)
	}
	if _, err := d.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?)",
		"netgrip.executor_token.flint2", "netgrip-secret"); err != nil {
		t.Fatalf("insert executor token: %v", err)
	}

	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// Plan guestwifi YA APLICADO con enabled=true (el rollback lo invierte).
	planID := "plan-roll-01"
	opsJSON, _ := json.Marshal([]executor.Op{{Kind: "uci_set", Args: map[string]string{"key": "guestwifi.guest.enabled"}, Desc: "enable guest"}})
	if _, err := d.Exec(
		`INSERT INTO orchestr_plans (id, router_id, resource, desired, diff, status, created_by, created_at, applied_at, result)
		 VALUES (?, ?, 'guestwifi', ?, ?, 'applied', 'admin', ?, ?, '{"status":"applied"}')`,
		planID, "flint2", `{"enabled":true,"allowNonGateway":false}`, string(opsJSON),
		time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	// Rollback.
	req, _ := http.NewRequest("POST", srv.URL+"/api/plans/"+planID+"/rollback", nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("rollback: esperaba 202, got %d: %s", res.StatusCode, string(b))
	}

	if len(receivedOps) == 0 {
		t.Fatal("NetGrip no recibió las ops inversas del rollback")
	}
	var status string
	if err := d.QueryRow("SELECT status FROM orchestr_plans WHERE id = ?", planID).Scan(&status); err != nil {
		t.Fatalf("select plan status: %v", err)
	}
	if status != "rolled_back" {
		t.Fatalf("plan status: esperaba rolled_back, got %s", status)
	}
}
