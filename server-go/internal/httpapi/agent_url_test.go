// agent_url_test.go — #1335: NETPULSE_AGENT_URL como URL base preferente con
// la que los agentes alcanzan al server. Cadena: AGENT_URL > PUBLIC_URL >
// Host de la petición. Antes de #1335 el install line ignoraba ambas
// variables, así que tras un reverse proxy con dominio público el comando
// copiado apuntaba al dominio en vez de a la LAN.
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// makeURLTestServer levanta el handler real con un env extra (las variables
// NETPULSE_* del caso) para observar cómo queda el install line.
func makeURLTestServer(t *testing.T, extraEnv map[string]string) *agentTestServer {
	t.Helper()
	auth.SetTrustProxy(true)
	t.Cleanup(func() { auth.SetTrustProxy(false) })
	dataDir := t.TempDir()
	env := map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
	}
	for k, v := range extraEnv {
		env[k] = v
	}
	cfg, err := config.Load(env, dataDir)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatalf("users: %v", err)
	}
	reg := adapters.NewAgentRegistry(90 * time.Second)
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	agentHub := sse.NewAgentHub(func(_, _ string) bool { return false })
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Hub: hub, Secret: secret,
		Agents: reg, Started: time.Now(), AgentHub: agentHub,
	})
	srv := httptest.NewServer(handler)
	status, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if status != 204 {
		t.Fatalf("login: %d", status)
	}
	ts := &agentTestServer{Server: srv, db: d, agents: reg, cookie: cookie}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts
}

func TestAgentInstallLinePrefiereAgentURL(t *testing.T) {
	ts := makeURLTestServer(t, map[string]string{
		"NETPULSE_AGENT_URL":  "http://192.168.1.50:9999",
		"NETPULSE_PUBLIC_URL": "https://netpulse.example.com",
	})
	status, _, install := createAgentToken(t, ts, "rt1")
	if status != http.StatusCreated {
		t.Fatalf("POST /api/agents: %d", status)
	}
	if !strings.Contains(install, "--server=http://192.168.1.50:9999") {
		t.Fatalf("install line debe usar AGENT_URL: %s", install)
	}
	if strings.Contains(install, "netpulse.example.com") {
		t.Fatalf("install line no debe usar PUBLIC_URL cuando hay AGENT_URL: %s", install)
	}
}

func TestAgentInstallLinePrefierePublicURL(t *testing.T) {
	ts := makeURLTestServer(t, map[string]string{
		"NETPULSE_PUBLIC_URL": "https://netpulse.example.com",
	})
	_, _, install := createAgentToken(t, ts, "rt1")
	if !strings.Contains(install, "--server=https://netpulse.example.com") {
		t.Fatalf("install line debe usar PUBLIC_URL: %s", install)
	}
	if strings.Contains(install, "--server="+ts.URL) {
		t.Fatalf("install line no debe caer al Host de la petición cuando hay PUBLIC_URL: %s", install)
	}
}

func TestAgentInstallLineFallbackHostPeticion(t *testing.T) {
	ts := makeURLTestServer(t, nil)
	_, _, install := createAgentToken(t, ts, "rt1")
	if !strings.Contains(install, "--server="+ts.URL) {
		t.Fatalf("install line debe caer al Host de la petición: %s", install)
	}
}
