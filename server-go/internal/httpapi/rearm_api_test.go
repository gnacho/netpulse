// rearm_api_test.go — Fase 5 (Plan B): POST /api/agents/{slug}/rearm.
// Casos: 404 sin slug registrado, 409 slug sin router en la tabla routers,
// 503 sin pool SSH (modo demo), 502 si el SSH falla, 200 recuperado (un push
// de vuelta llega tras el reinicio), 200 sin push de vuelta y 429 por cooldown.
package httpapi_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// rearmBody refleja la respuesta JSON de POST /api/agents/{slug}/rearm.
type rearmBody struct {
	Slug      string `json:"slug"`
	Restarted bool   `json:"restarted"`
	Recovered bool   `json:"recovered"`
	Message   string `json:"message"`
}

// fakeSSH registra los comandos ejecutados y falla si fail=true.
type fakeSSH struct {
	mu   sync.Mutex
	cmds []string
	fail bool
}

func (f *fakeSSH) Run(host, cmd string, _ time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return "", errors.New("ssh down")
	}
	f.cmds = append(f.cmds, host+":"+cmd)
	return "", nil
}

func (f *fakeSSH) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cmds)
}

type rearmTestServer struct {
	*httptest.Server
	db     *db.DB
	agents *adapters.AgentRegistry
	ssh    *fakeSSH
	cookie string
}

// makeRearmTestServer: app real con registry + router "patio" en la tabla
// routers. pool=nil simula demo sin SSH; pollWait corto para no dormir tests.
func makeRearmTestServer(t *testing.T, pool *fakeSSH, pollWait time.Duration) *rearmTestServer {
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
	// Router "patio" en la tabla routers (slug = name slugificado)
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "patio", Host: "192.168.1.4", Type: "openwrt",
	}); err != nil {
		t.Fatalf("AddRouter: %v", err)
	}
	reg := adapters.NewAgentRegistry(90 * time.Second)
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	var runner httpapi.SSHRunner
	if pool != nil {
		runner = pool
	}
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Hub: hub, Secret: secret,
		Agents: reg, Pool: runner, RearmPollWait: pollWait, Started: time.Now(),
	})
	srv := httptest.NewServer(handler)
	status, cookie, _ := loginCookie(t, srv.URL, "admin", "test1234")
	if status != 204 {
		t.Fatalf("login: %d", status)
	}
	ts := &rearmTestServer{Server: srv, db: d, agents: reg, ssh: pool, cookie: cookie}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts
}

// rearm dispara el POST y devuelve status + body.
func rearm(t *testing.T, ts *rearmTestServer, slug string) (int, rearmBody) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/agents/"+slug+"/rearm", strings.NewReader(""))
	req.Header.Set("Cookie", "session="+ts.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rearm: %v", err)
	}
	defer res.Body.Close()
	var body rearmBody
	data, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(data, &body)
	return res.StatusCode, body
}

