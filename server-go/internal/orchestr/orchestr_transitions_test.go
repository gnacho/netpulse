// orchestr_transitions_test.go: ciclo de vida del plan. Cada transición se
// valida contra el estado de partida y deja auditoría. Si el envío por SSE
// falla, el handler restaura el estado anterior con una transición condicional
// (RestoreTransition), nunca con un UPDATE ciego que pise el resultado que el
// agente ya hubiera reportado.
package orchestr

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func newTestManager(t *testing.T) (*Manager, *db.DB) {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return New(d), d
}

func newTestPlan(t *testing.T, m *Manager) *Plan {
	t.Helper()
	plan, err := m.CreatePlan("flint2", "guestwifi", json.RawMessage(`{"enabled":true}`),
		[]executor.Op{{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "commit wireless"}},
		"admin")
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return plan
}

func auditActions(t *testing.T, d *db.DB, planID string) []string {
	t.Helper()
	rows, err := d.Query(`SELECT action FROM orchestr_audit WHERE plan_id = ? ORDER BY id`, planID)
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

func requireStatus(t *testing.T, m *Manager, planID, want string) {
	t.Helper()
	p, err := m.GetPlan(planID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if p.Status != want {
		t.Fatalf("estado del plan: got %q, want %q", p.Status, want)
	}
}

func TestSetApplyingOnlyFromPending(t *testing.T) {
	m, d := newTestManager(t)
	plan := newTestPlan(t, m)

	if err := m.SetApplying(plan.ID, "admin"); err != nil {
		t.Fatalf("SetApplying desde pending: %v", err)
	}
	requireStatus(t, m, plan.ID, "applying")
	if got := auditActions(t, d, plan.ID); len(got) == 0 || got[len(got)-1] != "applying" {
		t.Errorf("se esperaba una entrada de auditoría \"applying\", got %v", got)
	}

	// Repetir la transición sobre un plan que ya está en marcha debe fallar
	// (no es un UPDATE a ciegas) y no debe mover el estado.
	err := m.SetApplying(plan.ID, "admin")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("SetApplying sobre applying: esperaba ErrInvalidTransition, got %v", err)
	}
	requireStatus(t, m, plan.ID, "applying")
}

func TestSetApplyingRejectsAppliedPlan(t *testing.T) {
	m, _ := newTestManager(t)
	plan := newTestPlan(t, m)
	if err := m.SetApplying(plan.ID, "admin"); err != nil {
		t.Fatalf("SetApplying: %v", err)
	}
	if err := m.SetResult(plan.ID, ApplyResult{Status: "applied"}); err != nil {
		t.Fatalf("SetResult: %v", err)
	}
	requireStatus(t, m, plan.ID, "applied")

	if err := m.SetApplying(plan.ID, "admin"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("SetApplying sobre applied: esperaba ErrInvalidTransition, got %v", err)
	}
	requireStatus(t, m, plan.ID, "applied")
}

func TestSetRollingBackOnlyFromApplied(t *testing.T) {
	m, d := newTestManager(t)
	plan := newTestPlan(t, m)

	// pending no se puede revertir.
	if err := m.SetRollingBack(plan.ID, "admin"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("SetRollingBack sobre pending: esperaba ErrInvalidTransition, got %v", err)
	}
	requireStatus(t, m, plan.ID, "pending")

	if err := m.SetApplying(plan.ID, "admin"); err != nil {
		t.Fatalf("SetApplying: %v", err)
	}
	if err := m.SetResult(plan.ID, ApplyResult{Status: "applied"}); err != nil {
		t.Fatalf("SetResult: %v", err)
	}
	if err := m.SetRollingBack(plan.ID, "admin"); err != nil {
		t.Fatalf("SetRollingBack desde applied: %v", err)
	}
	requireStatus(t, m, plan.ID, "rolling_back")
	actions := auditActions(t, d, plan.ID)
	if actions[len(actions)-1] != "rollback" {
		t.Errorf("se esperaba auditoría \"rollback\", got %v", actions)
	}

	// Repetir el rollback no puede volver a mover el estado.
	if err := m.SetRollingBack(plan.ID, "admin"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("SetRollingBack repetido: esperaba ErrInvalidTransition, got %v", err)
	}
	requireStatus(t, m, plan.ID, "rolling_back")
}

func TestRestoreTransitionOnlyRevertsMatchingState(t *testing.T) {
	m, d := newTestManager(t)
	plan := newTestPlan(t, m)
	if err := m.SetApplying(plan.ID, "admin"); err != nil {
		t.Fatalf("SetApplying: %v", err)
	}

	// El envío por SSE falló: se deshace la transición y queda pendiente.
	if err := m.RestoreTransition(plan.ID, "applying", "pending", "admin", "sse delivery failed"); err != nil {
		t.Fatalf("RestoreTransition: %v", err)
	}
	requireStatus(t, m, plan.ID, "pending")
	if got := auditActions(t, d, plan.ID); got[len(got)-1] != "restore" {
		t.Errorf("se esperaba auditoría \"restore\", got %v", got)
	}

	// Ya no hay nada que restaurar: repetirlo es un error, no un no-op ciego.
	if err := m.RestoreTransition(plan.ID, "applying", "pending", "admin", "retry"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("RestoreTransition repetido: esperaba ErrInvalidTransition, got %v", err)
	}
	requireStatus(t, m, plan.ID, "pending")
}

func TestRestoreTransitionDoesNotClobberAgentResult(t *testing.T) {
	m, _ := newTestManager(t)
	plan := newTestPlan(t, m)
	if err := m.SetApplying(plan.ID, "admin"); err != nil {
		t.Fatalf("SetApplying: %v", err)
	}
	// El agente responde antes de que el handler se dé cuenta de que el
	// envío falló: el resultado manda y la restauración no puede pisarlo.
	if err := m.SetResult(plan.ID, ApplyResult{Status: "applied"}); err != nil {
		t.Fatalf("SetResult: %v", err)
	}
	if err := m.RestoreTransition(plan.ID, "applying", "pending", "admin", "sse delivery failed"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("RestoreTransition tras applied: esperaba ErrInvalidTransition, got %v", err)
	}
	requireStatus(t, m, plan.ID, "applied")
}

func TestSetResultMapsStatesHonestly(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(m *Manager, id string) error
		result  ApplyResult
		want    string
	}{
		{
			name:    "applying + applied -> applied",
			prepare: func(m *Manager, id string) error { return m.SetApplying(id, "admin") },
			result:  ApplyResult{Status: "applied"},
			want:    "applied",
		},
		{
			name:    "applying + failed -> failed",
			prepare: func(m *Manager, id string) error { return m.SetApplying(id, "admin") },
			result:  ApplyResult{Status: "failed", Error: "uci_commit exit 1"},
			want:    "failed",
		},
		{
			name: "rolling_back + applied -> rolled_back",
			prepare: func(m *Manager, id string) error {
				if err := m.SetApplying(id, "admin"); err != nil {
					return err
				}
				if err := m.SetResult(id, ApplyResult{Status: "applied"}); err != nil {
					return err
				}
				return m.SetRollingBack(id, "admin")
			},
			result: ApplyResult{Status: "applied"},
			want:   "rolled_back",
		},
		{
			name: "rolling_back + rolled_back -> rolled_back",
			prepare: func(m *Manager, id string) error {
				if err := m.SetApplying(id, "admin"); err != nil {
					return err
				}
				if err := m.SetResult(id, ApplyResult{Status: "applied"}); err != nil {
					return err
				}
				return m.SetRollingBack(id, "admin")
			},
			result: ApplyResult{Status: "rolled_back"},
			want:   "rolled_back",
		},
		{
			name: "rolling_back + failed -> failed",
			prepare: func(m *Manager, id string) error {
				if err := m.SetApplying(id, "admin"); err != nil {
					return err
				}
				if err := m.SetResult(id, ApplyResult{Status: "applied"}); err != nil {
					return err
				}
				return m.SetRollingBack(id, "admin")
			},
			result: ApplyResult{Status: "failed", Error: "uci rollback falló"},
			want:   "failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newTestManager(t)
			plan := newTestPlan(t, m)
			if err := tc.prepare(m, plan.ID); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if err := m.SetResult(plan.ID, tc.result); err != nil {
				t.Fatalf("SetResult: %v", err)
			}
			requireStatus(t, m, plan.ID, tc.want)
			p, err := m.GetPlan(plan.ID)
			if err != nil {
				t.Fatalf("get plan: %v", err)
			}
			if p.AppliedAt == nil {
				t.Error("el resultado debe sellar applied_at")
			}
			if p.Result == nil || p.Result.Status != tc.result.Status {
				t.Errorf("el resultado del agente debe persistirse, got %+v", p.Result)
			}
		})
	}
}

