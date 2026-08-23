// upgrade_api_test.go — Fase 6.3 (issue #243): self-update del agente.
// Contrato de POST /api/agents/{slug}/upgrade (admin) y
// POST /api/agents/{slug}/upgrade-result (Bearer del agente), y del campo
// updateAvailable de GET /api/agents.
package httpapi_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
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

type upgradeTestServer struct {
	*httptest.Server
	db     *db.DB
	agents *adapters.AgentRegistry
	hub    *sse.AgentHub
	cookie string
}

// makeUpgradeTestServer: app real con registry + AgentHub (checkToken contra
// kv, como producción). El AgentHub habilita stream/refresh/upgrade.
func makeUpgradeTestServer(t *testing.T) *upgradeTestServer {
	t.Helper()
	auth.SetTrustProxy(true)
	t.Cleanup(func() { auth.SetTrustProxy(false) })
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
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatalf("users: %v", err)
	}
	reg := adapters.NewAgentRegistry(90 * time.Second)
	hub := sse.NewAgentHub(func(slug, token string) bool {
		if token == "" || d == nil {
			return false
		}
		var stored string
		if err := d.QueryRow("SELECT value FROM kv WHERE key = ?", "agent.token."+slug).Scan(&stored); err != nil {
			return false
		}
		sum := sha256.Sum256([]byte(token))
		return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(stored)) == 1
	})
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(),
		Hub:    sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil }),
		Secret: secret, Agents: reg, AgentHub: hub, Started: time.Now(),
	})
	srv := httptest.NewServer(handler)
	status, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if status != 204 {
		t.Fatalf("login: %d", status)
	}
	ts := &upgradeTestServer{Server: srv, db: d, agents: reg, hub: hub, cookie: cookie}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts
}

