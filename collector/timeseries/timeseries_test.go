// timeseries_test.go — primeros tests del paquete (contrato C4, Fase 5).
// SQLite real en t.TempDir(); el fallo de DB se inyecta cerrando la conexión
// (equivale a Begin/Commit fallando por FS RO o disco lleno).
package timeseries

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func newTestTS(t *testing.T) *TimeSeries {
	t.Helper()
	ts, err := NewTimeSeries(filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatalf("NewTimeSeries: %v", err)
	}
	return ts
}

func mustRegister(t *testing.T, ts *TimeSeries, key string) {
	t.Helper()
	if err := ts.RegisterMetric(Metric{Key: key, Unit: "ms", Kind: "gauge"}); err != nil {
		t.Fatalf("RegisterMetric(%s): %v", key, err)
	}
}

// Bug C4.1: con la DB fallando el buffer NO puede crecer sin límite:
// cap en bufferCap, drop-oldest y contador `dropped`.
func TestBufferCapDropOldest(t *testing.T) {
	ts := newTestTS(t)
	mustRegister(t, ts, "m")
	// Inyectar fallo de flush: Begin/Commit fallan con la conexión cerrada.
	if err := ts.db.Close(); err != nil {
		t.Fatalf("cerrar db: %v", err)
	}

	total := bufferCap + 250
	for i := 1; i <= total; i++ {
		if !ts.Write("m", float64(i)) {
			t.Fatalf("Write(%d) devolvió false", i)
		}
	}

	if got := ts.Dropped(); got != 250 {
		t.Errorf("dropped = %d, quiero 250", got)
	}
	ts.mu.Lock()
	pending := len(ts.buffer)
	oldest := ts.buffer[0].value
	newest := ts.buffer[len(ts.buffer)-1].value
	ts.mu.Unlock()
	if pending != bufferCap {
		t.Errorf("len(buffer) = %d, quiero cap %d", pending, bufferCap)
	}
	// drop-oldest: quedan las 10000 muestras más recientes (251..10250).
	if oldest != 251 || newest != float64(total) {
		t.Errorf("buffer = [%v..%v], quiero [251..%d] (drop-oldest)", oldest, newest, total)
	}
	// Close con la DB rota no debe colgar ni panic (flush/checkpoint fallan y se sigue).
	ts.Close()
}

// Bug C4.5: Close ordenado — el flush final persiste el lote pendiente y
// wg.Wait() ocurre antes de db.Close() (sin data race con flushLoop/NightlyJob).
func TestCloseFlushesAndStopsCleanly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.db")
	ts, err := NewTimeSeries(path)
	if err != nil {
		t.Fatalf("NewTimeSeries: %v", err)
	}
	mustRegister(t, ts, "m")

	// NightlyJob vivo con su ctx: debe parar antes de que Close cierre la DB.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); ts.RunNightlyJob(ctx, 3, 30) }()

	// Lote pendiente sin llegar a flushBatch: solo el flush de Close lo persiste.
	for i := 0; i < 10; i++ {
		ts.Write("m", float64(i))
	}
	cancel()
	ts.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunNightlyJob no paró tras cancel + Close")
	}

	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatalf("reabrir: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM samples").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 10 {
		t.Errorf("samples persistidas = %d, quiero 10 (flush final de Close)", n)
	}
}

// Bug C4.9: NewTimeSeries recarga métricas existentes (scan inicial + rows.Err).
func TestNewTimeSeriesReloadsMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.db")
	ts, err := NewTimeSeries(path)
	if err != nil {
		t.Fatalf("NewTimeSeries: %v", err)
	}
	mustRegister(t, ts, "m")
	ts.Close()

	ts2, err := NewTimeSeries(path)
	if err != nil {
		t.Fatalf("reabrir: %v", err)
	}
	defer ts2.Close()
	// La métrica recargada acepta writes sin RegisterMetric previo.
	if !ts2.Write("m", 1) {
		t.Error("Write tras reabrir: métrica no recargada desde la DB")
	}
}

// Bug C4.6 (camino feliz): NightlyJob completa rollup/purga sin error.
func TestNightlyJobRollup(t *testing.T) {
	ts := newTestTS(t)
	mustRegister(t, ts, "m")
	for i := 0; i < 60; i++ {
		ts.Write("m", float64(i))
	}
	ts.NightlyJob()
	var n int
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM buckets").Scan(&n); err != nil {
		t.Fatalf("count buckets: %v", err)
	}
	if n == 0 {
		t.Error("NightlyJob no generó buckets (rollup 5min vacío)")
	}
	ts.Close()
}
