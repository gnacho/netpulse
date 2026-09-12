// autosched_test.go - tests del auto-update programado (#759): vencimiento
// daily/weekly/monthly, validación, roundtrip kv y disparo con fakes.
package updater

import (
	"context"
	"testing"
	"time"
)

func autoIntPtr(v int) *int { return &v }

func TestAutoUpdateDueDaily(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.Local) // domingo
	slot := time.Date(2026, 9, 13, 3, 30, 0, 0, time.Local)
	st := AutoUpdateSettings{Enabled: true, Kind: AutoDaily, Time: "03:30"}
	if !AutoUpdateDue(st, 0, now) {
		t.Fatalf("slot pasado sin lastRun debe disparar")
	}
	if AutoUpdateDue(st, slot.UnixMilli(), now) {
		t.Fatalf("ya disparado hoy no repite")
	}
	if AutoUpdateDue(st, 0, slot.Add(-time.Minute)) {
		t.Fatalf("antes del slot no dispara")
	}
	if AutoUpdateDue(AutoUpdateSettings{Kind: AutoDaily, Time: "03:30"}, 0, now) {
		t.Fatalf("disabled no dispara")
	}
}

func TestAutoUpdateDueWeeklyMonthly(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.Local) // domingo 10:00
	weekly := AutoUpdateSettings{Enabled: true, Kind: AutoWeekly, DayOfWeek: autoIntPtr(0), Time: "04:00"}
	if !AutoUpdateDue(weekly, 0, now) {
		t.Fatalf("domingo despues del slot debe disparar")
	}
	weeklyTue := AutoUpdateSettings{Enabled: true, Kind: AutoWeekly, DayOfWeek: autoIntPtr(2), Time: "04:00"}
	if AutoUpdateDue(weeklyTue, 0, now) {
		t.Fatalf("otro dia no dispara")
	}
	monthly := AutoUpdateSettings{Enabled: true, Kind: AutoMonthly, DayOfMonth: autoIntPtr(13), Time: "04:00"}
	if !AutoUpdateDue(monthly, 0, now) {
		t.Fatalf("dia 13 despues del slot debe disparar")
	}
	clampFeb := AutoUpdateSettings{Enabled: true, Kind: AutoMonthly, DayOfMonth: autoIntPtr(31), Time: "04:00"}
	if !AutoUpdateDue(clampFeb, 0, time.Date(2026, 2, 28, 5, 0, 0, 0, time.Local)) {
		t.Fatalf("clamp 31->28 en febrero dispara")
	}
}

func TestValidateAutoUpdate(t *testing.T) {
	if err := ValidateAutoUpdate(AutoUpdateSettings{Kind: AutoDaily}); err == nil {
		t.Fatalf("daily sin time falla")
	}
	if err := ValidateAutoUpdate(AutoUpdateSettings{Kind: AutoWeekly, DayOfWeek: autoIntPtr(1)}); err == nil {
		t.Fatalf("weekly sin time falla")
	}
	if err := ValidateAutoUpdate(AutoUpdateSettings{Kind: "hourly"}); err == nil {
		t.Fatalf("kind desconocido falla")
	}
	if err := ValidateAutoUpdate(AutoUpdateSettings{Kind: AutoMonthly, DayOfMonth: autoIntPtr(1), Time: "04:00"}); err != nil {
		t.Fatalf("monthly valido: %v", err)
	}
}

func TestAutoUpdateSettingsRoundtrip(t *testing.T) {
	db := openDB(t)
	st := AutoUpdateSettings{Enabled: true, Kind: AutoWeekly, DayOfWeek: autoIntPtr(4), Time: "02:15"}
	if err := SaveAutoUpdateSettings(db, st); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := LoadAutoUpdateSettings(db)
	if !got.Enabled || got.Kind != AutoWeekly || got.DayOfWeek == nil || *got.DayOfWeek != 4 || got.Time != "02:15" {
		t.Fatalf("roundtrip: %+v", got)
	}
	if err := SaveAutoUpdateSettings(db, AutoUpdateSettings{Enabled: true, Kind: "nope"}); err == nil {
		t.Fatalf("guardar basura debe fallar")
	}
	if _, r := AutoRunState(db); r != "" {
		t.Fatalf("sin disparos lastResult vacío, got %q", r)
	}
}

