// rollup_test.go — escalera de retención (Fase 8.3): samples → buckets 5 min
// → daily, con el mismo orden del job nocturno. Verifica valores agregados y
// que el rollup es idempotente (INSERT OR REPLACE).
package db_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "netpulse.db")
	if _, err := os.Stat(path); err == nil {
		os.Remove(path)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestRollupSamplesToBucketsYDaily(t *testing.T) {
	d := openTestDB(t)

	// Muestras de un router en 3 ventanas de 5 min (bucket alineado al epoch).
	// ts en ms. Base: now redondeado a bucket.
	now := time.Now().Truncate(5 * time.Minute)
	rxBase, txBase, cpuBase := 5e6, 1e6, 20.0
	for i := 0; i < 3; i++ {
		ts := now.Add(time.Duration(i) * time.Minute).UnixMilli()
		_, err := d.Exec(
			"INSERT INTO metrics (router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps) VALUES (?,?,?,?,?,?,?,?)",
			"r1", ts, cpuBase+float64(i), 50.0, 40.0, 2.0, rxBase+float64(i)*1e6, txBase+float64(i)*1e5)
		if err != nil {
			t.Fatalf("insert sample: %v", err)
		}
	}

	d.NightlyJob()

	// Un bucket (las 3 muestras caen en la misma ventana de 5 min).
	var n int
	var rxAvg, txAvg, cpuAvg float64
	err := d.QueryRow(
		"SELECT n, AVG(rx_avg), AVG(tx_avg), AVG(cpu_avg) FROM metrics_buckets WHERE router_id='r1'").Scan(&n, &rxAvg, &txAvg, &cpuAvg)
	if err != nil {
		t.Fatalf("query buckets: %v", err)
	}
	if n != 3 {
		t.Fatalf("n en bucket = %d, esperaba 3", n)
	}
	// AVG de rx: (5e6+6e6+7e6)/3 = 6e6; tx: (1e6+1.1e6+1.2e6)/3 = 1.1e6
	if rxAvg < 5.9e6 || rxAvg > 6.1e6 {
		t.Fatalf("rx_avg = %v, esperaba ~6e6", rxAvg)
	}
	if txAvg < 1.09e6 || txAvg > 1.11e6 {
		t.Fatalf("tx_avg = %v, esperaba ~1.1e6", txAvg)
	}
	if cpuAvg < 20.9 || cpuAvg > 21.1 {
		t.Fatalf("cpu_avg = %v, esperaba ~21", cpuAvg)
	}

	// Rollup buckets→daily: un día con n=3.
	var dn int
	if err := d.QueryRow("SELECT SUM(n) FROM metrics_daily WHERE router_id='r1'").Scan(&dn); err != nil {
		t.Fatalf("query daily: %v", err)
	}
	if dn != 3 {
		t.Fatalf("daily n = %d, esperaba 3", dn)
	}

	// Idempotencia: repetir el rollup no cambia los agregados (REPLACE).
	d.NightlyJob()
	var n2 int
	if err := d.QueryRow("SELECT n FROM metrics_buckets WHERE router_id='r1'").Scan(&n2); err != nil {
		t.Fatalf("query buckets 2: %v", err)
	}
	if n2 != 3 {
		t.Fatalf("n tras re-rollup = %d, esperaba 3 (idempotente)", n2)
	}
}

func TestShouldRunNightly(t *testing.T) {
	// Solo a las 03:xx locales.
	if !db.ShouldRunNightly(time.Date(2026, 8, 6, 3, 15, 0, 0, time.Local)) {
		t.Fatal("03:15 debería disparar el rollup")
	}
	if db.ShouldRunNightly(time.Date(2026, 8, 6, 10, 0, 0, 0, time.Local)) {
		t.Fatal("10:00 no debería disparar el rollup")
	}
}
