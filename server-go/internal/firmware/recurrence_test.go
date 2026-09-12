// recurrence_test.go - tests de la programación recurrente de firmware
// (#761): vencimiento weekly/monthly/once (patrón de scheduler744_test) y
// persistencia kv roundtrip.
package firmware

import (
	"testing"
	"time"
)

func intPtr(v int) *int { return &v }

func TestRecurrenceDueWeekly(t *testing.T) {
	// Domingo 2026-09-13 10:00 local (fecha del desarrollo de #761).
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.Local)
	slot := time.Date(2026, 9, 13, 3, 30, 0, 0, time.Local)
	cases := []struct {
		name string
		rc   Recurrence
		now  time.Time
		want bool
	}{
		{"domingo despues del slot sin lastRun", Recurrence{Enabled: true, Kind: RecurrenceWeekly, DayOfWeek: intPtr(0), Time: "03:30"}, now, true},
		{"domingo antes del slot", Recurrence{Enabled: true, Kind: RecurrenceWeekly, DayOfWeek: intPtr(0), Time: "11:00"}, now, false},
		{"otro dia", Recurrence{Enabled: true, Kind: RecurrenceWeekly, DayOfWeek: intPtr(3), Time: "03:30"}, now, false},
		{"ya disparado hoy", Recurrence{Enabled: true, Kind: RecurrenceWeekly, DayOfWeek: intPtr(0), Time: "03:30", LastRunMs: slot.UnixMilli()}, now, false},
		{"disparado ayer", Recurrence{Enabled: true, Kind: RecurrenceWeekly, DayOfWeek: intPtr(0), Time: "03:30", LastRunMs: slot.Add(-24 * time.Hour).UnixMilli()}, now, true},
		{"disabled", Recurrence{Enabled: false, Kind: RecurrenceWeekly, DayOfWeek: intPtr(0), Time: "03:30"}, now, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RecurrenceDue(c.rc, c.now); got != c.want {
				t.Fatalf("RecurrenceDue = %v, esperaba %v", got, c.want)
			}
		})
	}
}

func TestRecurrenceDueMonthly(t *testing.T) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.Local)
	slot := time.Date(2026, 9, 12, 4, 0, 0, 0, time.Local)
	cases := []struct {
		name string
		rc   Recurrence
		now  time.Time
		want bool
	}{
		{"dia 12 a las 04", Recurrence{Enabled: true, Kind: RecurrenceMonthly, DayOfMonth: intPtr(12), Time: "04:00"}, now, true},
		{"dia 13", Recurrence{Enabled: true, Kind: RecurrenceMonthly, DayOfMonth: intPtr(13), Time: "04:00"}, now, false},
		{"ya disparado este mes", Recurrence{Enabled: true, Kind: RecurrenceMonthly, DayOfMonth: intPtr(12), Time: "04:00", LastRunMs: slot.UnixMilli()}, now, false},
		{"clamp 31 en febrero", Recurrence{Enabled: true, Kind: RecurrenceMonthly, DayOfMonth: intPtr(31), Time: "04:00"}, time.Date(2026, 2, 28, 5, 0, 0, 0, time.Local), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RecurrenceDue(c.rc, c.now); got != c.want {
				t.Fatalf("RecurrenceDue = %v, esperaba %v", got, c.want)
			}
		})
	}
}

func TestRecurrenceDueOnce(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour).UnixMilli()
	future := now.Add(time.Hour).UnixMilli()
	if !RecurrenceDue(Recurrence{Enabled: true, Kind: RecurrenceOnce, AtMs: past}, now) {
		t.Fatalf("once vencido sin disparar debe disparar")
	}
	if RecurrenceDue(Recurrence{Enabled: true, Kind: RecurrenceOnce, AtMs: past, LastRunMs: past + 1}, now) {
		t.Fatalf("once ya disparado no repite")
	}
	if RecurrenceDue(Recurrence{Enabled: true, Kind: RecurrenceOnce, AtMs: future}, now) {
		t.Fatalf("once futuro no dispara")
	}
}

func TestValidateRecurrence(t *testing.T) {
	if err := ValidateRecurrence(Recurrence{Kind: RecurrenceWeekly}); err == nil {
		t.Fatalf("weekly sin dayOfWeek/time debe fallar")
	}
	if err := ValidateRecurrence(Recurrence{Kind: RecurrenceMonthly, DayOfMonth: intPtr(12)}); err == nil {
		t.Fatalf("monthly sin time debe fallar")
	}
	if err := ValidateRecurrence(Recurrence{Kind: RecurrenceOnce}); err == nil {
		t.Fatalf("once sin atMs debe fallar")
	}
	if err := ValidateRecurrence(Recurrence{Kind: "hourly"}); err == nil {
		t.Fatalf("kind desconocido debe fallar")
	}
	if err := ValidateRecurrence(Recurrence{Kind: RecurrenceWeekly, DayOfWeek: intPtr(5), Time: "23:59"}); err != nil {
		t.Fatalf("weekly válida: %v", err)
	}
}

func TestRecurrenceSaveLoadRoundtrip(t *testing.T) {
	st := open(t)
	rc := Recurrence{
		Enabled: true, Kind: RecurrenceWeekly, DayOfWeek: intPtr(2), Time: "04:15",
	}
	if err := SaveRecurrence(st.db, "rt1", rc); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := LoadRecurrence(st.db, "rt1")
	if !got.Enabled || got.Kind != RecurrenceWeekly || got.DayOfWeek == nil || *got.DayOfWeek != 2 || got.Time != "04:15" {
		t.Fatalf("roundtrip: %+v", got)
	}
	SetRecurrenceLastRun(st.db, "rt1", 12345)
	if LoadRecurrence(st.db, "rt1").LastRunMs != 12345 {
		t.Fatalf("lastRun no persistió")
	}
	DisableRecurrence(st.db, "rt1")
	if LoadRecurrence(st.db, "rt1").Enabled {
		t.Fatalf("disable no aplicó")
	}
}

func TestNextRecurrenceAtWeekly(t *testing.T) {
	// Domingo 10:00, programado martes 04:15 -> próximo martes.
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.Local)
	rc := Recurrence{Enabled: true, Kind: RecurrenceWeekly, DayOfWeek: intPtr(2), Time: "04:15"}
	next := NextRecurrenceAt(rc, now)
	if next.Weekday() != time.Tuesday || next.Hour() != 4 || next.Minute() != 15 {
		t.Fatalf("next = %v (%s), esperaba martes 04:15", next, next.Weekday())
	}
}
