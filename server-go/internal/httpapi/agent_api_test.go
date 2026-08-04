// agent_api_test.go — Fase 3 piloto (SPEC-AGENTE-PILOTO §1): ingesta con
// token válido/inválido, body grande, rate limit, payload inválido (no rompe
// el pipeline), y gestión de tokens (crear/listar/revocar; el token solo se
// muestra una vez y NUNCA sale por GET).
package httpapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

type agentTestServer struct {
	*httptest.Server
	db     *db.DB
	agents *adapters.AgentRegistry
	cookie string
}

// makeAgentTestServer: app real con registry de agentes + sesión admin.
func makeAgentTestServer(t *testing.T) *agentTestServer {
	t.Helper()
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

// createAgentToken crea el token de un slug vía API (con sesión) y devuelve
// (status, token, install).
func createAgentToken(t *testing.T, ts *agentTestServer, slug string) (int, string, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/agents", strings.NewReader(fmt.Sprintf(`{"slug":%q}`, slug)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+ts.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/agents: %v", err)
	}
	defer res.Body.Close()
	var body struct {
		Slug    string `json:"slug"`
		Token   string `json:"token"`
		Install string `json:"install"`
	}
	data, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(data, &body)
	return res.StatusCode, body.Token, body.Install
}

// ingest hace un push de agente; ip fija la X-Forwarded-For (rate limit).
func ingest(t *testing.T, ts *agentTestServer, token, ip, payload string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/ingest/agent", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	return res
}

const validPayload = `{"router":"patio","ts":1754140000,"version":"0.1.0","data":{"system":{"sysinfo":{"uptime":100,"load":[0,0,0],"memory":{"total":100,"free":50,"buffered":0,"available":50}},"cpu":10,"temp":40},"wireless":{"clients":{}},"dhcp":{"leases":[]},"fdb":{"macs":{}}}}`

func TestIngestTokenValidoEInvalido(t *testing.T) {
	ts := makeAgentTestServer(t)
	status, token, install := createAgentToken(t, ts, "patio")
	if status != 201 || token == "" {
		t.Fatalf("create: %d token=%q", status, token)
	}
	if !strings.Contains(install, "install-agent.sh") || !strings.Contains(install, token) || !strings.Contains(install, "--slug patio") {
		t.Fatalf("one-liner: %q", install)
	}
	// kv guarda sha256, NUNCA el token en claro
	var stored string
	if err := ts.db.QueryRow("SELECT value FROM kv WHERE key = 'agent.token.patio'").Scan(&stored); err != nil {
		t.Fatalf("kv: %v", err)
	}
	sum := sha256.Sum256([]byte(token))
	if stored != hex.EncodeToString(sum[:]) || strings.Contains(stored, token) {
		t.Fatalf("kv debería guardar sha256: %q", stored)
	}
	// Ingesta sin sesión (exenta del middleware) + token válido → 202
	res := ingest(t, ts, token, "10.0.0.1", validPayload)
	res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatalf("ingest válido: %d", res.StatusCode)
	}
	if _, version, ok := ts.agents.Info("patio"); !ok || version != "0.1.0" {
		t.Fatalf("registry no actualizado: %v %q", ok, version)
	}
	// Token inválido → 401; sin token → 401; token de otro slug → 401
	for _, tok := range []string{"", "deadbeef", token + "00"} {
		res := ingest(t, ts, tok, "10.0.0.2", validPayload)
		res.Body.Close()
		if res.StatusCode != 401 {
			t.Fatalf("token %q debería dar 401, dio %d", tok, res.StatusCode)
		}
	}
}

func TestIngestBodyGrandeYPayloadInvalido(t *testing.T) {
	ts := makeAgentTestServer(t)
	_, token, _ := createAgentToken(t, ts, "patio")

	// 413: body > 2 MB
	big := strings.Repeat("x", (2<<20)+100)
	res := ingest(t, ts, token, "10.0.1.1", big)
	res.Body.Close()
	if res.StatusCode != 413 {
		t.Fatalf("body grande: %d", res.StatusCode)
	}
	// 400: JSON roto, slug inválido, ts ausente — y NADA se rompe después
	for _, p := range []string{
		`{no json`,
		`{"router":"PATIO!","ts":1,"data":{}}`,
		`{"router":"patio","data":{}}`,
		`{"router":"patio","ts":-3,"data":{}}`,
	} {
		res := ingest(t, ts, token, "10.0.1.2", p)
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("payload %q debería dar 400, dio %d", p, res.StatusCode)
		}
	}
	// El pipeline sigue sano tras los 400
	res = ingest(t, ts, token, "10.0.1.2", validPayload)
	res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatalf("ingest tras 400s: %d", res.StatusCode)
	}
}

