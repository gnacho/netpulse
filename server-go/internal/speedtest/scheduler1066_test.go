// scheduler1066_test.go — #1066: programación "auto" (diaria a la hora de
// menos tráfico del histórico propio), hora por defecto 03:00 y picker de
// hora valle sobre la tabla metrics (misma BD que el scheduler).
package speedtest

import (
	"testing"
	"time"
)

// seedMetrics inserta filas en la tabla metrics del scheduler (ts epoch ms).
func seedMetrics(t *testing.T, sched *Scheduler, rows [][3]int64) {
	t.Helper()
	for _, r := range rows {
		if _, err := sched.db.Exec(
			"INSERT INTO metrics (router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps) VALUES ('rt1', ?, 0, 0, 0, 0, ?, ?)",
			r[0], r[1], r[2]); err != nil {
			t.Fatalf("seed metrics: %v", err)
		}
	}
}

// atLocal construye el ts epoch ms de una fecha en la zona local del test.
func atLocal(y int, m time.Month, day, h, min int) int64 {
	return time.Date(y, m, day, h, min, 0, 0, time.Local).UnixMilli()
}

func TestDefaultSchedTime0300(t *testing.T) {
	if DefaultSchedTime != "03:00" {
		t.Fatalf("DefaultSchedTime = %q, want 03:00", DefaultSchedTime)
	}
	// LoadSettings con kv vacío debe traer la hora por defecto nueva (era 01:00).
	_, sched := openStore(t)
	if got := sched.LoadSettings().Time; got != "03:00" {
		t.Fatalf("LoadSettings().Time = %q, want 03:00", got)
	}
}

func TestQuietHourPicksArgmin(t *testing.T) {
	_, sched := openStore(t)
	// Junio (sin DST en Europa): 7 días con tráfico alto de 12:00 a 14:00
	// y un valle claro a las 04:00. El picker debe elegir el valle.
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.Local)
	sched.now = func() time.Time { return now }
	var rows [][3]int64
	for d := 0; d < 7; d++ {
		day := now.AddDate(0, 0, -d)
		y, m, dd := day.Date()
		for _, h := range []int{12, 13, 14} {
			rows = append(rows, [3]int64{atLocal(y, m, dd, h, 0), 80_000_000, 20_000_000})
		}
		rows = append(rows, [3]int64{atLocal(y, m, dd, 4, 0), 100_000, 50_000})
		rows = append(rows, [3]int64{atLocal(y, m, dd, 22, 0), 30_000_000, 5_000_000})
	}
	seedMetrics(t, sched, rows)
	if got := sched.quietHour(now); got != 4 {
		t.Fatalf("quietHour = %d, want 4 (el valle del histórico)", got)
	}
	// resolveAutoTime expone la hora elegida en formato HH:00 para Status.
	if got := sched.resolveAutoTime(); got != "04:00" {
		t.Fatalf("resolveAutoTime = %q, want 04:00", got)
	}
}

func TestQuietHourFallbackSinDatos(t *testing.T) {
	_, sched := openStore(t)
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.Local)
	if got := sched.quietHour(now); got != defaultQuietHour {
		t.Fatalf("quietHour sin datos = %d, want %d (fallback)", got, defaultQuietHour)
	}
	// Datos fuera de la ventana de 7 días tampoco sirven.
	_, sched2 := openStore(t)
	seedMetrics(t, sched2, [][3]int64{{atLocal(2026, 5, 1, 4, 0), 1000, 1000}})
	if got := sched2.quietHour(now); got != defaultQuietHour {
		t.Fatalf("quietHour con datos antiguos = %d, want %d (fallback)", got, defaultQuietHour)
	}
}

func TestSaveSettingsAutoRoundtrip(t *testing.T) {
	_, sched := openStore(t)
	if err := sched.SaveSettings(Settings{Enabled: true, ScheduleKind: "auto"}); err != nil {
		t.Fatalf("auto debe aceptarse sin day/time: %v", err)
	}
	if got := sched.LoadSettings().ScheduleKind; got != "auto" {
		t.Fatalf("roundtrip scheduleKind = %q, want auto", got)
	}
	err := sched.SaveSettings(Settings{Enabled: true, ScheduleKind: "diario"})
	if err == nil {
		t.Fatal("scheduleKind inválido debe rechazarse")
	}
}

func TestTestDueAutoDiario(t *testing.T) {
	st := Settings{Enabled: true, ScheduleKind: "auto", Time: "04:00"}
	at := func(h, m int) time.Time { return time.Date(2026, 6, 15, h, m, 0, 0, time.Local) }
	cases := []struct {
		name string
		now  time.Time
		last *Result
		want bool
	}{
		{"05:00 sin histórico", at(5, 0), nil, true},
		{"05:00 con run de hoy 04:30", at(5, 0), &Result{TS: at(4, 30)}, false},
		{"03:59 (antes de la hora)", at(3, 59), nil, false},
		{"04:00 en punto", at(4, 0), nil, true},
		{"día siguiente con run de ayer", at(5, 0).AddDate(0, 0, 1), &Result{TS: at(4, 30)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := testDue(st, tc.last, tc.now); got != tc.want {
				t.Fatalf("testDue = %v, want %v", got, tc.want)
			}
		})
	}
	// Time vacío cae al default (03:00).
	stEmpty := Settings{Enabled: true, ScheduleKind: "auto"}
	if !testDue(stEmpty, nil, at(3, 30)) {
		t.Fatal("auto sin Time debe usar el default 03:00")
	}
}

func TestStatusAutoExponeHoraElegida(t *testing.T) {
	_, sched := openStore(t)
	// 02:00 local: antes del slot elegido (04:00), así nextRun cae hoy a esa
	// hora en vez de al clamp genérico de Status.
	now := time.Date(2026, 6, 15, 2, 0, 0, 0, time.Local)
	sched.now = func() time.Time { return now }
	var rows [][3]int64
	for d := 0; d < 7; d++ {
		day := now.AddDate(0, 0, -d)
		y, m, dd := day.Date()
		rows = append(rows, [3]int64{atLocal(y, m, dd, 12, 0), 80_000_000, 20_000_000})
		rows = append(rows, [3]int64{atLocal(y, m, dd, 4, 0), 100_000, 50_000})
	}
	seedMetrics(t, sched, rows)
	if err := sched.SaveSettings(Settings{Enabled: true, ScheduleKind: "auto"}); err != nil {
		t.Fatalf("save auto: %v", err)
	}
	st := sched.Status()
	if st.AutoTime != "04:00" {
		t.Fatalf("Status.AutoTime = %q, want 04:00", st.AutoTime)
	}
	if st.NextRun == nil {
		t.Fatal("Status.NextRun no debe ser nil con auto activado")
	}
	next := time.UnixMilli(*st.NextRun).In(time.Local)
	if next.Hour() != 4 {
		t.Fatalf("nextRun debe caer a las 04:xx local, cayó a las %02d:%02d", next.Hour(), next.Minute())
	}
}
