// scheduler744_test.go — #744: trampa de server_url (la web de Ookla mide
// basura), sanity check (no persistir resultados con down/up <= 0) y
// programación weekly/monthly con vencimiento derivado del último run.
package speedtest

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTestDueWeekly(t *testing.T) {
	dow := 1 // lunes
	st := Settings{Enabled: true, ScheduleKind: "weekly", DayOfWeek: &dow, Time: "01:00"}
	at := func(day time.Weekday, h, m int) time.Time {
		// lunes 2026-09-07; day 0=domingo..6=sábado
		base := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) // domingo
		return base.AddDate(0, 0, int(day)).Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}
	cases := []struct {
		name string
		now  time.Time
		last *Result
		want bool
	}{
		{"lunes 02:00 sin histórico", at(time.Monday, 2, 0), nil, true},
		{"lunes 02:00 con run de hoy 01:30", at(time.Monday, 2, 0), &Result{TS: at(time.Monday, 1, 30)}, false},
		{"lunes 00:30 (antes de la hora)", at(time.Monday, 0, 30), nil, false},
		{"domingo (no es el día)", at(time.Sunday, 2, 0), nil, false},
		{"martes ya corrió el lunes", at(time.Tuesday, 2, 0), &Result{TS: at(time.Monday, 1, 5)}, false},
		{"lunes siguiente sin run", at(time.Monday, 2, 0).AddDate(0, 0, 7), &Result{TS: at(time.Monday, 1, 5)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := testDue(st, tc.last, tc.now); got != tc.want {
				t.Fatalf("testDue = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTestDueMonthly(t *testing.T) {
	dom := 15
	st := Settings{Enabled: true, ScheduleKind: "monthly", DayOfMonth: &dom, Time: "01:00"}
	at := func(day, h, m int) time.Time { return time.Date(2026, 9, day, h, m, 0, 0, time.UTC) }
	cases := []struct {
		name string
		now  time.Time
		last *Result
		want bool
	}{
		{"día 15 tras la hora sin run", at(15, 2, 0), nil, true},
		{"día 15 tras la hora ya corrido", at(15, 2, 0), &Result{TS: at(15, 1, 30)}, false},
		{"día 14 (aún no)", at(14, 2, 0), nil, false},
		{"día 20 con run del 15", at(20, 2, 0), &Result{TS: at(15, 1, 0)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := testDue(st, tc.last, tc.now); got != tc.want {
				t.Fatalf("testDue = %v, want %v", got, tc.want)
			}
		})
	}
	// Clamp a fin de mes: day 31 en febrero (28 días) → slot el 28.
	d31 := 31
	stFeb := Settings{Enabled: true, ScheduleKind: "monthly", DayOfMonth: &d31, Time: "23:00"}
	now := time.Date(2026, 2, 28, 23, 30, 0, 0, time.UTC) // 28-02 23:30
	if !testDue(stFeb, nil, now) {
		t.Fatal("day 31 en febrero debe disparar el 28 a las 23:00 (clamp)")
	}
}

func TestSaveSettingsValidaTrappaYSchedule(t *testing.T) {
	_, sched := openStore(t)
	dow := 3
	if err := sched.SaveSettings(Settings{Enabled: true, IntervalHours: 6, ServerURL: "https://speedtest.net"}); err == nil || !strings.Contains(err.Error(), "Ookla") {
		t.Fatalf("la trampa speedtest.net debe rechazarse con mensaje claro: %v", err)
	}
	if err := sched.SaveSettings(Settings{Enabled: true, IntervalHours: 6, ServerURL: "https://www.speedtest.net"}); err == nil {
		t.Fatal("www.speedtest.net también es trampa")
	}
	if err := sched.SaveSettings(Settings{Enabled: true, IntervalHours: 6, ScheduleKind: "weekly"}); err == nil {
		t.Fatal("weekly sin dayOfWeek/time debe rechazarse")
	}
	if err := sched.SaveSettings(Settings{Enabled: true, ScheduleKind: "weekly", DayOfWeek: &dow, Time: "01:30"}); err != nil {
		t.Fatalf("weekly válido: %v", err)
	}
	got := sched.LoadSettings()
	if got.ScheduleKind != "weekly" || got.DayOfWeek == nil || *got.DayOfWeek != 3 || got.Time != "01:30" {
		t.Fatalf("roundtrip weekly: %+v", got)
	}
	d40 := 40
	if err := sched.SaveSettings(Settings{Enabled: true, IntervalHours: 6, ScheduleKind: "monthly", DayOfMonth: &d40}); err == nil {
		t.Fatal("dayOfMonth 40 debe rechazarse")
	}
	if err := sched.SaveSettings(Settings{Enabled: true, IntervalHours: 3}); err == nil {
		t.Fatal("intervalo 3h sigue fuera de la allowlist")
	}
	// URL legítima de un servidor real pasa.
	if err := sched.SaveSettings(Settings{Enabled: true, IntervalHours: 6, ServerURL: "http://testvelocidadsev.orange.es:8080/speedtest/upload.php"}); err != nil {
		t.Fatalf("URL legítima rechazada: %v", err)
	}
}

func TestSanitizeServerURL(t *testing.T) {
	_, sched := openStore(t)
	kvSet(sched.db, kvServerURL, "https://speedtest.net")
	sched.sanitizeServerURL()
	if got := kvGet(sched.db, kvServerURL); got != "" {
		t.Fatalf("la trampa debía limpiarse, queda %q", got)
	}
	// Una URL legítima se conserva.
	kvSet(sched.db, kvServerURL, "http://mi-servidor.ejemplo:8080/upload.php")
	sched.sanitizeServerURL()
	if got := kvGet(sched.db, kvServerURL); got == "" {
		t.Fatal("la URL legítima no debía tocarse")
	}
}

// garbageRunner devuelve la firma exacta del bug: down parcial, up residual.
type garbageRunner struct{}

func (garbageRunner) Run(ctx context.Context, serverURL string) (Result, error) {
	return Result{DownMbps: 382.2, UpMbps: -8e-06}, nil
}

func TestExecuteDiscardsGarbageResult(t *testing.T) {
	st, sched := openStore(t)
	sched.runner = garbageRunner{}
	sched.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	stSettings := Settings{Enabled: true, IntervalHours: 6}
	if err := sched.SaveSettings(stSettings); err != nil {
		t.Fatalf("save: %v", err)
	}
	err := sched.executeLocked(stSettings, "manual")
	if err == nil {
		t.Fatal("el resultado basura debe devolver error")
	}
	if !strings.Contains(err.Error(), "descartado") {
		t.Fatalf("mensaje inesperado: %v", err)
	}
	if last, _ := st.Latest(); last != nil {
		t.Fatalf("nada debía persistirse, hay %+v", last)
	}
}