// fakeUA: CheckerApplier scripted.
type fakeUA struct {
	status    Status
	applied   int
	applyRet  bool
	applyHook func()
}

func (f *fakeUA) Check(ctx context.Context) Status { return f.status }
func (f *fakeUA) CanApply() bool                   { return f.status.CanApply }
func (f *fakeUA) ApplyBy(by string) bool {
	f.applied++
	if f.applyHook != nil {
		f.applyHook()
	}
	return f.applyRet
}

func TestSchedulerShotAppliesWhenDue(t *testing.T) {
	db := openDB(t)
	latest := "v9.9.9"
	ua := &fakeUA{
		status:   Status{UpdateAvailable: true, CanApply: true, Latest: &latest},
		applyRet: true,
	}
	s := NewScheduler(db, ua)
	s.now = func() time.Time { return time.Date(2026, 9, 13, 10, 0, 0, 0, time.Local) }
	_ = AutoUpdateSettings{Enabled: true, Kind: AutoDaily, Time: "03:30"}
	s.shot(AutoUpdateSettings{}, autoRunState{})
	if ua.applied != 1 {
		t.Fatalf("debe aplicar: %d", ua.applied)
	}
	if _, res := AutoRunState(db); res != "applied" {
		t.Fatalf("lastResult: %q", res)
	}
	lr, _ := AutoRunState(db)
	if lr == 0 {
		t.Fatalf("lastRun debe quedar marcado")
	}
}

func TestSchedulerShotCheckFailed(t *testing.T) {
	// #743: estado rancio tras fetch fallido: NUNCA aplicar sobre él.
	db := openDB(t)
	ua := &fakeUA{status: Status{CheckFailed: true, UpdateAvailable: true, CanApply: true}}
	s := NewScheduler(db, ua)
	s.shot(AutoUpdateSettings{}, autoRunState{})
	if ua.applied != 0 {
		t.Fatalf("check fallido no debe aplicar")
	}
	if _, res := AutoRunState(db); res != "check-failed" {
		t.Fatalf("lastResult: %q", res)
	}
}

func TestSchedulerShotUpToDate(t *testing.T) {
	db := openDB(t)
	ua := &fakeUA{status: Status{UpdateAvailable: false, CanApply: true}}
	s := NewScheduler(db, ua)
	s.shot(AutoUpdateSettings{}, autoRunState{})
	if ua.applied != 0 {
		t.Fatalf("sin novedad no aplica")
	}
	if _, res := AutoRunState(db); res != "up-to-date" {
		t.Fatalf("lastResult: %q", res)
	}
}

func TestSchedulerTickMarksBeforeApply(t *testing.T) {
	// El last_run se persiste ANTES del apply: si el proceso muere en el
	// restart, el arranque posterior no re-dispara el mismo slot.
	db := openDB(t)
	latest := "v9.9.9"
	ua := &fakeUA{
		status:   Status{UpdateAvailable: true, CanApply: true, Latest: &latest},
		applyRet: true,
	}
	if err := SaveAutoUpdateSettings(db, AutoUpdateSettings{Enabled: true, Kind: AutoDaily, Time: "00:00"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.Local)
	s := NewScheduler(db, ua)
	s.now = func() time.Time { return now }
	// El apply "mata" el proceso: last_run ya debe estar persistido.
	ua.applyHook = func() {
		lr, _ := AutoRunState(db)
		if lr == 0 {
			t.Errorf("last_run debe persistir ANTES del apply")
		}
	}
	s.tick()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && ua.applied == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if ua.applied != 1 {
		t.Fatalf("tick con settings vencidos debe aplicar: %d", ua.applied)
	}
}
