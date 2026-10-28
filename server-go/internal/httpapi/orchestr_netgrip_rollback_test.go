// orchestr_netgrip_rollback_test.go — regresión F-06: el rollback va SIEMPRE
// al agente vía SSE (comando apply con las ops inversas, que es el único que
// el agente ejecuta) y no habla HTTP con NetGrip. El plan queda rolling_back
// hasta que el agente reporta, y entonces se cierra como rolled_back.
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
)

// fakeProbeSSH responde al probe de guestwifi con secciones vacías (sin guest
// configurado) y no hace SSH real.
type fakeProbeSSH struct{}

func (fakeProbeSSH) Run(host, cmd string, _ time.Duration) (string, error) {
	if strings.Contains(cmd, "===WIRELESS===") {
		return "===WIRELESS===\n===NETWORK===\n===FIREWALL===\n===END===", nil
	}
	return "", nil
}

// opKey identifica una op por tipo + argumentos relevantes, para comparar la
// secuencia exacta sin depender de Desc (texto libre).
func opKey(op executor.Op) string {
	keys := []string{"config", "section", "option", "value", "name", "action", "enforce", "type"}
	parts := []string{op.Kind}
	for _, k := range keys {
		if v, ok := op.Args[k]; ok {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, " ")
}

func TestRollbackSendsInverseOpsOverSSE(t *testing.T) {
	ts, _ := newSSEPlanServer(t, nil)
	netgripMock, hits := countingNetGripMock(t)
	addPlanRouter(t, ts, "flint2", netgripMock.Listener.Addr().String(), true, "netgrip-secret")
	setAgentToken(t, ts, "flint2", "agent-token")

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	events := connectAgentStream(t, ts.URL, "flint2")

	// Plan guestwifi YA APLICADO con enabled=true (el rollback lo invierte).
	planID := "plan-roll-sse"
	opsJSON, _ := json.Marshal([]executor.Op{{Kind: "uci_set", Args: map[string]string{"key": "guestwifi.guest.enabled"}, Desc: "enable guest"}})
	if _, err := ts.db.Exec(
		`INSERT INTO orchestr_plans (id, router_id, resource, desired, diff, status, created_by, created_at, applied_at, result)
		 VALUES (?, ?, 'guestwifi', ?, ?, 'applied', 'admin', ?, ?, '{"status":"applied"}')`,
		planID, "flint2", `{"enabled":true,"allowNonGateway":false}`, string(opsJSON),
		time.Now().Unix(), time.Now().Unix()); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	req, _ := http.NewRequest("POST", ts.URL+"/api/plans/"+planID+"/rollback", nil)
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

	if got := atomic.LoadInt32(hits); got != 0 {
		t.Fatalf("NetGrip recibió %d peticiones HTTP; el rollback debe ir por SSE", got)
	}
	if got := planStatus(t, ts, planID); got != "rolling_back" {
		t.Fatalf("plan status: esperaba rolling_back, got %s", got)
	}

	ev := waitAgentEvent(t, events, "apply")
	var payload struct {
		PlanID string        `json:"plan_id"`
		Ops    []executor.Op `json:"ops"`
	}
	if err := json.Unmarshal([]byte(ev.data), &payload); err != nil {
		t.Fatalf("payload SSE del rollback inválido (%q): %v", ev.data, err)
	}
	if payload.PlanID != planID {
		t.Errorf("plan_id SSE: got %q, want %q", payload.PlanID, planID)
	}

	// El rollback manda el modo de ownership delante (igual que un apply) y
	// después EXACTAMENTE las ops que deshacen el guest: borra la sección de
	// red guest y recarga, sin volver a crear ni a configurar nada.
	want := []string{
		"ownership_mode enforce=true",
		"uci_set_managed config=network section=guest",
		"uci_commit config=wireless",
		"uci_delete_section config=network section=guest",
		"uci_commit config=network",
		"uci_commit config=firewall",
		"service name=network action=reload",
		"service name=firewall action=reload",
	}
	if len(payload.Ops) != len(want) {
		t.Fatalf("ops inversas: esperaba %d, got %d: %+v", len(want), len(payload.Ops), payload.Ops)
	}
	for i, op := range payload.Ops {
		if got := opKey(op); got != want[i] {
			t.Fatalf("op[%d]: got %q, want %q", i, got, want[i])
		}
		if strings.Contains(opKey(op), "wifi-iface") {
			t.Errorf("op[%d] recrea la wifi-iface guest: %+v", i, op)
		}
	}
	for _, op := range payload.Ops {
		if op.Kind == "uci_add" || op.Kind == "uci_set_named" || op.Kind == "uci_set" {
			t.Fatalf("el rollback no debe crear ni fijar opciones: %+v", op)
		}
	}

	// El agente confirma el rollback: el plan se cierra como rolled_back (no
	// se queda en rolling_back para siempre).
	if code, raw := postApplyResult(t, ts.URL, "flint2", "agent-token", planID,
		executor.ApplyResult{Status: "applied"}); code != http.StatusOK {
		t.Fatalf("apply-result del rollback: esperaba 200, got %d: %s", code, raw)
	}
	if got := planStatus(t, ts, planID); got != "rolled_back" {
		t.Fatalf("tras el rollback: esperaba rolled_back, got %s", got)
	}
	acts := auditActionsOf(t, ts, planID)
	if !containsAction(acts, "rollback") || !containsAction(acts, "rolled_back") {
		t.Errorf("la auditoría debe registrar rollback y rolled_back, got %v", acts)
	}
}

// TestRollbackRejectsPlanNotApplied: solo se revierte lo aplicado, y el plan
// no cambia de estado si la petición no procede.
func TestRollbackRejectsPlanNotApplied(t *testing.T) {
	ts, _ := newSSEPlanServer(t, nil)
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	planID := createPlanViaAPI(t, ts.URL, cookie, "flint2", "guestwifi", []executor.Op{
		{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "commit wireless"},
	})
	code, raw := postRollback(t, ts.URL, cookie, planID)
	if code != http.StatusConflict {
		t.Fatalf("rollback de un plan pendiente: esperaba 409, got %d: %s", code, raw)
	}
	if got := planStatus(t, ts, planID); got != "pending" {
		t.Fatalf("el plan debe seguir pending, got %s", got)
	}
}

// TestRollbackWithoutAgentKeepsPlanApplied: si el agente no está, no se puede
// dejar el plan en rolling_back (eso lo dejaría colgado sin nadie que lo cierre).
func TestRollbackWithoutAgentKeepsPlanApplied(t *testing.T) {
	ts, _ := newSSEPlanServer(t, nil)
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	planID := "plan-roll-no-agent"
	opsJSON, _ := json.Marshal([]executor.Op{{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "commit wireless"}})
	if _, err := ts.db.Exec(
		`INSERT INTO orchestr_plans (id, router_id, resource, desired, diff, status, created_by, created_at, applied_at, result)
		 VALUES (?, ?, 'guestwifi', ?, ?, 'applied', 'admin', ?, ?, '{"status":"applied"}')`,
		planID, "flint2", `{"enabled":true,"allowNonGateway":false}`, string(opsJSON),
		time.Now().Unix(), time.Now().Unix()); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	// Sin agente conectado por SSE.
	code, raw := postRollback(t, ts.URL, cookie, planID)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("rollback sin agente: esperaba 503, got %d: %s", code, raw)
	}
	if got := planStatus(t, ts, planID); got != "applied" {
		t.Fatalf("un rollback no entregado no puede mover el plan, got %s", got)
	}
}
