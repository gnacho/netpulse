package pathanalysis

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func setup(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := EnsureSchema(d); err != nil {
		t.Fatal(err)
	}
	return NewStore(d)
}

func TestInsertAndLatest(t *testing.T) {
	s := setup(t)
	err := s.Insert(PathResult{
		RouterID: "patio", Destination: "8.8.8.8", TS: db.NowMS(),
		HopCount: 5, TotalMs: 25.3,
		Hops: []Hop{
			{Index: 1, Host: "192.168.1.1", AvgMs: 1.2, LossPct: 0},
			{Index: 2, Host: "10.0.0.1", AvgMs: 5.1, LossPct: 0},
			{Index: 3, Host: "172.16.0.1", AvgMs: 8.4, LossPct: 2.5},
			{Index: 4, Host: "216.239.0.1", AvgMs: 15.2, LossPct: 0},
			{Index: 5, Host: "8.8.8.8", AvgMs: 25.3, LossPct: 0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Latest("patio", "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if r.HopCount != 5 || r.TotalMs != 25.3 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if len(r.Hops) != 5 {
		t.Fatalf("expected 5 hops, got %d", len(r.Hops))
	}
	if r.Hops[2].LossPct != 2.5 {
		t.Fatalf("hop 3 loss should be 2.5, got %.1f", r.Hops[2].LossPct)
	}
}

func TestLatestNotFound(t *testing.T) {
	s := setup(t)
	_, err := s.Latest("nonexistent", "8.8.8.8")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestHistory(t *testing.T) {
	s := setup(t)
	now := db.NowMS()
	for i := 0; i < 5; i++ {
		_ = s.Insert(PathResult{
			RouterID: "patio", Destination: "1.1.1.1",
			TS: now - int64(i)*3600000, HopCount: 3, TotalMs: 10 + float64(i),
			Hops: []Hop{{Index: 1, Host: "gw", AvgMs: 1}},
		})
	}
	results, err := s.History("patio", "1.1.1.1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
}

func TestSummaries(t *testing.T) {
	s := setup(t)
	_ = s.Insert(PathResult{
		RouterID: "patio", Destination: "8.8.8.8", TS: db.NowMS(),
		HopCount: 4, TotalMs: 20,
		Hops: []Hop{
			{Index: 1, Host: "gw", LossPct: 0},
			{Index: 2, Host: "isp", LossPct: 5.0},
			{Index: 3, Host: "google", LossPct: 0},
		},
	})
	_ = s.Insert(PathResult{
		RouterID: "patio", Destination: "1.1.1.1", TS: db.NowMS(),
		HopCount: 3, TotalMs: 15,
		Hops: []Hop{{Index: 1, Host: "gw", LossPct: 0}},
	})
	summaries, err := s.Summaries("patio")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
}

func TestPurge(t *testing.T) {
	s := setup(t)
	_ = s.Insert(PathResult{RouterID: "r1", Destination: "d1", TS: db.NowMS() - 86400000*30, HopCount: 1, Hops: []Hop{}})
	_ = s.Insert(PathResult{RouterID: "r1", Destination: "d1", TS: db.NowMS(), HopCount: 1, Hops: []Hop{}})
	n, err := s.Purge(7 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 purged, got %d", n)
	}
}
