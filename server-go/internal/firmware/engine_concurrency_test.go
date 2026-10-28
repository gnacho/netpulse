package firmware

import (
	"sync"
	"sync/atomic"
	"testing"
)

type auditConcurrentSender struct{ calls atomic.Int64 }

func (s *auditConcurrentSender) SendPrivileged(string, string, any) error {
	s.calls.Add(1)
	return nil
}

func TestAuditConcurrentStartUpgradeIsSingleFlight(t *testing.T) {
	store := open(t)
	setTarget(t, store, "rt1")
	sender := &auditConcurrentSender{}
	engine := NewEngine(store, sender)

	for attempt := 0; attempt < 100; attempt++ {
		if _, err := store.db.Exec("DELETE FROM firmware_upgrades"); err != nil {
			t.Fatalf("reset upgrades: %v", err)
		}
		sender.calls.Store(0)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, _ = engine.StartUpgrade("rt1")
			}()
		}
		close(start)
		wg.Wait()

		var rows int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM firmware_upgrades WHERE router_id = 'rt1'").Scan(&rows); err != nil {
			t.Fatalf("count upgrades: %v", err)
		}
		if rows != 1 || sender.calls.Load() != 1 {
			t.Fatalf("attempt %d: rows=%d commands=%d, want exactly one", attempt, rows, sender.calls.Load())
		}
	}
}
