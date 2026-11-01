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

// #641: deriveFps calcula frames/s entre muestras consecutivas y deja a 0 el
// intervalo que cruza un reset del contador (p.ej. reboot del switch).
func TestDeriveFps(t *testing.T) {
	base := time.Now().Truncate(time.Second)
	pts := []PortPoint{
		{TS: base, RxFrames: 0, TxFrames: 0},
		{TS: base.Add(30 * time.Second), RxFrames: 3000, TxFrames: 1500},  // 100 rx, 50 tx fps
		{TS: base.Add(60 * time.Second), RxFrames: 6000, TxFrames: 3000},  // 100 rx, 50 tx fps
		{TS: base.Add(90 * time.Second), RxFrames: 500, TxFrames: 100},    // reset: contador bajó
		{TS: base.Add(120 * time.Second), RxFrames: 3500, TxFrames: 2100}, // 100 rx, 66.6 tx fps
	}
	deriveFps(pts)
	// primer punto sin anterior → 0
	if pts[0].RxFps != 0 || pts[0].TxFps != 0 {
		t.Fatalf("primer punto debería tener fps 0: %+v", pts[0])
	}
	if pts[1].RxFps != 100 || pts[1].TxFps != 50 {
		t.Fatalf("punto 1: rx=%.1f tx=%.1f, esperaba 100/50", pts[1].RxFps, pts[1].TxFps)
	}
	if pts[2].RxFps != 100 || pts[2].TxFps != 50 {
		t.Fatalf("punto 2: rx=%.1f tx=%.1f, esperaba 100/50", pts[2].RxFps, pts[2].TxFps)
	}
	// reset: intervalo a 0
	if pts[3].RxFps != 0 || pts[3].TxFps != 0 {
		t.Fatalf("punto 3 (reset) debería tener fps 0: %+v", pts[3])
	}
	if pts[4].RxFps != 100 || pts[4].TxFps < 66 || pts[4].TxFps > 67 {
		t.Fatalf("punto 4: rx=%.1f tx=%.1f, esperaba 100/~66.6", pts[4].RxFps, pts[4].TxFps)
	}
}

// #1535: deriveBps calcula bits/s entre muestras consecutivas desde los bytes
// acumulados y deja a 0 el intervalo que cruza un reset del contador.
func TestDeriveBps(t *testing.T) {
	base := time.Now().Truncate(time.Second)
	pts := []PortPoint{
		{TS: base, RxBytes: 0, TxBytes: 0},
		{TS: base.Add(30 * time.Second), RxBytes: 3_750_000, TxBytes: 1_875_000},  // 1 Mbps rx, 0.5 Mbps tx
		{TS: base.Add(60 * time.Second), RxBytes: 7_500_000, TxBytes: 3_750_000},  // 1 Mbps rx, 0.5 Mbps tx
		{TS: base.Add(90 * time.Second), RxBytes: 100, TxBytes: 50},               // reset: contador bajó
		{TS: base.Add(120 * time.Second), RxBytes: 3_750_100, TxBytes: 1_875_050}, // 1 Mbps rx, 0.5 Mbps tx
	}
	deriveBps(pts)
	if pts[0].RxBps != 0 || pts[0].TxBps != 0 {
		t.Fatalf("primer punto debería tener bps 0: %+v", pts[0])
	}
	if pts[1].RxBps != 1_000_000 || pts[1].TxBps != 500_000 {
		t.Fatalf("punto 1: rx=%.0f tx=%.0f, esperaba 1000000/500000", pts[1].RxBps, pts[1].TxBps)
	}
	if pts[2].RxBps != 1_000_000 || pts[2].TxBps != 500_000 {
		t.Fatalf("punto 2: rx=%.0f tx=%.0f, esperaba 1000000/500000", pts[2].RxBps, pts[2].TxBps)
	}
	if pts[3].RxBps != 0 || pts[3].TxBps != 0 {
		t.Fatalf("punto 3 (reset) debería tener bps 0: %+v", pts[3])
	}
	if pts[4].RxBps != 1_000_000 || pts[4].TxBps != 500_000 {
		t.Fatalf("punto 4: rx=%.0f tx=%.0f, esperaba 1000000/500000", pts[4].RxBps, pts[4].TxBps)
	}
}

