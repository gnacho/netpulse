// scheduler_test.go — scheduler de programaciones (#494). El launcher es un
// fake que registra las llamadas; nunca se toca un router ni firmware real.
package firmware

import (
	"testing"
	"time"
)

// fakeLauncher registra los upgrades lanzados.
type fakeLauncher struct {
	launched []Upgrade
}

func (f *fakeLauncher) LaunchScheduled(u Upgrade) error {
	f.launched = append(f.launched, u)
	return nil
}

func schedulerWithNow(t *testing.T, now time.Time) (*Store, *Scheduler, *fakeLauncher) {
	t.Helper()
	s := open(t)
	l := &fakeLauncher{}
	sch := NewScheduler(s, l)
	sch.now = func() time.Time { return now }
	return s, sch, l
}

// TestSchedulerTickPicksDue (#494): recoge las vencidas y las lanza; la no
// vencida no se toca.
func TestSchedulerTickPicksDue(t *testing.T) {
	now := time.UnixMilli(5_000)
	s, sch, l := schedulerWithNow(t, now)

	if _, err := s.ScheduleUpgrade("due", "v2", "http://x", "a", 4_000); err != nil {
		t.Fatalf("schedule due: %v", err)
	}
	if _, err := s.ScheduleUpgrade("future", "v2", "http://x", "b", 9_000); err != nil {
		t.Fatalf("schedule future: %v", err)
	}

	sch.Tick()

	if len(l.launched) != 1 || l.launched[0].RouterID != "due" {
		t.Fatalf("esperaba solo 'due' lanzada, got %+v", l.launched)
	}
	// La lanzada ya no está scheduled (la transiciona el launcher real; aquí
	// el fake no muta el store, pero un segundo Tick no debe volver a listarla
	// porque sigue 'scheduled' en el store del fake). Se verifica el filtro en
	// DueScheduled por separado; aquí basta con que el tick no liste la futura.
}

// TestSchedulerTickIgnoresNonDue (#494): sin vencidas no se lanza nada.
func TestSchedulerTickIgnoresNonDue(t *testing.T) {
	now := time.UnixMilli(5_000)
	s, sch, l := schedulerWithNow(t, now)

	if _, err := s.ScheduleUpgrade("future", "v2", "http://x", "b", 9_000); err != nil {
		t.Fatalf("schedule future: %v", err)
	}
	sch.Tick()
	if len(l.launched) != 0 {
		t.Fatalf("nada debía lanzarse, got %+v", l.launched)
	}
}

// TestSchedulerStop (#494): Start lanza el bucle y Stop lo termina.
func TestSchedulerStop(t *testing.T) {
	s := open(t)
	l := &fakeLauncher{}
	sch := NewScheduler(s, l)
	done := make(chan struct{})
	go func() {
		sch.Start()
		close(done)
	}()
	sch.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop no terminó el bucle")
	}
}
