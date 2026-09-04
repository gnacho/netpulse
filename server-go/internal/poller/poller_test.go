package poller

import (
	"context"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// blockingAdapter embebe el adaptador demo (Snapshotter completo) y bloquea
// GetOverview hasta que release se cierre: simula un sondeo SNMP/SSH colgado
// (issue #481).
type blockingAdapter struct {
	*adapters.Demo
	release chan struct{}
}

func (b *blockingAdapter) GetOverview(context.Context) (*adapters.Overview, error) {
	<-b.release
	return nil, nil
}

func newBlockingPoller() (*Poller, *blockingAdapter) {
	a := &blockingAdapter{Demo: adapters.NewDemo(), release: make(chan struct{})}
	return New(a, nil, nil), a
}

func TestStopDoesNotHangOnSlowTick(t *testing.T) {
	old := stopWait
	stopWait = 150 * time.Millisecond
	defer func() { stopWait = old }()

	p, a := newBlockingPoller()
	p.Start()
	// espera a que el primer tick entre en GetOverview (bloqueado)
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	p.Stop()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Stop se bloqueó %v con un tick colgado; se esperaba <= ~150ms", elapsed)
	}
	close(a.release)
	time.Sleep(50 * time.Millisecond)
}

func TestStopWaitsForFinishedTick(t *testing.T) {
	old := stopWait
	stopWait = 150 * time.Millisecond
	defer func() { stopWait = old }()

	p, a := newBlockingPoller()
	p.Start()
	time.Sleep(50 * time.Millisecond)
	close(a.release) // el tick termina solo

	start := time.Now()
	p.Stop()
	if elapsed := time.Since(start); elapsed > stopWait {
		t.Fatalf("Stop tardó %v con un tick que ya terminó; se esperaba < %v", elapsed, stopWait)
	}
}