// HourlyFpsTotal: agrega el tráfico del router en fps por hora sumando los
// deltas de contadores acumulados (rx+tx) de todos sus puertos.
func TestHourlyFpsTotal(t *testing.T) {
	s := openTestDB(t)
	nowHour := time.Now().Truncate(time.Hour)
	// Un puerto; acumulados que crecen 1.8M rx + 0.9M tx por hora → 750 fps.
	acc := []struct {
		off    time.Duration
		rx, tx uint64
	}{
		{-2 * time.Hour, 1_800_000, 900_000},
		{-1 * time.Hour, 3_600_000, 1_800_000},
		{0, 5_400_000, 2_700_000},
	}
	for _, a := range acc {
		if err := s.RecordSample(PortSample{RouterID: "sw1", PortID: "p1",
			TS: nowHour.Add(a.off), RxFrames: a.rx, TxFrames: a.tx}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := s.HourlyFpsTotal("sw1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("esperaba 3 buckets, tengo %d", len(out))
	}
	// Primer bucket sin anterior → 0; los dos siguientes ~750 fps.
	if out[0] != 0 {
		t.Fatalf("hora 0: %.1f fps, esperaba 0", out[0])
	}
	for i := 1; i <= 2; i++ {
		if out[i] < 749 || out[i] > 751 {
			t.Fatalf("hora %d: %.1f fps, esperaba ~750", i, out[i])
		}
	}
}

// HourlyBpsTotal: respaldo de la línea de 24 h de las unidades sin serie
// propia de throughput (#1741). Por hora y puerto se conserva el pico de las
// muestras y se suman los puertos.
func TestHourlyBpsTotal(t *testing.T) {
	s := openTestDB(t)
	nowHour := time.Now().Truncate(time.Hour)
	samples := []PortSample{
		{RouterID: "crs305", PortID: "sfp-sfpplus1", TS: nowHour.Add(-time.Hour), RxBps: 100, TxBps: 50},
		{RouterID: "crs305", PortID: "ether1", TS: nowHour.Add(-time.Hour), RxBps: 200},
		{RouterID: "crs305", PortID: "sfp-sfpplus1", TS: nowHour, RxBps: 300, TxBps: 100},
		{RouterID: "crs305", PortID: "sfp-sfpplus1", TS: nowHour.Add(30 * time.Minute), RxBps: 500, TxBps: 100},
		{RouterID: "crs305", PortID: "ether1", TS: nowHour},
	}
	for _, smp := range samples {
		if err := s.RecordSample(smp); err != nil {
			t.Fatal(err)
		}
	}
	out, err := s.HourlyBpsTotal("crs305", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("esperaba 2 buckets, tengo %d", len(out))
	}
	// Hora anterior: 100+50 del SFP y 200 del ether1.
	if out[0] < 349 || out[0] > 351 {
		t.Fatalf("hora anterior: %.0f bps, esperaba 350", out[0])
	}
	// Hora en curso: pico del SFP (500 rx + 100 tx) y ether1 sin tasa.
	if out[1] < 599 || out[1] > 601 {
		t.Fatalf("hora en curso: %.0f bps, esperaba 600", out[1])
	}
}

// #812: una muestra de hace 60h (dentro de la retención raw, fuera de la
// antigua ventana de 48h) debe agregarse en el job nocturno.
func TestNightlyJobBackfillsGapBeyond48h(t *testing.T) {
	s := openTestDB(t)
	old := time.Now().Add(-60 * time.Hour)
	if err := s.RecordSample(PortSample{
		RouterID: "rt1", PortID: "lan1", TS: old,
		RxBytes: 1000, TxBytes: 2000, RxBps: 500, TxBps: 1000, SpeedMbps: 1000,
	}); err != nil {
		t.Fatal(err)
	}

	s.NightlyJob()

	bucket := (old.UnixMilli() / BucketMS) * BucketMS
	var n int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM port_series_5m WHERE router_id=? AND port_id=? AND bucket_ts=?",
		"rt1", "lan1", bucket).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("el tramo >48h no se agregó (bug #812): bucket %d, n=%d", bucket, n)
	}
}