// createUpgradeToken crea el token de un slug vía API (sesión admin).
func createUpgradeToken(t *testing.T, ts *upgradeTestServer, slug string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/agents", strings.NewReader(`{"slug":"`+slug+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+ts.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/agents: %v", err)
	}
	defer res.Body.Close()
	var body struct {
		Token string `json:"token"`
	}
	data, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(data, &body)
	return res.StatusCode, body.Token
}

// postUpgrade hace POST /api/agents/{slug}/upgrade con la cookie dada.
func postUpgrade(t *testing.T, ts *upgradeTestServer, slug, cookie string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/agents/"+slug+"/upgrade", nil)
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /upgrade: %v", err)
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res.StatusCode, body
}

// upgradeIngest hace un push de agente (mismo contrato que rearmIngest).
func upgradeIngest(t *testing.T, ts *upgradeTestServer, token, payload string) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/ingest/agent", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "10.9.9.1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		mac := hmac.New(sha256.New, []byte(token))
		mac.Write([]byte(payload))
		req.Header.Set("X-Agent-Signature", hex.EncodeToString(mac.Sum(nil)))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatalf("ingest: got %d want 202", res.StatusCode)
	}
}

// upgradePayload construye un payload válido con la versión dada (router "patio").
func upgradePayload(version string) string {
	return upgradePayloadFor("patio", version)
}

// upgradePayloadFor: igual que upgradePayload pero para un router arbitrario.
func upgradePayloadFor(router, version string) string {
	return fmt.Sprintf(`{"router":%q,"ts":%d,"version":%q,"data":{"system":{"sysinfo":{"uptime":100,"load":[0,0,0],"memory":{"total":100,"free":50,"buffered":0,"available":50}},"cpu":10,"temp":40},"wireless":{"clients":{}},"dhcp":{"leases":[]},"fdb":{"macs":{}}}}`, router, time.Now().Unix(), version)
}

// openStream registra una conexión SSE falsa del agente en el hub usando un
// ResponseRecorder (sin servidor HTTP real, para no disparar la carrera
// finishRequest de net/http). Devuelve cancel + done para detener el handler.
func openStream(t *testing.T, ts *upgradeTestServer, slug, token string) (cancel context.CancelFunc, done chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/agents/"+slug+"/stream", nil).WithContext(ctx)
	req.SetPathValue("slug", slug)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	done = make(chan struct{})
	go func() {
		ts.hub.HandleStream(rec, req)
		close(done)
	}()
	// Margen para que el hub registre la conexión antes del POST.
	time.Sleep(200 * time.Millisecond)
	return cancel, done
}

func TestUpgradeSinSesion401(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	status, body := postUpgrade(t, ts, "patio", "")
	if status != 401 || body["error"] != "unauthorized" {
		t.Fatalf("sin sesión: got %d %v", status, body)
	}
}

func TestUpgradeNoAdmin403(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	// Crear usuario no-admin y loguear con él.
	req, _ := http.NewRequest("POST", ts.URL+"/api/users", strings.NewReader(`{"username":"ana","password":"secreto12345"}`))
	req.Header.Set("Cookie", "session="+ts.cookie)
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	_, anaCookie, _ := loginCookie(t, ts.URL, "ana", "secreto12345")
	status, body := postUpgrade(t, ts, "patio", anaCookie)
	if status != 403 || body["error"] != "forbidden" {
		t.Fatalf("no-admin: got %d %v", status, body)
	}
}

func TestUpgradeSlugDesconocido404(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	// Sin token registrado → 404 (distinto del 409 de "no conectado").
	status, body := postUpgrade(t, ts, "noexiste", ts.cookie)
	if status != 404 || body["error"] != "not_found" {
		t.Fatalf("slug desconocido: got %d %v", status, body)
	}
}

func TestUpgradeNoConectado409(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	if st, _ := createUpgradeToken(t, ts, "patio"); st != 201 {
		t.Fatalf("create: %d", st)
	}
	status, body := postUpgrade(t, ts, "patio", ts.cookie)
	if status != 409 || body["error"] != "agent_not_connected" {
		t.Fatalf("no conectado: got %d %v", status, body)
	}
}

func TestUpgradeConectado202(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	st, tok := createUpgradeToken(t, ts, "patio")
	if st != 201 || tok == "" {
		t.Fatalf("create: %d", st)
	}
	cancel, done := openStream(t, ts, "patio", tok)

	status, body := postUpgrade(t, ts, "patio", ts.cookie)
	if status != 202 || body["ok"] != true {
		t.Fatalf("conectado: got %d %v", status, body)
	}
	if len(body) != 1 {
		t.Fatalf("body con claves extra: %v", body)
	}
	cancel()
	<-done
}

func TestUpgradeResultBearer(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	st, tok := createUpgradeToken(t, ts, "patio")
	if st != 201 || tok == "" {
		t.Fatalf("create: %d", st)
	}

	post := func(token, body string) int {
		req, _ := http.NewRequest("POST", ts.URL+"/api/agents/patio/upgrade-result", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("upgrade-result: %v", err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}

	// Sin token → 401 (bypass de RequireAuth, pero el handler exige Bearer).
	if got := post("", `{"ok":true}`); got != 401 {
		t.Fatalf("sin token: got %d want 401", got)
	}
	// Token inválido → 401.
	if got := post("deadbeef", `{"ok":true}`); got != 401 {
		t.Fatalf("token malo: got %d want 401", got)
	}
	// Token válido, body ok → 200.
	if got := post(tok, `{"ok":true}`); got != 200 {
		t.Fatalf("ok válido: got %d want 200", got)
	}
	// Token válido, body con error → 200 (se loguea).
	if got := post(tok, `{"ok":false,"error":"swap failed"}`); got != 200 {
		t.Fatalf("error válido: got %d want 200", got)
	}
	// Body no-JSON → 400.
	if got := post(tok, `not-json`); got != 400 {
		t.Fatalf("body roto: got %d want 400", got)
	}
}

// TestAgentsListUpdateAvailable: el campo updateAvailable refleja si la versión
// reportada por el agente difiere del binario embebido (agentbin.EmbeddedAgentVersion).
func TestAgentsListUpdateAvailable(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	st, tok := createUpgradeToken(t, ts, "patio")
	if st != 201 {
		t.Fatalf("create: %d", st)
	}

	agentItem := func() map[string]any {
		res := get(t, ts.URL, "/api/agents", ts.cookie)
		body := readJSON(t, res)
		for _, a := range body["agents"].([]any) {
			if m, _ := a.(map[string]any); m["slug"] == "patio" {
				return m
			}
		}
		t.Fatalf("patio ausente: %v", body)
		return nil
	}

	// Push con versión == EmbeddedAgentVersion (0.1.0) → updateAvailable=false.
	upgradeIngest(t, ts, tok, upgradePayload("0.1.0"))
	if patio := agentItem(); patio["updateAvailable"] != false {
		t.Fatalf("versión 0.1.0 debe dar updateAvailable=false: %v", patio)
	}

	// Push con versión distinta → updateAvailable=true.
	upgradeIngest(t, ts, tok, upgradePayload("9.9.9"))
	if patio := agentItem(); patio["updateAvailable"] != true {
		t.Fatalf("versión 9.9.9 debe dar updateAvailable=true: %v", patio)
	}
}

// TestAgentsUpgradeAll — #251: POST /api/agents/upgrade-all.
//   - 403 sin admin.
//   - Con un agente conectado y desactualizado → status "sent".
//   - Con un agente desconectado → status "not_connected".
//   - Con un agente al día (versión == embebida) → status "up_to_date".
func TestAgentsUpgradeAll(t *testing.T) {
	ts := makeUpgradeTestServer(t)

	// Sin admin → 403.
	res := doReq(t, "POST", ts.URL+"/api/agents/upgrade-all", "", "")
	if res.StatusCode != 403 && res.StatusCode != 401 {
		t.Fatalf("sin admin: got %d want 403/401", res.StatusCode)
	}

	// "patio" desactualizado y conectado → sent.
	st, tok := createUpgradeToken(t, ts, "patio")
	if st != 201 {
		t.Fatalf("create patio: %d", st)
	}
	upgradeIngest(t, ts, tok, upgradePayload("9.9.9"))
	cancel, done := openStream(t, ts, "patio", tok)
	defer func() { cancel(); <-done }()

	// "otro" desactualizado pero SIN conectar → not_connected.
	if st, tokO := createUpgradeToken(t, ts, "otro"); st != 201 {
		t.Fatalf("create otro: %d", st)
	} else {
		upgradeIngest(t, ts, tokO, upgradePayloadFor("otro", "9.9.9"))
	}
	// "aldia" con versión == embebida → up_to_date.
	st2, tok2 := createUpgradeToken(t, ts, "aldia")
	if st2 != 201 {
		t.Fatalf("create aldia: %d", st2)
	}
	upgradeIngest(t, ts, tok2, upgradePayloadFor("aldia", "0.1.0"))

	res = doReq(t, "POST", ts.URL+"/api/agents/upgrade-all", ts.cookie, "")
	if res.StatusCode != 200 {
		t.Fatalf("upgrade-all: got %d", res.StatusCode)
	}
	body := readJSON(t, res)
	bySlug := map[string]string{}
	for _, a := range body["agents"].([]any) {
		m := a.(map[string]any)
		bySlug[m["slug"].(string)] = m["status"].(string)
	}
	if bySlug["patio"] != "sent" {
		t.Fatalf("patio debería ser sent: %v", bySlug)
	}
	if bySlug["otro"] != "not_connected" {
		t.Fatalf("otro debería ser not_connected: %v", bySlug)
	}
	if bySlug["aldia"] != "up_to_date" {
		t.Fatalf("al_dia debería ser up_to_date: %v", bySlug)
	}
}
