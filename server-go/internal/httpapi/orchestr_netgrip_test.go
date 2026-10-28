// orchestr_netgrip_test.go — regresión F-06: aplicar un plan va SIEMPRE al
// agente por SSE. El servidor no habla HTTP con NetGrip ni manda su executor
// token, así que un NetGrip HTTP accesible recibe cero peticiones.
package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/apitoken"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/configbackup"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/mqttpub"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// serverOption ajusta el servidor de test antes de arrancarlo.
type serverOption func(*serverOptions)

type serverOptions struct {
	wrap func(http.Handler) http.Handler
}

// withHandlerWrap envuelve el handler completo: lo usan los tests que
// necesitan interceptar el stream SSE del agente (agente que responde al
// instante o conexión que se rompe a mitad del envío).
func withHandlerWrap(wrap func(http.Handler) http.Handler) serverOption {
	return func(o *serverOptions) { o.wrap = wrap }
}

// newSSEPlanServer monta un servidor con AgentHub (auth siempre válida),
// registry de agentes, manager MQTT y overview opcional, de modo que los
// tests puedan conectar un agente falso y comprobar lo que recibe por SSE.
func newSSEPlanServer(t *testing.T, ov *adapters.Overview, opts ...serverOption) (*testServer, *adapters.AgentRegistry) {
	t.Helper()
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
	if err := apitoken.EnsureSchema(d); err != nil {
		t.Fatalf("api_tokens schema: %v", err)
	}
	configBackup, err := configbackup.NewStore(d)
	if err != nil {
		t.Fatalf("config backup store: %v", err)
	}
	reg := adapters.NewAgentRegistry(0)
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	agentHub := sse.NewAgentHub(func(_, _ string) bool { return true })
	agentHub.SetAllowInsecureCommands(true)
	mqttMgr := mqttpub.NewManager(mqttpub.Config{
		Enabled: true, Host: "broker.test", Port: 1883,
		User: "netpulse", Pass: "broker-pass", Instance: "netpulse",
		Interval: 60 * time.Second,
	}, "test", nil, false)
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Hub: hub, Secret: secret,
		Agents: reg, Pool: fakeProbeSSH{}, Started: time.Now(),
		TokenStore:   apitoken.NewStore(d, secret),
		ConfigBackup: configBackup,
		Orchestr:     orchestr.New(d),
		AgentHub:     agentHub,
		MQTT:         mqttMgr,
		LastOverview: func() *adapters.Overview { return ov },
	})
	var so serverOptions
	for _, opt := range opts {
		opt(&so)
	}
	var served http.Handler = handler
	if so.wrap != nil {
		served = so.wrap(handler)
	}
	srv := httptest.NewServer(served)
	ts := &testServer{Server: srv, db: d, secret: secret}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts, reg
}

// addPlanRouter inserta un router con ID fijo y, si tiene, el executor token
// de NetGrip en kv: con el código anterior eso bastaba para delegar por HTTP.
func addPlanRouter(t *testing.T, ts *testServer, id, host string, gateway bool, executorToken string) {
	t.Helper()
	if _, err := routerstore.AddRouter(ts.db.DB, routerstore.AddInput{
		Name: "Flota " + id, Host: host, Type: "openwrt", IsGateway: gateway,
	}); err != nil {
		t.Fatalf("add router %s: %v", id, err)
	}
	if _, err := ts.db.Exec("UPDATE routers SET id = ? WHERE name = ?", id, "Flota "+id); err != nil {
		t.Fatalf("update router id %s: %v", id, err)
	}
	if executorToken != "" {
		if _, err := ts.db.Exec(
			"INSERT INTO kv (key, value) VALUES (?, ?)",
			"netgrip.executor_token."+id, executorToken); err != nil {
			t.Fatalf("insert executor token %s: %v", id, err)
		}
	}
}

// countingNetGripMock emula el executor HTTP de NetGrip y cuenta peticiones.
// El contrato F-06 exige que la cuenta se quede a cero y que el token no salga.
func countingNetGripMock(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "NetPulse no debe delegar por HTTP", http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// agentEvent es un evento SSE recibido del stream del agente.
type agentEvent struct {
	name string
	data string
}

// connectAgentStream abre el stream SSE del agente y devuelve el canal de
// eventos. Espera al evento "connected" para que el hub ya tenga la conexión
// registrada antes de disparar el comando.
func connectAgentStream(t *testing.T, base, slug string) <-chan agentEvent {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := make(chan agentEvent, 16)
	go func() {
		defer close(events)
		req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/agents/"+slug+"/stream", nil)
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer agent-token")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer res.Body.Close()
		br := bufio.NewReader(res.Body)
		name := ""
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				events <- agentEvent{name: name, data: strings.TrimPrefix(line, "data: ")}
			}
		}
	}()
	select {
	case ev, ok := <-events:
		if !ok || ev.name != "connected" {
			t.Fatalf("stream SSE: esperaba connected, got %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("el stream SSE del agente no llegó a conectar")
	}
	return events
}

// waitAgentEvent descarta heartbeats y eventos intermedios hasta `name`.
func waitAgentEvent(t *testing.T, events <-chan agentEvent, name string) agentEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("stream SSE cerrado esperando el evento %q", name)
			}
			if ev.name == name {
				return ev
			}
		case <-deadline:
			t.Fatalf("timeout esperando el evento SSE %q", name)
		}
	}
}

