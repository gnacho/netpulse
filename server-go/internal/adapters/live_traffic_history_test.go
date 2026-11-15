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

	// #2046: 24h se sirve en buckets de 15 min, así que 48 minutos de
	// muestras dan 4 puntos y el pico conserva su valor.
	got := l.metricsHistory("gw", "24h")
	if len(got) != 4 {
		t.Fatalf("len=%d, want 4 buckets of 15 minutes", len(got))
	}
	peak := got[0]
	for _, p := range got {
		if p.down > peak.down {
			peak = p
		}
	}
	if peak.down != 447.5 || peak.up != 28.2 {
		t.Fatalf("traffic=(%.1f, %.1f) Mbps, want peak (447.5, 28.2)", peak.down, peak.up)
	}
	// La media de CPU es por bucket: las 5 muestras del bucket del pico son
	// 10,10,100,10,10 → 28.
	if peak.cpu != 28 {
		t.Fatalf("cpu=%d, want bucket average 28", peak.cpu)
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

// #2046: la resolución de cada rango es una decisión de producto, no un
// detalle. Este test la fija: 24h en 15 min y 7d en 6 h. Antes eran una hora
// y un día, y la gráfica se comía los picos entre puntos.
func TestMetricsHistoryResolution(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	now := time.Now()

	insert := func(ts time.Time) {
		t.Helper()
		if _, err := d.Exec(`INSERT OR REPLACE INTO metrics
			(router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps)
			VALUES ('gw', ?, 10, 20, 40, 0, 1e6, 1e6)`, ts.UnixMilli()); err != nil {
			t.Fatalf("insert metric: %v", err)
		}
	}

	// 24 h en pasos de 15 min.
	for i := 0; i < 96; i++ {
		insert(now.Add(-time.Duration(i)*15*time.Minute - time.Minute))
	}
	got := l.metricsHistory("gw", "24h")
	if len(got) < 95 || len(got) > 97 {
		t.Fatalf("24h tiene %d puntos; want ~96 (buckets de 15 min)", len(got))
	}

	// 7 d en pasos de 6 h.
	for i := 0; i < 28; i++ {
		insert(now.Add(-time.Duration(i)*6*time.Hour - time.Minute))
	}
	week := l.metricsHistory("gw", "7d")
	if len(week) < 27 || len(week) > 29 {
		t.Fatalf("7d tiene %d puntos; want ~28 (buckets de 6 h)", len(week))
	}
}
