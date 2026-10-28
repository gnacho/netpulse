// mqtt_propagate_test.go — regresión F-06: POST /api/settings/mqtt/propagate
// entrega la op mqtt.configure a cada router NetGrip por SSE, sin hablar HTTP
// con sus executors. Cada router recibe un plan REAL (fila en orchestr_plans,
// marcado applying antes del envío) y la respuesta dice "encolado", nunca
// "aplicado".
package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

type propagateResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PlanID    string `json:"planId"`
	Queued    bool   `json:"queued"`
	Delegated bool   `json:"delegated"`
	Status    string `json:"status"`
	Error     string `json:"error"`
}

// propagateMQTT llama al endpoint y devuelve los resultados por id más el
// cuerpo crudo (para comprobar que no se afirma que el plan se aplicó).
func propagateMQTT(t *testing.T, ts *testServer, cookie string) (map[string]propagateResult, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/settings/mqtt/propagate", nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("propagate: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("propagate: esperaba 200, got %d", res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("leer respuesta propagate: %v", err)
	}
	var body struct {
		Results []propagateResult `json:"results"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("respuesta propagate: %v (%s)", err, raw)
	}
	out := map[string]propagateResult{}
	for _, r := range body.Results {
		if _, dup := out[r.ID]; dup {
			t.Fatalf("router duplicado en la respuesta: %s", r.ID)
		}
		out[r.ID] = r
	}
	return out, string(raw)
}

func TestMQTTPropagateSendsOpsOverSSE(t *testing.T) {
	ov := &adapters.Overview{Routers: []adapters.Router{
		{ID: "flint2", Name: "Flint 2"},
		{ID: "rt2", Name: "RT2"},
		{ID: "rt1", Name: "RT1"}, // agente nativo: no participa
	}}
	ts, reg := newSSEPlanServer(t, ov)
	netgripMock, hits := countingNetGripMock(t)
	host := netgripMock.Listener.Addr().String()
	addPlanRouter(t, ts, "flint2", host, true, "netgrip-secret")
	addPlanRouter(t, ts, "rt2", host, false, "netgrip-secret")
	addPlanRouter(t, ts, "rt1", host, false, "netgrip-secret")
	// flint2 y rt2 con agente conectado; rt1 es nativo y queda fuera.
	setAgentToken(t, ts, "flint2", "agent-token")
	setAgentToken(t, ts, "rt2", "agent-token")
	reg.Ingest(&probe.Payload{Router: "flint2", Kind: "netgrip", Version: "0.77.3"})
	reg.Ingest(&probe.Payload{Router: "rt2", Kind: "netgrip", Version: "0.77.3"})
	reg.Ingest(&probe.Payload{Router: "rt1", Kind: "native", Version: "3.0.26"})

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	flintEvents := connectAgentStream(t, ts.URL, "flint2")
	rt2Events := connectAgentStream(t, ts.URL, "rt2")

	results, raw := propagateMQTT(t, ts, cookie)

	if strings.Contains(raw, `"applied"`) {
		t.Errorf("la propagación solo encola: la respuesta no puede afirmar que se aplicó: %s", raw)
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Fatalf("NetGrip recibió %d peticiones HTTP; la propagación debe ir por SSE", got)
	}
	if len(results) != 2 {
		t.Fatalf("esperaba 2 routers NetGrip en el resultado, got %+v", results)
	}
	if _, ok := results["rt1"]; ok {
		t.Error("rt1 es agente nativo: no debe aparecer en la propagación")
	}

	// El agente conectado recibe la op mqtt.configure con el broker del server.
	streams := map[string]<-chan agentEvent{"flint2": flintEvents, "rt2": rt2Events}
	planIDs := map[string]string{}
	for id, r := range results {
		if r.Error != "" {
			t.Errorf("%s: error inesperado %q", id, r.Error)
		}
		if !r.Queued || !r.Delegated {
			t.Errorf("%s: se entregó al agente, esperaba queued/delegated true, got %+v", id, r)
		}
		// La respuesta habla de encolado, no de aplicación.
		if r.Status != "queued" {
			t.Errorf("%s: estado esperado queued, got %+v", id, r)
		}
		if r.PlanID == "" {
			t.Fatalf("%s: la respuesta debe llevar el planId real", id)
		}
		planIDs[id] = r.PlanID

		// El plan existe de verdad y está en curso (applying), no inventado.
		p := readPlan(t, ts, r.PlanID)
		if p.Status != "applying" {
			t.Errorf("%s: el plan debe quedar applying, got %s", id, p.Status)
		}
		if p.Resource != "mqtt" {
			t.Errorf("%s: recurso del plan: got %q, want mqtt", id, p.Resource)
		}
		if p.CreatedBy != "admin" {
			t.Errorf("%s: createdBy del plan: got %q, want admin", id, p.CreatedBy)
		}
		if !bytes.Contains([]byte(p.Diff), []byte("mqtt.configure")) {
			t.Errorf("%s: el diff persistido no lleva la op mqtt.configure: %s", id, p.Diff)
		}
		if !bytes.Contains([]byte(p.Desired), []byte("broker.test")) {
			t.Errorf("%s: el desired persistido no lleva el broker: %s", id, p.Desired)
		}
		// La contraseña del broker no se duplica en la tabla de planes.
		if bytes.Contains([]byte(p.Diff), []byte("broker-pass")) ||
			bytes.Contains([]byte(p.Desired), []byte("broker-pass")) {
			t.Errorf("%s: la contraseña del broker no debe persistirse en el plan", id)
		}

		ev := waitAgentEvent(t, streams[id], "apply")
		var payload struct {
			PlanID string        `json:"plan_id"`
			Ops    []executor.Op `json:"ops"`
		}
		if err := json.Unmarshal([]byte(ev.data), &payload); err != nil {
			t.Fatalf("%s: payload SSE apply inválido (%q): %v", id, ev.data, err)
		}
		if payload.PlanID != r.PlanID {
			t.Errorf("%s: plan_id SSE got %q, want %q", id, payload.PlanID, r.PlanID)
		}
		if len(payload.Ops) != 1 || payload.Ops[0].Kind != "mqtt.configure" {
			t.Fatalf("%s: ops SSE: %+v", id, payload.Ops)
		}
		if got := payload.Ops[0].Args["host"]; got != "broker.test" {
			t.Errorf("%s: broker en la op: got %q, want broker.test", id, got)
		}
		// El secreto viaja al agente (por el canal autenticado), aunque no se
		// guarde en el plan.
		if got := payload.Ops[0].Args["pass"]; got != "broker-pass" {
			t.Errorf("%s: la op enviada debe llevar la contraseña real, got %q", id, got)
		}
	}
	if planIDs["flint2"] == planIDs["rt2"] {
		t.Fatalf("cada router necesita su propio plan, ambos comparten %q", planIDs["flint2"])
	}

	// El agente confirma: el plan de ese router pasa a applied.
	if code, raw := postApplyResult(t, ts.URL, "flint2", "agent-token", planIDs["flint2"],
		executor.ApplyResult{Status: "applied"}); code != http.StatusOK {
		t.Fatalf("apply-result: esperaba 200, got %d: %s", code, raw)
	}
	if got := planStatus(t, ts, planIDs["flint2"]); got != "applied" {
		t.Fatalf("el plan confirmado por el agente debe quedar applied, got %s", got)
	}
}

// TestMQTTPropagateWithoutAgentIsHonest: sin agente conectado no se simula
// éxito: la respuesta lo dice y el plan no queda en curso.
func TestMQTTPropagateWithoutAgentIsHonest(t *testing.T) {
	ov := &adapters.Overview{Routers: []adapters.Router{{ID: "flint2", Name: "Flint 2"}}}
	ts, reg := newSSEPlanServer(t, ov)
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")
	reg.Ingest(&probe.Payload{Router: "flint2", Kind: "netgrip", Version: "0.77.3"})

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	results, _ := propagateMQTT(t, ts, cookie)

	r, ok := results["flint2"]
	if !ok {
		t.Fatalf("falta flint2 en la respuesta: %+v", results)
	}
	if r.Queued || r.Delegated || r.Status == "queued" {
		t.Errorf("sin agente conectado no se puede afirmar que se encoló: %+v", r)
	}
	if r.Error != "agent_not_connected" {
		t.Errorf("error esperado agent_not_connected, got %+v", r)
	}
	if r.PlanID == "" {
		t.Fatal("la respuesta debe identificar el plan creado para el intento")
	}
	if got := planStatus(t, ts, r.PlanID); got != "pending" {
		t.Errorf("un envío fallido no puede dejar el plan en curso, got %s", got)
	}
}

// TestMQTTPropagateDoesNotPersistBrokerPassword revisa la copia persistida
// del plan: el secreto viaja por SSE, no se guarda en orchestr_plans.
func TestMQTTPropagateDoesNotPersistBrokerPassword(t *testing.T) {
	ov := &adapters.Overview{Routers: []adapters.Router{{ID: "flint2", Name: "Flint 2"}}}
	ts, reg := newSSEPlanServer(t, ov)
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")
	reg.Ingest(&probe.Payload{Router: "flint2", Kind: "netgrip", Version: "0.77.3"})
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	connectAgentStream(t, ts.URL, "flint2")

	results, _ := propagateMQTT(t, ts, cookie)
	r := results["flint2"]
	if r.PlanID == "" {
		t.Fatalf("sin planId en la respuesta: %+v", r)
	}

	// La fila completa, tal cual queda en la base de datos.
	var desired, diff, result string
	if err := ts.db.QueryRow(
		`SELECT desired, diff, COALESCE(result, '') FROM orchestr_plans WHERE id = ?`,
		r.PlanID).Scan(&desired, &diff, &result); err != nil {
		t.Fatalf("select plan: %v", err)
	}
	blob := strings.Join([]string{desired, diff, result}, "\n")
	if strings.Contains(blob, "broker-pass") {
		t.Fatalf("la contraseña del broker quedó persistida en el plan:\n%s", blob)
	}
	if !strings.Contains(diff, "mqtt.configure") {
		t.Errorf("el plan debe registrar la op enviada: %s", diff)
	}
}