// planStatus lee el estado persistido de un plan.
func planStatus(t *testing.T, ts *testServer, id string) string {
	t.Helper()
	var status string
	if err := ts.db.QueryRow("SELECT status FROM orchestr_plans WHERE id = ?", id).Scan(&status); err != nil {
		t.Fatalf("select plan status: %v", err)
	}
	return status
}

func TestApplySendsOpsOverSSEWithoutNetGripHTTP(t *testing.T) {
	ts, reg := newSSEPlanServer(t, nil)
	netgripMock, hits := countingNetGripMock(t)

	// Router "NetGrip": host del mock + executor token en kv. Con el código
	// anterior, aplicar delegaba aquí por HTTP con el Bearer del executor.
	addPlanRouter(t, ts, "flint2", netgripMock.Listener.Addr().String(), true, "netgrip-secret")
	reg.Ingest(&probe.Payload{Router: "flint2", Kind: "netgrip", Version: "0.77.3"})

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	events := connectAgentStream(t, ts.URL, "flint2")

	planID := "plan-apply-sse"
	ops := []executor.Op{{Kind: "uci_set", Args: map[string]string{"key": "guestwifi.guest.enabled"}, Desc: "enable guest"}}
	opsJSON, _ := json.Marshal(ops)
	if _, err := ts.db.Exec(
		`INSERT INTO orchestr_plans (id, router_id, resource, desired, diff, status, created_by, created_at)
		 VALUES (?, ?, 'guestwifi', '{}', ?, 'pending', 'admin', ?)`,
		planID, "flint2", string(opsJSON), time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	req, _ := http.NewRequest("POST", ts.URL+"/api/plans/"+planID+"/apply", nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("apply: esperaba 202, got %d: %s", res.StatusCode, string(b))
	}

	// Ninguna petición al executor HTTP de NetGrip (ni con token).
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Fatalf("NetGrip recibió %d peticiones HTTP; el apply debe ir por SSE", got)
	}
	// El plan queda en curso: lo asienta el resultado real del agente.
	if got := planStatus(t, ts, planID); got != "applying" {
		t.Fatalf("plan status: esperaba applying, got %s", got)
	}

	ev := waitAgentEvent(t, events, "apply")
	var payload struct {
		PlanID string        `json:"plan_id"`
		Ops    []executor.Op `json:"ops"`
	}
	if err := json.Unmarshal([]byte(ev.data), &payload); err != nil {
		t.Fatalf("payload SSE apply inválido (%q): %v", ev.data, err)
	}
	if payload.PlanID != planID {
		t.Errorf("plan_id SSE: got %q, want %q", payload.PlanID, planID)
	}
	if len(payload.Ops) != 1 || payload.Ops[0].Kind != "uci_set" {
		t.Fatalf("ops SSE: %+v", payload.Ops)
	}
}

// streamHookMode decide cómo se comporta el wrapper del stream SSE del agente.
type streamHookMode int

const (
	// streamHookNone: stream normal (solo cuenta lo que pasa).
	streamHookNone streamHookMode = iota
	// streamHookFastAgent: en cuanto el servidor escribe el evento "apply",
	// el agente responde su apply-result ANTES de que Send devuelva. Es el
	// caso real de un agente que aplica rápido y reporta al instante.
	streamHookFastAgent
	// streamHookFailOnApply: la escritura del evento "apply" falla (el agente
	// se cayó a mitad del envío). Send debe devolver false.
	streamHookFailOnApply
)

// hookedStreamWriter intercepta el stream SSE del agente.
type hookedStreamWriter struct {
	http.ResponseWriter
	mode    streamHookMode
	onApply func()
	failAt  int
	seen    *int32
}

func (w *hookedStreamWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *hookedStreamWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *hookedStreamWriter) Write(b []byte) (int, error) {
	isApply := strings.Contains(string(b), "event: apply")
	idx := int32(0)
	if isApply && w.seen != nil {
		idx = atomic.AddInt32(w.seen, 1)
	}
	if isApply && w.mode == streamHookFailOnApply && (w.failAt == 0 || int(idx) == w.failAt) {
		return 0, io.ErrClosedPipe
	}
	n, err := w.ResponseWriter.Write(b)
	if err == nil && isApply && w.mode == streamHookFastAgent {
		w.Flush()
		if w.onApply != nil {
			w.onApply()
		}
	}
	return n, err
}

// withStreamHook envuelve el handler para interceptar el stream del agente.
// failAt > 0 limita el fallo al n-ésimo evento apply (1 = el primero).
func withStreamHook(mode streamHookMode, onApply func()) func(http.Handler) http.Handler {
	return withStreamHookFailAt(mode, onApply, 0)
}

func withStreamHookFailAt(mode streamHookMode, onApply func(), failAt int) func(http.Handler) http.Handler {
	var seen int32
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/stream") {
				w = &hookedStreamWriter{ResponseWriter: w, mode: mode, onApply: onApply, failAt: failAt, seen: &seen}
			}
			h.ServeHTTP(w, r)
		})
	}
}
