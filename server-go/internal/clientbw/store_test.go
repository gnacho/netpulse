package clientbw

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	sqldb, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	sqldb.SetMaxOpenConns(1)
	s, err := NewStore(sqldb)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqldb.Close() })
	return s
}

func TestSchemaCreation(t *testing.T) {
	s := openTestStore(t)
	for _, tbl := range []string{"client_bw_raw", "client_bw_5m", "client_bw_daily"} {
		var name string
		err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&name)
		if err != nil {
			t.Errorf("table %s not created: %v", tbl, err)
		}
	}
}

func TestInsertAndQuery(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	for i := 0; i < 10; i++ {
		if err := s.Insert(Sample{
			MAC: "aa:bb:cc:dd:ee:ff", RouterID: "gw",
			TS: now.Add(time.Duration(i) * time.Minute),
			RxBytes: uint64(i * 1000), TxBytes: uint64(i * 500),
			RxBps: float64(i * 8000), TxBps: float64(i * 4000),
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
	// El bps viaja hasta la serie (patrón portseries, #551).
	if points[9].RxBps != 9*8000 || points[9].TxBps != 9*4000 {
		t.Fatalf("bps no persistido: %+v", points[9])
	}
}

func TestRollup(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	for i := 0; i < 20; i++ {
		_ = s.Insert(Sample{
			MAC: "aa:bb:cc:dd:ee:ff", RouterID: "gw",
			TS:      now.Add(time.Duration(i) * time.Minute),
			RxBytes: 1000, TxBytes: 500,
			RxBps: 8000, TxBps: 4000,
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
	// Los rollups YA calculan bps reales (antes quedaban a 0, #551).
	for _, p := range points {
		if p.RxBps != 8000 || p.TxBps != 4000 {
			t.Fatalf("bps en bucket: %+v", p)
		}
	}
}

func TestTopClients(t *testing.T) {
	s := openTestStore(t)
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

func TestGetMACSeriesAgregaRouters(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().Truncate(time.Minute).Add(time.Minute)
	// La misma MAC vista en dos routers (roaming) a la misma hora.
	for _, rid := range []string{"rt2", "rt4"} {
		for i := 0; i < 3; i++ {
			_ = s.Insert(Sample{
				MAC: "aa:bb:cc:dd:ee:ff", RouterID: rid,
				TS:      now.Add(time.Duration(i) * time.Minute),
				RxBytes: 1000, TxBytes: 500, RxBps: 8000, TxBps: 4000,
			})
		}
	}
	// GetSeries con router filtra; GetMACSeries agrega ambos.
	one, err := s.GetSeries("aa:bb:cc:dd:ee:ff", "rt2", now.Add(-time.Hour), now.Add(time.Hour), "raw")
	if err != nil || len(one) != 3 {
		t.Fatalf("serie por router: %d %v", len(one), err)
	}
	all, err := s.GetMACSeries("aa:bb:cc:dd:ee:ff", now.Add(-time.Hour), now.Add(time.Hour), "raw")
	if err != nil {
		t.Fatalf("serie mac: %v", err)
	}
	// Mismo ts en ambos routers → cada punto agrega 2 muestras.
	if len(all) != 3 {
		t.Fatalf("serie mac puntos: %d (want 3)", len(all))
	}
	if all[0].RxBytes != 2000 || all[0].TxBytes != 1000 {
		t.Fatalf("agregación mac: %+v", all[0])
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
