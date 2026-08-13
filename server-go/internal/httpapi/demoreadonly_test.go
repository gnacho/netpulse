package httpapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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

// demoReadOnly (issue #118): en modo demo las mutaciones fuera de la allowlist
// se rechazan con 409 demo_read_only; la allowlist (login, demo/enable,
// refresh, push, users/me) sigue funcionando.
func TestDemoReadOnly(t *testing.T) {
	ts := makeDemoTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test1234")
	if cookie == "" {
		t.Fatal("login no devolvió cookie")
	}

	do := func(method, path, body string) (int, string) {
		t.Helper()
		var req *http.Request
		if body != "" {
			req, _ = http.NewRequest(method, ts.URL+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req, _ = http.NewRequest(method, ts.URL+path, nil)
		}
		req.Header.Set("Cookie", "session="+cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer res.Body.Close()
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, res.Body)
		return res.StatusCode, buf.String()
	}

	// Mutación bloqueada: crear un override de topología en demo
	status, bodyStr := do("POST", "/api/topology/overrides", `{"mac":"aa:bb:cc:dd:ee:ff","kind":"hypervisor"}`)
	if status != http.StatusConflict {
		t.Errorf("POST override en demo: status %d, esperado 409", status)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(bodyStr), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "demo_read_only" {
		t.Errorf("error=%q, esperado demo_read_only", body.Error)
	}

	// Mutación bloqueada: añadir router
	if status, _ := do("POST", "/api/config/routers", `{"host":"10.0.0.9","name":"r9"}`); status != http.StatusConflict {
		t.Errorf("POST router en demo: status %d, esperado 409", status)
	}

	// Mutación bloqueada: plan de orquestación
	if status, _ := do("POST", "/api/plans", `{"router_id":"r1","resource":"x","desired":"y"}`); status != http.StatusConflict {
		t.Errorf("POST plan en demo: status %d, esperado 409", status)
	}

	// Allowlist: refresh (no escribe) debe funcionar
	if status, _ := do("POST", "/api/refresh", ""); status != http.StatusNoContent && status != http.StatusAccepted && status != http.StatusTooManyRequests {
		t.Errorf("POST refresh en demo: status %d, no esperado", status)
	}

	// Allowlist: preferencia de idioma (users/me) debe funcionar
	if status, _ := do("PUT", "/api/users/me/language", `{"language":"es"}`); status != http.StatusOK && status != http.StatusNoContent {
		t.Errorf("PUT language en demo: status %d, esperado 2xx", status)
	}

	// GET sigue funcionando (solo lectura)
	if status, _ := do("GET", "/api/topology/overrides", ""); status != http.StatusOK {
		t.Errorf("GET overrides en demo: status %d, esperado 200", status)
	}
}

// makeDemoAgentTestServer: server en modo demo CON registry de agentes, para
// probar que la ingesta externa en demo es no-op benigna (issue #168).
func makeDemoAgentTestServer(t *testing.T) *agentTestServer {
	t.Helper()
	auth.SetTrustProxy(true)
	t.Cleanup(func() { auth.SetTrustProxy(false) })
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test1234",
		"DEMO_MODE": "1", "DATA_DIR": dataDir, "NODE_ENV": "test",
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
	reg := adapters.NewAgentRegistry(90 * time.Second)
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Hub: hub, Secret: secret,
		Agents: reg, Started: time.Now(),
	})
	srv := httptest.NewServer(handler)
	status, cookie, _ := loginCookie(t, srv.URL, "admin", "test1234")
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

// TestIngestAgentNoOpEnDemo (issue #168): en modo demo la ingesta externa de
// scrapers/colectores se acepta (202 + demo:true) como no-op, sin tocar el
// registry ni la BD. La validación (token + HMAC + anti-replay) se mantiene.
func TestIngestAgentNoOpEnDemo(t *testing.T) {
	ts := makeDemoAgentTestServer(t)
	// En demo no se pueden crear tokens por API (409 demo_read_only), igual
	// que en producción: el scraper ya tenía su token ANTES de entrar en demo.
	// Lo insertamos directo en kv (sha256), simulando ese flujo real.
	token := "feedfacefeedfacefeedfacefeedfacefeedfacefeedfacefeedfacefeedface"
	sum := sha256.Sum256([]byte(token))
	if _, err := ts.db.Exec("INSERT INTO kv (key, value) VALUES ('agent.token.patio', ?)",
		hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("sembrar token: %v", err)
	}

	// Ingesta válida en demo → 202 con demo:true (no-op benigno)
	res := ingest(t, ts, token, "10.0.0.1", validPayload())
	body := readJSON(t, res)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest demo: status %d, esperado 202 (body=%v)", res.StatusCode, body)
	}
	if body["ok"] != true || body["demo"] != true {
		t.Fatalf("body esperado {ok:true, demo:true}, got %v", body)
	}

	// No-op: el registry NO debe haber ingerido el payload
	if _, _, ok := ts.agents.Info("patio"); ok {
		t.Fatal("en demo el registry NO debe actualizarse")
	}
	// No-op: tampoco debe persistirse el estado del agente en kv
	var stored string
	if err := ts.db.QueryRow("SELECT value FROM kv WHERE key = 'agent.state.patio'").Scan(&stored); err == nil {
		t.Fatalf("no debería persistirse estado de agente en demo (kv=%q)", stored)
	}

	// La validación sigue viva: token inválido → 401 (no un falso 202)
	res = ingest(t, ts, "deadbeef", "10.0.0.2", validPayload())
	body = readJSON(t, res)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token inválido en demo: status %d, esperado 401", res.StatusCode)
	}
	if body["error"] != "unauthorized" {
		t.Fatalf("error=%v, esperado unauthorized", body["error"])
	}
	// Firma HMAC incorrecta (token correcto, sig errónea) → 401 invalid_signature
	payload := validPayload()
	req, _ := http.NewRequest("POST", ts.URL+"/api/ingest/agent", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "10.0.0.3")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Agent-Signature", strings.Repeat("0", 64))
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ingest sig: %v", err)
	}
	body = readJSON(t, res2)
	if res2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("HMAC inválido en demo: status %d, esperado 401", res2.StatusCode)
	}
	if body["error"] != "invalid_signature" {
		t.Fatalf("error=%v, esperado invalid_signature", body["error"])
	}
}
