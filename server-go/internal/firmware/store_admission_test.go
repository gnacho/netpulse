package firmware

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestScheduleUpgradeConcurrentSingleRow(t *testing.T) {
	s := open(t)
	const attempts = 50
	const callers = 16

	for attempt := 0; attempt < attempts; attempt++ {
		if _, err := s.db.Exec(`DELETE FROM firmware_upgrades`); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		errs := make(chan error, callers)
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, err := s.ScheduleUpgrade("rt1", fmt.Sprintf("v%d", i), "http://x/image.bin", "abc", time.Now().Add(time.Hour).Unix())
				errs <- err
			}(i)
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("attempt %d: ScheduleUpgrade: %v", attempt, err)
			}
		}
		var rows int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM firmware_upgrades WHERE router_id='rt1' AND status='scheduled'`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 1 {
			t.Fatalf("attempt %d: scheduled rows=%d, want 1", attempt, rows)
		}
	}
}

func TestStartScheduledWaitsForActiveUpgrade(t *testing.T) {
	s := open(t)
	manualID, err := s.BeginUpgradeIfIdle("rt1", "manual", "http://x/manual.bin", "abc")
	if err != nil {
		t.Fatal(err)
	}
	scheduledID, err := s.ScheduleUpgrade("rt1", "scheduled", "http://x/scheduled.bin", "def", time.Now().Add(time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}

	started, err := s.StartScheduled(scheduledID)
	if err != nil {
		t.Fatal(err)
	}
	if started {
		t.Fatal("scheduled upgrade started while a manual upgrade was active")
	}

	if err := s.SetStatus(manualID, "done", "", ""); err != nil {
		t.Fatal(err)
	}
	started, err = s.StartScheduled(scheduledID)
	if err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("scheduled upgrade did not start after the manual upgrade finished")
	}
}
