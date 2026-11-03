package httpapi

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

func newScheduleTestServer(t *testing.T) *server {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return &server{db: d, agentHub: sse.NewAgentHub(nil)}
}

func TestConfigBackupScheduleDefaultsAndRoundTrip(t *testing.T) {
	s := newScheduleTestServer(t)
	got := s.loadConfigBackupSchedule()
	if got.Enabled || got.DayOfWeek != 0 || got.Time != "03:00" {
		t.Fatalf("defaults: %+v", got)
	}
	want := ConfigBackupSchedule{Enabled: true, DayOfWeek: 5, Time: "04:25", LastRunMs: 1234}
	if err := s.saveConfigBackupSchedule(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got = s.loadConfigBackupSchedule()
	if !got.Enabled || got.DayOfWeek != 5 || got.Time != "04:25" || got.LastRunMs != 1234 {
		t.Fatalf("round trip: %+v", got)
	}
}

func TestConfigBackupScheduleValidation(t *testing.T) {
	for _, tc := range []ConfigBackupSchedule{
		{DayOfWeek: 7, Time: "03:00"},
		{DayOfWeek: -1, Time: "03:00"},
		{DayOfWeek: 0, Time: "3:00"},
		{DayOfWeek: 0, Time: "25:00"},
	} {
		if err := validateConfigBackupSchedule(tc); err == nil {
			t.Fatalf("validó %+v", tc)
		}
	}
}

func TestConfigBackupScheduleDue(t *testing.T) {
	loc := time.FixedZone("test", 3600)
	monday := time.Date(2026, time.November, 2, 4, 0, 0, 0, loc) // lunes
	sched := ConfigBackupSchedule{Enabled: true, DayOfWeek: 1, Time: "03:00"}
	if !configBackupScheduleDue(sched, monday) {
		t.Fatal("el slot del lunes debería vencer")
	}
	sched.LastRunMs = monday.Add(-time.Hour).UnixMilli()
	if configBackupScheduleDue(sched, monday) {
		t.Fatal("un run posterior al slot no debe repetirse")
	}
	sched.LastRunMs = 0
	if configBackupScheduleDue(sched, monday.AddDate(0, 0, 1)) {
		t.Fatal("martes no debe hacer catch-up del lunes")
	}
	sched.Enabled = false
	if configBackupScheduleDue(sched, monday) {
		t.Fatal("desactivado no debe vencer")
	}
}

func TestNextConfigBackupSchedule(t *testing.T) {
	loc := time.FixedZone("test", 3600)
	monday := time.Date(2026, time.November, 2, 4, 0, 0, 0, loc)
	next := nextConfigBackupSchedule(1, "03:00", monday)
	if !next.Equal(time.Date(2026, time.November, 9, 3, 0, 0, 0, loc)) {
		t.Fatalf("next = %v", next)
	}
	sunday := time.Date(2026, time.November, 8, 4, 0, 0, 0, loc)
	next = nextConfigBackupSchedule(0, "05:00", sunday)
	if !next.Equal(time.Date(2026, time.November, 8, 5, 0, 0, 0, loc)) {
		t.Fatalf("next domingo = %v", next)
	}
}

func TestConfigBackupScheduleTickWithoutAgentsKeepsSlot(t *testing.T) {
	s := newScheduleTestServer(t)
	loc := time.FixedZone("test", 3600)
	monday := time.Date(2026, time.November, 2, 4, 0, 0, 0, loc)
	if err := s.saveConfigBackupSchedule(ConfigBackupSchedule{Enabled: true, DayOfWeek: 1, Time: "03:00"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	s.configBackupScheduleTickAt(monday)
	if got := s.loadConfigBackupSchedule().LastRunMs; got != 0 {
		t.Fatalf("sin agentes no debe marcar last_run: %d", got)
	}
}
