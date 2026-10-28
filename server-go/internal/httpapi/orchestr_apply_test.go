// orchestr_apply_test.go — tests del endpoint /api/orchestr/apply (#451) con
// el contrato F-06: las ops (ya envueltas con ownership) van al agente por
// SSE y no hay ninguna petición HTTP al executor de NetGrip.
package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/gnacho/netpulse/agent/executor"
)

func TestOrchestrApplyCreatesAndAppliesPlan(t *testing.T) {
	ts, _ := newSSEPlanServer(t, nil)
	netgripMock, hits := countingNetGripMock(t)
	addPlanRouter(t, ts, "flint2", netgripMock.Listener.Addr().String(), true, "netgrip-secret")

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	events := connectAgentStream(t, ts.URL, "flint2")

	ops := []executor.Op{{
		Kind: "uci_set",
		Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "1.1.1.1"},
		Desc: "set dns",
	}}
	body, _ := json.Marshal(map[string]any{
		"routerId": "flint2",
		"resource": "guestwifi",
		"diff":     ops,
	})

	req, _ := http.NewRequest("POST", ts.URL+"/api/orchestr/apply", bytes.NewReader(body))
	req.Header.Set("Cookie", "session="+cookie)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("apply: esperaba 202, got %d: %s", res.StatusCode, string(b))
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Fatalf("NetGrip recibió %d peticiones HTTP; el apply debe ir por SSE", got)
	}

	// El agente recibe el plan completo, con las ops de ownership delante.
	ev := waitAgentEvent(t, events, "apply")
	var payload struct {
		PlanID string        `json:"plan_id"`
		Ops    []executor.Op `json:"ops"`
	}
	if err := json.Unmarshal([]byte(ev.data), &payload); err != nil {
		t.Fatalf("payload SSE apply inválido (%q): %v", ev.data, err)
	}
	if len(payload.Ops) < 3 {
		t.Fatalf("el agente debería recibir al menos 3 ops, got %d", len(payload.Ops))
	}
	if payload.Ops[0].Kind != "ownership_mode" || payload.Ops[0].Args["enforce"] != "true" {
		t.Errorf("op[0] debería ser ownership_mode enforce=true, got %+v", payload.Ops[0])
	}
	if payload.Ops[1].Kind != "uci_set_managed" {
		t.Errorf("op[1] debería ser uci_set_managed, got %+v", payload.Ops[1])
	}

	var planID string
	if err := ts.db.QueryRow("SELECT id FROM orchestr_plans ORDER BY created_at DESC LIMIT 1").Scan(&planID); err != nil {
		t.Fatalf("select plan: %v", err)
	}
	if payload.PlanID != planID {
		t.Errorf("plan_id SSE: got %q, want %q", payload.PlanID, planID)
	}
	if got := planStatus(t, ts, planID); got != "applying" {
		t.Fatalf("plan status: esperaba applying, got %s", got)
	}
}