func TestIngestRateLimit(t *testing.T) {
	ts := makeAgentTestServer(t)
	_, token, _ := createAgentToken(t, ts, "patio")
	// 30/min por IP: las 30 primeras pasan, la 31 es 429 con Retry-After
	for i := 0; i < 30; i++ {
		res := ingest(t, ts, token, "10.0.2.1", validPayload)
		res.Body.Close()
		if res.StatusCode != 202 {
			t.Fatalf("push %d: %d", i, res.StatusCode)
		}
	}
	res := ingest(t, ts, token, "10.0.2.1", validPayload)
	defer res.Body.Close()
	if res.StatusCode != 429 {
		t.Fatalf("push 31 debería ser 429, dio %d", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("429 json: %v", err)
	}
	if body["error"] != "rate_limited" || res.Header.Get("Retry-After") == "" {
		t.Fatalf("429 shape: %v retry=%q", body, res.Header.Get("Retry-After"))
	}
	// Otra IP no está limitada
	res2 := ingest(t, ts, token, "10.0.2.2", validPayload)
	res2.Body.Close()
	if res2.StatusCode != 202 {
		t.Fatalf("otra IP: %d", res2.StatusCode)
	}
}

func TestAgentsListYRevocacion(t *testing.T) {
	ts := makeAgentTestServer(t)
	// GET /api/agents exige sesión
	res := get(t, ts.URL, "/api/agents", "")
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("GET /api/agents sin sesión: %d", res.StatusCode)
	}

	_, token, _ := createAgentToken(t, ts, "patio")
	if _, token2, _ := createAgentToken(t, ts, "living"); token2 == "" {
		t.Fatal("segundo token")
	}

	// Lista: 2 slugs, lastSeen null (nunca empujaron), SIN token ni hash
	res = get(t, ts.URL, "/api/agents", ts.cookie)
	body := readJSON(t, res)
	agents, _ := body["agents"].([]any)
	if len(agents) != 2 {
		t.Fatalf("agents: %v", body)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), "token") {
		t.Fatalf("GET /api/agents no debería exponer tokens: %s", raw)
	}

	// Tras un push: lastSeen (unix s) + versión + fresh
	res2 := ingest(t, ts, token, "10.0.3.1", validPayload)
	res2.Body.Close()
	res = get(t, ts.URL, "/api/agents", ts.cookie)
	body = readJSON(t, res)
	agents, _ = body["agents"].([]any)
	var patio map[string]any
	for _, a := range agents {
		m, _ := a.(map[string]any)
		if m["slug"] == "patio" {
			patio = m
		}
	}
	if patio == nil || patio["lastSeen"] == nil || patio["version"] != "0.1.0" || patio["fresh"] != true {
		t.Fatalf("patio tras push: %v", patio)
	}

	// Revocar → 204; el push siguiente recibe 401; GET ya no lo lista
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/agents/patio", nil)
	req.Header.Set("Cookie", "session="+ts.cookie)
	resDel, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resDel.Body.Close()
	if resDel.StatusCode != 204 {
		t.Fatalf("DELETE: %d", resDel.StatusCode)
	}
	res3 := ingest(t, ts, token, "10.0.3.2", validPayload)
	res3.Body.Close()
	if res3.StatusCode != 401 {
		t.Fatalf("push tras revocar: %d", res3.StatusCode)
	}
	res = get(t, ts.URL, "/api/agents", ts.cookie)
	body = readJSON(t, res)
	agents, _ = body["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("tras revocar: %v", body)
	}
	// Revocar de nuevo → 404
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/agents/patio", nil)
	req.Header.Set("Cookie", "session="+ts.cookie)
	resDel, _ = http.DefaultClient.Do(req)
	resDel.Body.Close()
	if resDel.StatusCode != 404 {
		t.Fatalf("DELETE 2ª vez: %d", resDel.StatusCode)
	}
}

func TestAgentsCreateValidaSlug(t *testing.T) {
	ts := makeAgentTestServer(t)
	for _, slug := range []string{"", "PATIO", "con espacios", "-x", strings.Repeat("a", 65)} {
		status, _, _ := createAgentToken(t, ts, slug)
		if status != 400 {
			t.Fatalf("slug %q debería dar 400, dio %d", slug, status)
		}
	}
}
