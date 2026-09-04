// store_test.go — contrato del store de firmware (issue #519: descarte del
// último intento fallido).
package firmware

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func open(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return NewStore(d.DB)
}

func TestDismissLatestFailed(t *testing.T) {
	s := open(t)
	id, err := s.BeginUpgrade("rt1", "v2", "http://x", "abc")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := s.SetStatus(id, "failed", "agent not connected", ""); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	up, _ := s.LatestUpgrade("rt1")
	if up == nil || up.Status != "failed" {
		t.Fatalf("esperaba upgrade failed, got %+v", up)
	}
	if err := s.DismissLatest("rt1"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	up, _ = s.LatestUpgrade("rt1")
	if up != nil {
		t.Fatalf("tras dismiss no debería haber upgrade: %+v", up)
	}
}

func TestDismissLatestKeepsRunning(t *testing.T) {
	s := open(t)
	id, err := s.BeginUpgrade("rt1", "v2", "http://x", "abc")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// Estado en curso (requested): el descarte no debe tocarlo.
	if err := s.DismissLatest("rt1"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	up, _ := s.LatestUpgrade("rt1")
	if up == nil || up.ID != id {
		t.Fatalf("un upgrade en curso debe sobrevivir al dismiss: %+v", up)
	}
	// Router sin upgrades: no debe fallar.
	if err := s.DismissLatest("no-existe"); err != nil {
		t.Fatalf("dismiss sin upgrades: %v", err)
	}
}

// TestScheduleUpgradeUpsert (#494): una programación por router; reprogramar
// actualiza la hora y no crea una segunda fila.
func TestScheduleUpgradeUpsert(t *testing.T) {
	s := open(t)
	id1, err := s.ScheduleUpgrade("rt1", "v2", "http://x", "abc", 1000)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	up, _ := s.LatestUpgrade("rt1")
	if up == nil || up.Status != "scheduled" {
		t.Fatalf("esperaba fila scheduled, got %+v", up)
	}
	if up.ScheduledFor == nil || *up.ScheduledFor != 1000 {
		t.Fatalf("scheduled_for = %v, want 1000", up.ScheduledFor)
	}
	id2, err := s.ScheduleUpgrade("rt1", "v3", "http://y", "def", 2000)
	if err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("reprogramar debe reutilizar la fila: id1=%d id2=%d", id1, id2)
	}
	up, _ = s.LatestUpgrade("rt1")
	if up == nil || *up.ScheduledFor != 2000 || up.TargetVersion != "v3" {
		t.Fatalf("reschedule no aplicó: %+v", up)
	}
	// Una sola fila.
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM firmware_upgrades WHERE router_id='rt1'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("esperaba 1 fila, got %d", n)
	}
}

// TestCancelScheduled (#494): borra solo la programación pendiente; un
// upgrade en curso (requested) no se toca.
func TestCancelScheduled(t *testing.T) {
	s := open(t)
	if _, err := s.ScheduleUpgrade("rt1", "v2", "http://x", "abc", 1000); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := s.CancelScheduled("rt1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if up, _ := s.LatestUpgrade("rt1"); up != nil {
		t.Fatalf("tras cancelar no debería haber fila: %+v", up)
	}
	// Cancelar sin programación: no-op sin error.
	if err := s.CancelScheduled("rt1"); err != nil {
		t.Fatalf("cancel sin fila: %v", err)
	}
}

// TestDueScheduled (#494): solo las vencidas y en orden de hora.
func TestDueScheduled(t *testing.T) {
	s := open(t)
	if _, err := s.ScheduleUpgrade("rt1", "v2", "http://x", "a", 3000); err != nil {
		t.Fatalf("schedule rt1: %v", err)
	}
	if _, err := s.ScheduleUpgrade("rt2", "v2", "http://x", "b", 1000); err != nil {
		t.Fatalf("schedule rt2: %v", err)
	}
	if _, err := s.ScheduleUpgrade("rt3", "v2", "http://x", "c", 9000); err != nil {
		t.Fatalf("schedule rt3: %v", err)
	}
	due, err := s.DueScheduled(5000)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 2 {
		t.Fatalf("esperaba 2 vencidas, got %d", len(due))
	}
	if due[0].RouterID != "rt2" || due[1].RouterID != "rt1" {
		t.Fatalf("orden de vencidas incorrecto: %v, %v", due[0].RouterID, due[1].RouterID)
	}
}

// TestStartScheduled (#494): transiciona scheduled→requested una sola vez.
func TestStartScheduled(t *testing.T) {
	s := open(t)
	id, err := s.ScheduleUpgrade("rt1", "v2", "http://x", "abc", 1000)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	ok, err := s.StartScheduled(id)
	if err != nil || !ok {
		t.Fatalf("start: ok=%v err=%v", ok, err)
	}
	up, _ := s.GetUpgradeByID(id)
	if up == nil || up.Status != "requested" {
		t.Fatalf("esperaba requested, got %+v", up)
	}
	// Segunda transición: guard devuelve false (ya no está scheduled).
	ok, err = s.StartScheduled(id)
	if err != nil || ok {
		t.Fatalf("segundo start debe ser no-op: ok=%v err=%v", ok, err)
	}
}