// rearmRaw devuelve status + body crudo (mensajes de error).
func rearmRaw(t *testing.T, ts *rearmTestServer, slug string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/agents/"+slug+"/rearm", strings.NewReader(""))
	req.Header.Set("Cookie", "session="+ts.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rearm: %v", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(data)
}

// createRearmToken crea el token de un slug vía API (con sesión).
func createRearmToken(t *testing.T, ts *rearmTestServer, slug string) (int, string) {
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

// rearmIngest hace un push de agente contra ts (mismo contrato que ingest de
// agent_api_test.go, pero con URL directa).
func rearmIngest(t *testing.T, ts *rearmTestServer, token, payload string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/ingest/agent", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "10.9.9.1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	return res
}

func TestRearmSinSlugRegistrado404(t *testing.T) {
	ts := makeRearmTestServer(t, &fakeSSH{}, time.Second)
	status, _ := rearm(t, ts, "noexiste")
	if status != 404 {
		t.Fatalf("slug sin token: quiero 404, tuve %d", status)
	}
}

func TestRearmSinRouter409(t *testing.T) {
	ts := makeRearmTestServer(t, &fakeSSH{}, time.Second)
	// Token sin router asociado en la tabla routers
	if st, tok := createRearmToken(t, ts, "solitario"); st != 201 || tok == "" {
		t.Fatalf("create: %d", st)
	}
	status, body := rearmRaw(t, ts, "solitario")
	if status != 409 {
		t.Fatalf("quiero 409 router_unknown, tuve %d (%s)", status, body)
	}
	if ts.ssh.count() != 0 {
		t.Fatalf("no debe ejecutarse SSH con router desconocido")
	}
}

func TestRearmRecuperado200(t *testing.T) {
	ssh := &fakeSSH{}
	ts := makeRearmTestServer(t, ssh, 1500*time.Millisecond)
	st, tok := createRearmToken(t, ts, "patio")
	if st != 201 {
		t.Fatalf("create: %d", st)
	}
	// Estado previo (prevSeen): un push anterior para que el rearme espere
	// uno NUEVO.
	ts.agents.Ingest(&probe.Payload{Router: "patio", Ts: time.Now().Unix(), Version: "0.1.0"})

	// En paralelo: 200 ms después el agente "vuelve a empujar"
	go func() {
		time.Sleep(200 * time.Millisecond)
		res := rearmIngest(t, ts, tok, validPayload)
		res.Body.Close()
	}()

	status, body := rearm(t, ts, "patio")
	if status != 200 {
		t.Fatalf("quiero 200, tuve %d", status)
	}
	if !body.Restarted || !body.Recovered {
		t.Fatalf("quiero restarted+recovered, tuve %+v", body)
	}
	if ssh.count() != 1 {
		t.Fatalf("quiero 1 comando SSH, tuve %d", ssh.count())
	}
}

func TestRearmSinPushVuelta(t *testing.T) {
	ts := makeRearmTestServer(t, &fakeSSH{}, 300*time.Millisecond)
	st, _ := createRearmToken(t, ts, "patio")
	if st != 201 {
		t.Fatalf("create: %d", st)
	}
	status, body := rearm(t, ts, "patio")
	if status != 200 {
		t.Fatalf("quiero 200, tuve %d", status)
	}
	if !body.Restarted || body.Recovered {
		t.Fatalf("quiero restarted sin recovered, tuve %+v", body)
	}
}

func TestRearmSSHCae502(t *testing.T) {
	ssh := &fakeSSH{fail: true}
	ts := makeRearmTestServer(t, ssh, time.Second)
	st, _ := createRearmToken(t, ts, "patio")
	if st != 201 {
		t.Fatalf("create: %d", st)
	}
	status, body := rearmRaw(t, ts, "patio")
	if status != 502 {
		t.Fatalf("quiero 502 ssh_failed, tuve %d (%s)", status, body)
	}
}

func TestRearmCooldown429(t *testing.T) {
	ts := makeRearmTestServer(t, &fakeSSH{}, 100*time.Millisecond)
	st, _ := createRearmToken(t, ts, "patio")
	if st != 201 {
		t.Fatalf("create: %d", st)
	}
	if status, _ := rearm(t, ts, "patio"); status != 200 {
		t.Fatalf("primer rearme: quiero 200, tuve %d", status)
	}
	status, body := rearmRaw(t, ts, "patio")
	if status != 429 {
		t.Fatalf("segundo rearme inmediato: quiero 429, tuve %d (%s)", status, body)
	}
}

func TestRearmSinPool503(t *testing.T) {
	// pool nil → demo sin SSH: el endpoint debe decirlo claramente.
	ts := makeRearmTestServer(t, nil, time.Second)
	st, _ := createRearmToken(t, ts, "patio")
	if st != 201 {
		t.Fatalf("create: %d", st)
	}
	status, body := rearmRaw(t, ts, "patio")
	if status != 503 {
		t.Fatalf("quiero 503 ssh_unavailable, tuve %d (%s)", status, body)
	}
}
