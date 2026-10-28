// orchestr_apply_result_test.go — ciclo completo de un plan real:
// POST /api/plans -> apply (marca applying ANTES de enviar) -> el agente
// reporta por POST /api/agents/{slug}/apply-result -> applied / rolled_back,
// y los caminos honestos cuando el plan no existe o el envío SSE falla.
package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
)

// planRow es el estado persistido de un plan.
type planRow struct {
	Status    string
	Resource  string
	CreatedBy string
	Desired   string
	Diff      string
	AppliedAt *int64
	Result    *string
}

func readPlan(t *testing.T, ts *testServer, id string) planRow {
	t.Helper()
	var p planRow
	if err := ts.db.QueryRow(
		`SELECT status, resource, created_by, desired, diff, applied_at, result
		 FROM orchestr_plans WHERE id = ?`, id).Scan(
		&p.Status, &p.Resource, &p.CreatedBy, &p.Desired, &p.Diff, &p.AppliedAt, &p.Result); err != nil {
		t.Fatalf("select plan %s: %v", id, err)
	}
	return p
}

// createPlanViaAPI crea un plan real con diff explícito (sin probe SSH).
func createPlanViaAPI(t *testing.T, base, cookie, routerID, resource string, ops []executor.Op) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"routerId": routerID,
		"resource": resource,
		"diff":     ops,
	})
	req, _ := http.NewRequest("POST", base+"/api/plans", bytes.NewReader(body))
	req.Header.Set("Cookie", "session="+cookie)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create plan: esperaba 201, got %d: %s", res.StatusCode, raw)
	}
	var plan struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("plan JSON: %v (%s)", err, raw)
	}
	if plan.ID == "" {
		t.Fatalf("el plan creado llegó sin id: %s", raw)
	}
	return plan.ID
}

// createPlanFromDesired crea un plan real dejando que el módulo calcule el
// diff (el probe SSH es fakeProbeSSH, sin red), de modo que desired y diff
// quedan persistidos y el rollback puede invertirse.
func createPlanFromDesired(t *testing.T, base, cookie, routerID, resource string, desired any) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"routerId": routerID,
		"resource": resource,
		"desired":  desired,
	})
	req, _ := http.NewRequest("POST", base+"/api/plans", bytes.NewReader(body))
	req.Header.Set("Cookie", "session="+cookie)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create plan: esperaba 201, got %d: %s", res.StatusCode, raw)
	}
	var plan struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("plan JSON: %v (%s)", err, raw)
	}
	if plan.ID == "" {
		t.Fatalf("el plan creado llegó sin id: %s", raw)
	}
	return plan.ID
}

