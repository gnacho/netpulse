package adapters

import (
	"testing"
	"time"
)

// #2047: el resumen sale de las muestras crudas del rango, no de los puntos
// del gráfico (que son MAX por bucket y promediarlos daría la media de picos).
func TestWanRangeStatsOneHour(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d, now: fixedNow}
	now := fixedNow()

	// Dentro de la última hora: 20:10, 20:40 y 21:00.
	insertMetric(t, d, "gw", now.Add(-50*time.Minute), 30e6)
	insertMetric(t, d, "gw", now.Add(-20*time.Minute), 60e6)
	insertMetric(t, d, "gw", now, 90e6)
	// Fuera del rango: no debe entrar.
	insertMetric(t, d, "gw", now.Add(-2*time.Hour), 500e6)

	stats := l.wanRangeStats("gw")
	st, ok := stats["1h"]
	if !ok {
		t.Fatalf("sin resumen de 1h: %+v", stats)
	}
	if st.Samples != 3 {
		t.Fatalf("samples=%d, esperaba 3", st.Samples)
	}
	if st.PeakMbps != 90 {
		t.Fatalf("peakMbps=%v, esperaba 90", st.PeakMbps)
	}
	if st.AvgMbps != 60 {
		t.Fatalf("avgMbps=%v, esperaba 60", st.AvgMbps)
	}
	// dt real entre muestras = (21:00-20:10)/2 = 1500 s.
	// (30+60+90)e6 × 1500 / 8 = 3.375e10 bytes.
	if st.TotalBytes != 3.375e10 {
		t.Fatalf("totalBytes=%v, esperaba 3.375e10", st.TotalBytes)
	}
	if st.LossPct != nil {
		t.Fatal("lossPct no debe rellenarse sin serie histórica de pérdida")
	}
}

// Un rango sin muestras no publica ceros: no aparece y la UI pinta un guion.
func TestWanRangeStatsSkipsEmptyRanges(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d, now: fixedNow}
	if stats := l.wanRangeStats("gw"); len(stats) != 0 {
		t.Fatalf("sin datos debería ir vacío: %+v", stats)
	}
	// Un router sin datos tampoco contamina al gateway.
	insertMetric(t, d, "gw", fixedNow().Add(-10*time.Minute), 10e6)
	stats := l.wanRangeStats("rt2")
	if len(stats) != 0 {
		t.Fatalf("el resumen de rt2 no debe tener datos: %+v", stats)
	}
}

// Con una sola muestra no hay intervalo entre muestras, así que el volumen no
// se puede estimar y queda a cero (mejor eso que inventar bytes).
func TestWanRangeStatSingleSampleHasNoVolume(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d, now: fixedNow}
	insertMetric(t, d, "gw", fixedNow().Add(-5*time.Minute), 40e6)
	st, ok := l.wanRangeStat("gw", fixedNow().Add(-time.Hour), time.Hour)
	if !ok {
		t.Fatal("una muestra basta para el pico y la media")
	}
	if st.Samples != 1 {
		t.Fatalf("samples=%d, esperaba 1", st.Samples)
	}
	if st.TotalBytes != 0 {
		t.Fatalf("totalBytes=%v, esperaba 0 con una sola muestra", st.TotalBytes)
	}
}

// Largo plazo (30d) sale de metrics_buckets, donde cada bucket de 5 min ya
// guarda la media del tramo.
func TestWanRangeStatUsesBucketsForLongRanges(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d, now: fixedNow}
	now := fixedNow()

	ins := func(ts time.Time, n int64, rxMax, rxAvg float64) {
		t.Helper()
		if _, err := d.Exec(`INSERT INTO metrics_buckets
			(router_id, bucket_ts, n, min_ts, rx_max, rx_avg)
			VALUES (?,?,?,?,?,?)`, "gw", ts.UnixMilli(), n, ts.UnixMilli(), rxMax, rxAvg); err != nil {
			t.Fatalf("insert bucket: %v", err)
		}
	}
	ins(now.AddDate(0, 0, -20), 10, 40e6, 30e6)
	ins(now.AddDate(0, 0, -10), 10, 20e6, 10e6)

	st, ok := l.wanRangeStat("gw", now.AddDate(0, 0, -30), 30*24*time.Hour)
	if !ok {
		t.Fatal("sin resumen de 30d")
	}
	if st.PeakMbps != 40 {
		t.Fatalf("peakMbps=%v, esperaba 40", st.PeakMbps)
	}
	// Media ponderada por muestras: (30×10 + 10×10)/20 = 20 Mbps.
	if st.AvgMbps != 20 {
		t.Fatalf("avgMbps=%v, esperaba 20", st.AvgMbps)
	}
	// SUM(rx_avg) × 300 s / 8 = 40e6 × 300 / 8 = 1.5e9 bytes.
	if st.TotalBytes != 1.5e9 {
		t.Fatalf("totalBytes=%v, esperaba 1.5e9", st.TotalBytes)
	}
	if st.Samples != 20 {
		t.Fatalf("samples=%d, esperaba 20", st.Samples)
	}
}
