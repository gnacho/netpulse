// mcp_gate_test.go - /mcp como integración (#1114): el toggle
// settings.integrations.mcp (default activo cuando el endpoint está montado
// vía NETPULSE_MCP_ENABLED=1) apaga el endpoint con 404 sin reiniciar.
package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/apitoken"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/channelplan"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/configbackup"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/mcp"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// makeMCPTestServer levanta la app real con NETPULSE_MCP_ENABLED=1 y el
// servidor MCP montado (poller sin snapshot: los tools dan isError, el gate
// no depende de ello).
func makeMCPTestServer(t *testing.T) *testServer {
	t.Helper()
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
		"NETPULSE_MCP_ENABLED": "1",
	}, dataDir)
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
	if err := apitoken.EnsureSchema(d); err != nil {
		t.Fatalf("api_tokens schema: %v", err)
	}
	tokenStore := apitoken.NewStore(d, secret)
	configBackup, err := configbackup.NewStore(d)
	if err != nil {
		t.Fatalf("config backup store: %v", err)
	}
	mcpSrv := mcp.New(mcp.Deps{LastOverview: func() *adapters.Overview { return nil }, Version: "test"})
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Hub: sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil }),
		Secret: secret, Started: time.Now(), TokenStore: tokenStore,
		ConfigBackup: configBackup, Orchestr: orchestr.New(d), Agents: adapters.NewAgentRegistry(0),
		ChannelPlan: channelplan.NewStore(d.DB), MCP: mcpSrv,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(func() { srv.Close(); d.Close() })
	return &testServer{Server: srv, db: d, secret: secret}
}

func mcpPost(t *testing.T, url, cookie, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("POST", url+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer res.Body.Close()
	buf := new(strings.Builder)
	_, _ = io.Copy(buf, res.Body)
	return res.StatusCode, buf.String()
}

func TestMCPGateDefaultOn(t *testing.T) {
	srv := makeMCPTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// View: mcp activo (montado + default on) y no bloqueado.
	res, body := wanSpeedRequest(t, "GET", srv.URL, "/api/settings/integrations", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET integrations: status %d", res.StatusCode)
	}
	if body["mcp"] != true || body["mcpLocked"] != false {
		t.Fatalf("view mcp: %v / locked %v, esperado true/false", body["mcp"], body["mcpLocked"])
	}

	// /mcp responde (401 sin token Bearer: la auth propia sigue aplicando).
	status, _ := mcpPost(t, srv.URL, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("/mcp sin token: status %d, esperado 401 (endpoint vivo)", status)
	}
}

func TestMCPGateToggleOffHidesEndpoint(t *testing.T) {
	srv := makeMCPTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// Apagar la integración: sin reinicio el endpoint desaparece (404).
	res, body := wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/integrations", cookie, `{"mcp":false}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT mcp=false: status %d (%v)", res.StatusCode, body)
	}
	if body["mcp"] != false {
		t.Fatalf("echo mcp: %v, esperado false", body["mcp"])
	}
	status, _ := mcpPost(t, srv.URL, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if status != http.StatusNotFound {
		t.Fatalf("/mcp con integración off: status %d, esperado 404", status)
	}

	// Reactivar: vuelve a responder (401) sin reiniciar.
	res, body = wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/integrations", cookie, `{"mcp":true}`)
	if res.StatusCode != http.StatusOK || body["mcp"] != true {
		t.Fatalf("PUT mcp=true: status %d (%v)", res.StatusCode, body)
	}
	status, _ = mcpPost(t, srv.URL, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("/mcp reactivado: status %d, esperado 401", status)
	}
}

func TestMCPLockedWithoutEnv(t *testing.T) {
	// Sin NETPULSE_MCP_ENABLED el endpoint no se monta: la view lo reporta
	// bloqueado y /mcp es 404 aunque el toggle kv diga otra cosa.
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	res, body := wanSpeedRequest(t, "GET", srv.URL, "/api/settings/integrations", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET integrations: status %d", res.StatusCode)
	}
	if body["mcp"] != false || body["mcpLocked"] != true {
		t.Fatalf("view sin env: mcp %v / locked %v, esperado false/true", body["mcp"], body["mcpLocked"])
	}
	status, _ := mcpPost(t, srv.URL, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if status != http.StatusNotFound {
		t.Fatalf("/mcp sin env: status %d, esperado 404 (no montado)", status)
	}
}
