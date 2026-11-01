package adapters

import (
	"testing"
	"time"
)

func TestMetricsHistoryPreservesTrafficPeaks(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	start := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)

	for i := 0; i < 16; i++ {
		rx, tx, cpu := 1e6, 1e6, 10.0
		if i == 7 {
			rx, tx, cpu = 447.5e6, 28.2e6, 100.0
		}
		if _, err := d.Exec(`INSERT INTO metrics
			(router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps)
			VALUES ('gw', ?, ?, 20, 40, 0, ?, ?)`,
			start.Add(time.Duration(i)*3*time.Minute).UnixMilli(), cpu, rx, tx); err != nil {
			t.Fatalf("insert metric: %v", err)
		}
	}

	got := l.metricsHistory("gw", "24h")
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1 hourly point", len(got))
	}
	if got[0].down != 447.5 || got[0].up != 28.2 {
		t.Fatalf("traffic=(%.1f, %.1f) Mbps, want peak (447.5, 28.2)", got[0].down, got[0].up)
	}
	if got[0].cpu != 16 {
		t.Fatalf("cpu=%d, want average 16", got[0].cpu)
	}
}

func TestMetricsHistoryBucketsPreservesTrafficPeaks(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	start := time.Now().Add(-24 * time.Hour).Truncate(6 * time.Hour)
	samples := []struct {
		offset       time.Duration
		rxAvg, rxMax float64
		txAvg, txMax float64
		cpu          float64
	}{
		{offset: 5 * time.Minute, rxAvg: 1e6, rxMax: 50e6, txAvg: 1e6, txMax: 10e6, cpu: 10},
		{offset: 10 * time.Minute, rxAvg: 2e6, rxMax: 447.5e6, txAvg: 2e6, txMax: 28.2e6, cpu: 20},
	}
	for _, sample := range samples {
		ts := start.Add(sample.offset).UnixMilli()
		if _, err := d.Exec(`INSERT INTO metrics_buckets
			(router_id, bucket_ts, n, min_ts, cpu_avg, ram_avg, temp_avg,
			 rx_min, rx_max, rx_avg, tx_min, tx_max, tx_avg)
			VALUES ('gw', ?, 1, ?, ?, 20, 40, 0, ?, ?, 0, ?, ?)`,
			ts, ts, sample.cpu, sample.rxMax, sample.rxAvg, sample.txMax, sample.txAvg); err != nil {
			t.Fatalf("insert metrics bucket: %v", err)
		}
	}

	got := l.metricsHistory("gw", "30d")
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1 six-hour point", len(got))
	}
	if got[0].down != 447.5 || got[0].up != 28.2 {
		t.Fatalf("traffic=(%.1f, %.1f) Mbps, want peak (447.5, 28.2)", got[0].down, got[0].up)
	}
	if got[0].cpu != 15 {
		t.Fatalf("cpu=%d, want average 15", got[0].cpu)
	}
}
