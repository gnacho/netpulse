package portseries

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return s
}

func TestSchemaCreation(t *testing.T) {
	s := openTestDB(t)
	if s == nil {
		t.Fatal("nil store")
	}
	for _, tbl := range []string{"port_series_raw", "port_series_5m", "port_series_daily"} {
		var name string
		err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&name)
		if err != nil {
			t.Errorf("table %s not created: %v", tbl, err)
		}
	}
}

func TestRecordAndReadRaw(t *testing.T) {
	s := openTestDB(t)
	now := time.Now().Truncate(time.Millisecond)
	sample := PortSample{
		RouterID: "rt1", PortID: "lan1", TS: now,
		RxBytes: 1000, TxBytes: 2000, RxErrors: 1, TxErrors: 0,
		RxFrames: 10, TxFrames: 20, RxBps: 500.0, TxBps: 1000.0, SpeedMbps: 1000,
	}
	if err := s.RecordSample(sample); err != nil {
		t.Fatal(err)
	}
	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)
	points, err := s.GetSeries("rt1", "lan1", from, to, "raw")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(points))
	}
	p := points[0]
	if p.RxBytes != 1000 || p.TxBytes != 2000 || p.RxErrors != 1 || p.RxBps != 500.0 || p.TxBps != 1000.0 || p.SpeedMbps != 1000 {
		t.Errorf("unexpected point: %+v", p)
	}
}

func TestRecordEmptyRouterPort(t *testing.T) {
	s := openTestDB(t)
	err := s.RecordSample(PortSample{RouterID: "", PortID: "lan1", TS: time.Now()})
	if err != nil {
		t.Errorf("empty router should be skipped silently, got: %v", err)
	}
	err = s.RecordSample(PortSample{RouterID: "rt1", PortID: "", TS: time.Now()})
	if err != nil {
		t.Errorf("empty port should be skipped silently, got: %v", err)
	}
}

func TestRollupRawTo5m(t *testing.T) {
	s := openTestDB(t)
	base := time.Now().Truncate(time.Minute).Add(-10 * time.Minute)
	for i := 0; i < 12; i++ {
		ts := base.Add(time.Duration(i) * 30 * time.Second)
		err := s.RecordSample(PortSample{
			RouterID: "rt1", PortID: "lan1", TS: ts,
			RxBytes: uint64(100 * i), TxBytes: uint64(200 * i),
			RxBps: float64(i * 100), TxBps: float64(i * 200), SpeedMbps: 1000,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RollupRawTo5m(time.Hour); err != nil {
		t.Fatal(err)
	}
	points, err := s.GetSeries("rt1", "lan1", base.Add(-time.Hour), base.Add(time.Hour), "5m")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 {
		t.Fatal("expected 5m buckets, got 0")
	}
}

func TestRollup5mToDaily(t *testing.T) {
	s := openTestDB(t)
	base := time.Now().Truncate(time.Minute).Add(-2 * time.Hour)
	for i := 0; i < 24; i++ {
		ts := base.Add(time.Duration(i) * 5 * time.Minute)
		err := s.RecordSample(PortSample{
			RouterID: "rt1", PortID: "lan1", TS: ts,
			RxBytes: uint64(1000), TxBytes: uint64(2000),
			RxBps: 100.0, TxBps: 200.0, SpeedMbps: 1000,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RollupRawTo5m(3 * time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := s.Rollup5mToDaily(3 * time.Hour); err != nil {
		t.Fatal(err)
	}
	points, err := s.GetSeries("rt1", "lan1", base.Add(-24*time.Hour), base.Add(24*time.Hour), "daily")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 {
		t.Fatal("expected daily points, got 0")
	}
}

func TestResolution(t *testing.T) {
	now := time.Now()
	if r := Resolution(now.Add(-2*time.Hour), now); r != "raw" {
		t.Errorf("2h should be raw, got %s", r)
	}
	if r := Resolution(now.Add(-7*24*time.Hour), now); r != "5m" {
		t.Errorf("7d should be 5m, got %s", r)
	}
	if r := Resolution(now.Add(-60*24*time.Hour), now); r != "daily" {
		t.Errorf("60d should be daily, got %s", r)
	}
}

func TestGetSeriesEmpty(t *testing.T) {
	s := openTestDB(t)
	now := time.Now()
	for _, res := range []string{"raw", "5m", "daily"} {
		points, err := s.GetSeries("nonexistent", "lan1", now.Add(-time.Hour), now, res)
		if err != nil {
			t.Errorf("resolution %s: unexpected error: %v", res, err)
		}
		if len(points) != 0 {
			t.Errorf("resolution %s: expected 0 points, got %d", res, len(points))
		}
	}
}

func TestPurgeRaw(t *testing.T) {
	s := openTestDB(t)
	old := time.Now().Add(-8 * 24 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)
	for _, ts := range []time.Time{old, recent} {
		if err := s.RecordSample(PortSample{
			RouterID: "rt1", PortID: "lan1", TS: ts,
			RxBytes: 100, TxBytes: 200, RxBps: 10, TxBps: 20,
		}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.PurgeRaw()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 purged, got %d", n)
	}
	points, err := s.GetSeries("rt1", "lan1", time.Now().Add(-2*time.Hour), time.Now(), "raw")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 {
		t.Errorf("expected 1 remaining raw point, got %d", len(points))
	}
}

func TestPurge5m(t *testing.T) {
	s := openTestDB(t)
	old := time.Now().Add(-400 * 24 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)
	for _, ts := range []time.Time{old, recent} {
		bucketMs := (ts.UnixMilli() / int64(BucketMS)) * int64(BucketMS)
		_, err := s.db.Exec(`INSERT OR REPLACE INTO port_series_5m
			(router_id, port_id, bucket_ts, n, rx_bytes, tx_bytes,
			 rx_errors, tx_errors, rx_frames, tx_frames,
			 rx_bps_min, rx_bps_max, rx_bps_avg,
			 tx_bps_min, tx_bps_max, tx_bps_avg, speed_mbps)
			VALUES (?, ?, ?, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1000)`,
			"rt1", "lan1", bucketMs)
		if err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.Purge5m()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 purged, got %d", n)
	}
}

func TestNightlyJobIntegration(t *testing.T) {
	s := openTestDB(t)
	base := time.Now().Truncate(time.Minute).Add(-10 * time.Minute)
	for i := 0; i < 12; i++ {
		ts := base.Add(time.Duration(i) * 30 * time.Second)
		if err := s.RecordSample(PortSample{
			RouterID: "rt1", PortID: "lan1", TS: ts,
			RxBytes: uint64(100 * i), TxBytes: uint64(200 * i),
			RxBps: float64(i * 100), TxBps: float64(i * 200), SpeedMbps: 1000,
		}); err != nil {
			t.Fatal(err)
		}
	}
	s.NightlyJob()
	points, err := s.GetSeries("rt1", "lan1", base.Add(-time.Hour), base.Add(time.Hour), "5m")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 {
		t.Error("expected 5m buckets after nightly job, got 0")
	}
}
