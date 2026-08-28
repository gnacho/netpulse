package baselines

import (
	"math"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestObserveAndCheck(t *testing.T) {
	s := NewStore(nil)
	base := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 50; i++ {
		s.Observe("gw", "cpu", 50+float64(i%5), base)
	}

	anomaly, sigma := s.Check("gw", "cpu", 52, base)
	if anomaly {
		t.Fatalf("normal value should not be anomaly: sigma=%f", sigma)
	}

	anomaly, sigma = s.Check("gw", "cpu", 200, base)
	if !anomaly {
		t.Fatalf("extreme value should be anomaly: sigma=%f", sigma)
	}
	if sigma <= 3 {
		t.Fatalf("sigma should be > 3 for extreme value: %f", sigma)
	}
}

func TestMinimumSamples(t *testing.T) {
	s := NewStore(nil)
	base := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		s.Observe("gw", "cpu", 50, base)
	}

	anomaly, _ := s.Check("gw", "cpu", 200, base)
	if anomaly {
		t.Fatal("should not trigger with < 10 samples")
	}
}

func TestUnknownRouter(t *testing.T) {
	s := NewStore(nil)
	base := time.Now()

	anomaly, sigma := s.Check("unknown", "cpu", 100, base)
	if anomaly || sigma != 0 {
		t.Fatalf("unknown router should return false/0: anomaly=%v sigma=%f", anomaly, sigma)
	}
}

func TestPersistence(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	s := NewStore(d)
	base := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		s.Observe("gw", "cpu", 50, base)
	}
	s.save()

	s2 := NewStore(d)
	snap := s2.Snapshot()
	if snap["gw"] == nil || snap["gw"]["cpu"] == nil {
		t.Fatal("persisted data not loaded")
	}
	bucket := snap["gw"]["cpu"]["34"]
	if bucket.N != 20 || math.Abs(bucket.Mean-50) > 1 {
		t.Fatalf("unexpected bucket: %+v", bucket)
	}
}

func TestSnapshot(t *testing.T) {
	s := NewStore(nil)
	base := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)

	s.Observe("gw", "cpu", 50, base)
	s.Observe("gw", "ram", 70, base)
	s.Observe("ap", "cpu", 30, base)

	snap := s.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 routers, got %d", len(snap))
	}
	if snap["gw"]["cpu"] == nil || snap["gw"]["ram"] == nil {
		t.Fatal("missing metrics for gw")
	}
	if snap["ap"]["cpu"] == nil {
		t.Fatal("missing cpu for ap")
	}
}