func postApply(t *testing.T, base, cookie, planID string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/api/plans/"+planID+"/apply", nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func postRollback(t *testing.T, base, cookie, planID string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/api/plans/"+planID+"/rollback", nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

// tryPostApplyResult hace el POST sin tocar *testing.T (se usa desde la
// goroutine del hook del stream).
func tryPostApplyResult(base, slug, token, planID string, result executor.ApplyResult) (int, []byte, error) {
	body, err := json.Marshal(map[string]any{"planId": planID, "result": result})
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest("POST", base+"/api/agents/"+slug+"/apply-result", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, raw, nil
}

// postApplyResult es lo que hace el agente al terminar de aplicar las ops.
func postApplyResult(t *testing.T, base, slug, token, planID string, result executor.ApplyResult) (int, []byte) {
	t.Helper()
	code, raw, err := tryPostApplyResult(base, slug, token, planID, result)
	if err != nil {
		t.Fatalf("apply-result: %v", err)
	}
	return code, raw
}

// waitPlanStatus espera (con deadline) a que el plan llegue a un estado.
func waitPlanStatus(t *testing.T, ts *testServer, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := planStatus(t, ts, id)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("plan %s: esperaba %q, sigue en %q", id, want, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func auditActionsOf(t *testing.T, ts *testServer, planID string) []string {
	t.Helper()
	rows, err := ts.db.Query(`SELECT action FROM orchestr_audit WHERE plan_id = ? ORDER BY id`, planID)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		out = append(out, a)
	}
	return out
}

func containsAction(actions []string, want string) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}

// TestApplyResultMarksPlanApplied: el agente reporta el resultado y el plan
// queda applied con applied_at y el resultado persistidos.
func TestApplyResultMarksPlanApplied(t *testing.T) {
	ts, _ := newSSEPlanServer(t, nil)
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")
	setAgentToken(t, ts, "flint2", "agent-token")

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	planID := createPlanViaAPI(t, ts.URL, cookie, "flint2", "guestwifi", []executor.Op{
		{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "commit wireless"},
	})

	code, raw := postApplyResult(t, ts.URL, "flint2", "agent-token", planID, executor.ApplyResult{
		Status: "applied", DurationMs: 12,
	})
	if code != http.StatusOK {
		t.Fatalf("apply-result: esperaba 200, got %d: %s", code, raw)
	}

	p := readPlan(t, ts, planID)
	if p.Status != "applied" {
		t.Fatalf("estado del plan: got %q, want applied", p.Status)
	}
	if p.AppliedAt == nil {
		t.Error("el plan aplicado debe llevar applied_at")
	}
	if p.Result == nil || !bytes.Contains([]byte(*p.Result), []byte(`"applied"`)) {
		t.Errorf("el resultado del agente debe persistirse, got %v", p.Result)
	}
	if acts := auditActionsOf(t, ts, planID); !containsAction(acts, "applied") {
		t.Errorf("se esperaba auditoría \"applied\", got %v", acts)
	}
}

// TestApplyResultUnknownPlanIsNotFound: un plan inexistente es un error del
// cliente (404), no un 500 de "update_error" que el agente reintenta diez veces.
func TestApplyResultUnknownPlanIsNotFound(t *testing.T) {
	ts, _ := newSSEPlanServer(t, nil)
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")
	setAgentToken(t, ts, "flint2", "agent-token")

	code, raw := postApplyResult(t, ts.URL, "flint2", "agent-token", "plan-que-no-existe",
		executor.ApplyResult{Status: "applied"})
	if code != http.StatusNotFound {
		t.Fatalf("apply-result con plan inexistente: esperaba 404, got %d: %s", code, raw)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("cuerpo de error: %v (%s)", err, raw)
	}
	if body.Error != "not_found" {
		t.Errorf("código de error: got %q, want not_found", body.Error)
	}
}

// TestApplyResultRejectsBadAgentToken: el token del agente se sigue exigiendo.
func TestApplyResultRejectsBadAgentToken(t *testing.T) {
	ts, _ := newSSEPlanServer(t, nil)
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")
	setAgentToken(t, ts, "flint2", "agent-token")

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	planID := createPlanViaAPI(t, ts.URL, cookie, "flint2", "guestwifi", []executor.Op{
		{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "commit wireless"},
	})

	code, raw := postApplyResult(t, ts.URL, "flint2", "token-equivocado", planID,
		executor.ApplyResult{Status: "applied"})
	if code != http.StatusUnauthorized {
		t.Fatalf("apply-result con token inválido: esperaba 401, got %d: %s", code, raw)
	}
	if got := planStatus(t, ts, planID); got != "pending" {
		t.Fatalf("un token inválido no puede tocar el plan, got %s", got)
	}
}

// TestPlanLifecycleApplyAndRollbackReportedByAgent recorre el ciclo completo
// con un plan real creado por la API: apply-result lo deja applied, el
// rollback manda las ops inversas por SSE y el nuevo apply-result lo cierra
// como rolled_back (no se queda en rolling_back).
func TestPlanLifecycleApplyAndRollbackReportedByAgent(t *testing.T) {
	ts, _ := newSSEPlanServer(t, nil)
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")
	setAgentToken(t, ts, "flint2", "agent-token")

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	events := connectAgentStream(t, ts.URL, "flint2")

	planID := createPlanFromDesired(t, ts.URL, cookie, "flint2", "guestwifi", map[string]any{
		"enabled": true, "ssid": "NetPulse-Guest", "allowNonGateway": false,
	})

	if code, raw := postApply(t, ts.URL, cookie, planID); code != http.StatusAccepted {
		t.Fatalf("apply: esperaba 202, got %d: %s", code, raw)
	}
	ev := waitAgentEvent(t, events, "apply")
	if !bytes.Contains([]byte(ev.data), []byte(planID)) {
		t.Fatalf("el evento SSE no lleva el plan real: %s", ev.data)
	}
	if code, raw := postApplyResult(t, ts.URL, "flint2", "agent-token", planID,
		executor.ApplyResult{Status: "applied"}); code != http.StatusOK {
		t.Fatalf("apply-result: esperaba 200, got %d: %s", code, raw)
	}
	if got := planStatus(t, ts, planID); got != "applied" {
		t.Fatalf("tras el apply-result: esperaba applied, got %s", got)
	}

	if code, raw := postRollback(t, ts.URL, cookie, planID); code != http.StatusAccepted {
		t.Fatalf("rollback: esperaba 202, got %d: %s", code, raw)
	}
	ev = waitAgentEvent(t, events, "apply")
	var payload struct {
		PlanID string        `json:"plan_id"`
		Ops    []executor.Op `json:"ops"`
	}
	if err := json.Unmarshal([]byte(ev.data), &payload); err != nil {
		t.Fatalf("payload SSE del rollback: %v (%s)", err, ev.data)
	}
	if payload.PlanID != planID {
		t.Errorf("plan_id del rollback: got %q, want %q", payload.PlanID, planID)
	}
	if got := planStatus(t, ts, planID); got != "rolling_back" {
		t.Fatalf("el rollback en curso debe quedar rolling_back, got %s", got)
	}

	// El agente termina el rollback y reporta: el plan NO se queda colgado.
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

// TestApplyMarksApplyingBeforeAgentSeesEvent: con un agente que responde al
// instante (el apply-result entra mientras el servidor aún está escribiendo el
// evento), el plan no se queda en applying: el estado applying se fija ANTES
// de enviar, así que SetResult no pisa un resultado que ya llegó.
func TestApplyMarksApplyingBeforeAgentSeesEvent(t *testing.T) {
	var (
		mu       sync.Mutex
		planID   string
		base     string
		hookCode int
		hookBody []byte
		hookErr  error
		hooks    int
	)
	onApply := func() {
		mu.Lock()
		id, url := planID, base
		hooks++
		mu.Unlock()
		if id == "" || url == "" {
			return
		}
		code, raw, err := tryPostApplyResult(url, "flint2", "agent-token", id,
			executor.ApplyResult{Status: "applied", DurationMs: 3})
		mu.Lock()
		hookCode, hookBody, hookErr = code, raw, err
		mu.Unlock()
	}

	ts, _ := newSSEPlanServer(t, nil, withHandlerWrap(withStreamHook(streamHookFastAgent, onApply)))
	base = ts.URL
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")
	setAgentToken(t, ts, "flint2", "agent-token")

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	connectAgentStream(t, ts.URL, "flint2")

	created := createPlanViaAPI(t, ts.URL, cookie, "flint2", "guestwifi", []executor.Op{
		{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "commit wireless"},
	})
	mu.Lock()
	planID = created
	mu.Unlock()

	if code, raw := postApply(t, ts.URL, cookie, planID); code != http.StatusAccepted {
		t.Fatalf("apply: esperaba 202, got %d: %s", code, raw)
	}

	mu.Lock()
	got := hooks
	code, body, err := hookCode, hookBody, hookErr
	mu.Unlock()
	if got == 0 {
		t.Fatal("el agente rápido no llegó a responder dentro del envío")
	}
	if err != nil {
		t.Fatalf("apply-result del agente rápido: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("apply-result del agente rápido: esperaba 200, got %d: %s", code, body)
	}
	// El resultado del agente es el último estado; nunca se queda en applying.
	waitPlanStatus(t, ts, planID, "applied")
}

// TestApplyRestoresPendingWhenSSEDeliveryFails: si la escritura del evento
// falla, el plan vuelve a pending (estado honesto), se audita la
// restauración y la respuesta es un 503, nunca un 202 de éxito.
func TestApplyRestoresPendingWhenSSEDeliveryFails(t *testing.T) {
	ts, _ := newSSEPlanServer(t, nil, withHandlerWrap(withStreamHook(streamHookFailOnApply, nil)))
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	connectAgentStream(t, ts.URL, "flint2")

	planID := createPlanViaAPI(t, ts.URL, cookie, "flint2", "guestwifi", []executor.Op{
		{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "commit wireless"},
	})

	code, raw := postApply(t, ts.URL, cookie, planID)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("apply con el stream roto: esperaba 503, got %d: %s", code, raw)
	}
	if got := planStatus(t, ts, planID); got != "pending" {
		t.Fatalf("tras fallar el envío el plan debe volver a pending, got %s", got)
	}
	if acts := auditActionsOf(t, ts, planID); !containsAction(acts, "restore") {
		t.Errorf("la restauración debe quedar auditada, got %v", acts)
	}
}

// TestApplyKeepsPlanPendingWithoutAgent: sin agente conectado no hay plan en
// curso ni 202: el plan sigue pendiente y se puede reintentar.
func TestApplyKeepsPlanPendingWithoutAgent(t *testing.T) {
	ts, _ := newSSEPlanServer(t, nil)
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	planID := createPlanViaAPI(t, ts.URL, cookie, "flint2", "guestwifi", []executor.Op{
		{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "commit wireless"},
	})

	code, raw := postApply(t, ts.URL, cookie, planID)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("apply sin agente: esperaba 503, got %d: %s", code, raw)
	}
	if got := planStatus(t, ts, planID); got != "pending" {
		t.Fatalf("sin agente el plan debe seguir pending, got %s", got)
	}
}

// TestRollbackRestoresAppliedWhenSSEDeliveryFails: si el envío del rollback
// falla, el plan vuelve a applied (no se queda rolling_back para siempre).
func TestRollbackRestoresAppliedWhenSSEDeliveryFails(t *testing.T) {
	// La primera entrega (apply) va bien; la segunda (rollback) se rompe.
	ts, _ := newSSEPlanServer(t, nil, withHandlerWrap(withStreamHookFailAt(streamHookFailOnApply, nil, 2)))
	addPlanRouter(t, ts, "flint2", "192.0.2.10", true, "")
	setAgentToken(t, ts, "flint2", "agent-token")

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	events := connectAgentStream(t, ts.URL, "flint2")

	planID := createPlanFromDesired(t, ts.URL, cookie, "flint2", "guestwifi", map[string]any{
		"enabled": true, "ssid": "NetPulse-Guest", "allowNonGateway": false,
	})
	if code, raw := postApply(t, ts.URL, cookie, planID); code != http.StatusAccepted {
		t.Fatalf("apply: esperaba 202, got %d: %s", code, raw)
	}
	waitAgentEvent(t, events, "apply")
	if code, raw := postApplyResult(t, ts.URL, "flint2", "agent-token", planID,
		executor.ApplyResult{Status: "applied"}); code != http.StatusOK {
		t.Fatalf("apply-result: got %d: %s", code, raw)
	}
	if got := planStatus(t, ts, planID); got != "applied" {
		t.Fatalf("tras el apply: esperaba applied, got %s", got)
	}

	code, raw := postRollback(t, ts.URL, cookie, planID)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("rollback con el stream roto: esperaba 503, got %d: %s", code, raw)
	}
	if got := planStatus(t, ts, planID); got != "applied" {
		t.Fatalf("tras fallar el envío el plan debe volver a applied, got %s", got)
	}
	if acts := auditActionsOf(t, ts, planID); !containsAction(acts, "restore") {
		t.Errorf("la restauración debe quedar auditada, got %v", acts)
	}
}
