package internethealth

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestRecordProbeAndSummary(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	s, err := NewStore(d)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	s.RecordProbe(ProbeResult{Ts: 1000, Target: "8.8.8.8", LatencyMs: 10, LossPct: 0, Success: true})
	s.RecordProbe(ProbeResult{Ts: 2000, Target: "1.1.1.1", LatencyMs: 15, LossPct: 0, Success: true})

	sum := s.Summary()
	if sum.LastProbe == nil || sum.LastProbe.Target != "1.1.1.1" {
		t.Fatalf("last probe: %+v", sum.LastProbe)
	}
	if sum.CurrentOutage != nil {
		t.Fatal("should have no outage")
	}
}

func TestOutageDetection(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	s, err := NewStore(d)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	s.RecordProbe(ProbeResult{Ts: 1000, Target: "8.8.8.8", LatencyMs: 0, LossPct: 100, Success: false})
	sum := s.Summary()
	if sum.CurrentOutage == nil {
		t.Fatal("expected active outage after failed probe")
	}

	s.RecordProbe(ProbeResult{Ts: 5000, Target: "8.8.8.8", LatencyMs: 10, LossPct: 0, Success: true})
	sum = s.Summary()
	if sum.CurrentOutage != nil {
		t.Fatal("outage should be resolved")
	}

	outages := s.RecentOutages(10)
	if len(outages) != 1 {
		t.Fatalf("expected 1 outage, got %d", len(outages))
	}
	if outages[0].DurationS == nil || *outages[0].DurationS != 4000 {
		t.Fatalf("duration: %v", outages[0].DurationS)
	}
}

func TestRecentProbes(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	s, err := NewStore(d)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	for i := 0; i < 5; i++ {
		s.RecordProbe(ProbeResult{Ts: int64(i * 1000), Target: "8.8.8.8", LatencyMs: 10, LossPct: 0, Success: true})
	}

	probes := s.RecentProbes(3)
	if len(probes) != 3 {
		t.Fatalf("expected 3 probes, got %d", len(probes))
	}
	if probes[0].Ts != 4000 {
		t.Fatalf("most recent first: %d", probes[0].Ts)
	}
}

func TestNilDB(t *testing.T) {
	s, err := NewStore(nil)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	s.RecordProbe(ProbeResult{Ts: 1000, Target: "8.8.8.8", LatencyMs: 10, LossPct: 0, Success: true})
	sum := s.Summary()
	if sum.LastProbe != nil {
		t.Fatal("nil DB should return empty summary")
	}
}