func TestSetResultUnknownPlan(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.SetResult("no-existe", ApplyResult{Status: "applied"}); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("SetResult con plan inexistente: esperaba ErrPlanNotFound, got %v", err)
	}
}

func TestSetResultKeepsRolledBackTerminal(t *testing.T) {
	m, _ := newTestManager(t)
	plan := newTestPlan(t, m)
	if err := m.SetApplying(plan.ID, "admin"); err != nil {
		t.Fatalf("SetApplying: %v", err)
	}
	if err := m.SetResult(plan.ID, ApplyResult{Status: "applied"}); err != nil {
		t.Fatalf("SetResult: %v", err)
	}
	if err := m.SetRollingBack(plan.ID, "admin"); err != nil {
		t.Fatalf("SetRollingBack: %v", err)
	}
	if err := m.SetResult(plan.ID, ApplyResult{Status: "applied"}); err != nil {
		t.Fatalf("SetResult (rollback): %v", err)
	}
	requireStatus(t, m, plan.ID, "rolled_back")

	// Reintento tardío del apply-result del apply original: no revive el plan.
	if err := m.SetResult(plan.ID, ApplyResult{Status: "applied"}); err != nil {
		t.Fatalf("SetResult tardío: %v", err)
	}
	requireStatus(t, m, plan.ID, "rolled_back")
}
