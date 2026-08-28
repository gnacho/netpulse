package clientbw

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestInsertAndQuery(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	s, err := NewStore(d.DB)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	now := time.Now()
	for i := 0; i < 10; i++ {
		if err := s.Insert(Sample{
			MAC: "aa:bb:cc:dd:ee:ff", RouterID: "gw",
			TS: now.Add(time.Duration(i) * time.Minute),
			RxBytes: uint64(i * 1000), TxBytes: uint64(i * 500),
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	points, err := s.GetSeries("aa:bb:cc:dd:ee:ff", "gw",
		now.Add(-time.Hour), now.Add(time.Hour), "raw")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(points) != 10 {
		t.Fatalf("expected 10 points, got %d", len(points))
	}
}

func TestRollup(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	s, err := NewStore(d.DB)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	now := time.Now()
	for i := 0; i < 20; i++ {
		_ = s.Insert(Sample{
			MAC: "aa:bb:cc:dd:ee:ff", RouterID: "gw",
			TS: now.Add(time.Duration(i) * time.Minute),
			RxBytes: 1000, TxBytes: 500,
		})
	}

	if err := s.RollupRawTo5m(48 * time.Hour); err != nil {
		t.Fatalf("rollup 5m: %v", err)
	}

	points, err := s.GetSeries("aa:bb:cc:dd:ee:ff", "gw",
		now.Add(-time.Hour), now.Add(time.Hour), "5m")
	if err != nil {
		t.Fatalf("query 5m: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("expected 5m buckets after rollup")
	}
}

func TestTopClients(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	s, err := NewStore(d.DB)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	now := time.Now()
	macs := []string{"aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02", "aa:bb:cc:dd:ee:03"}
	for i, mac := range macs {
		for j := 0; j < 5; j++ {
			_ = s.Insert(Sample{
				MAC: mac, RouterID: "gw",
				TS: now.Add(time.Duration(j) * time.Minute),
				RxBytes: uint64((i + 1) * 1000), TxBytes: uint64((i + 1) * 500),
			})
		}
	}

	top, err := s.TopClients("gw", now.Add(-time.Hour), 2)
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("expected 2 top clients, got %d", len(top))
	}
	if top[0].MAC != "aa:bb:cc:dd:ee:03" {
		t.Fatalf("expected heaviest client first: %s", top[0].MAC)
	}
}

func TestNilDB(t *testing.T) {
	s, err := NewStore(nil)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := s.Insert(Sample{MAC: "x", RouterID: "y", TS: time.Now()}); err != nil {
		t.Fatalf("insert nil: %v", err)
	}
	points, err := s.GetSeries("x", "y", time.Now(), time.Now(), "raw")
	if err != nil || len(points) != 0 {
		t.Fatalf("query nil: %v, %d", err, len(points))
	}
}
