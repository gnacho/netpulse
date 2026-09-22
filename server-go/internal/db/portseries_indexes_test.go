// portseries_indexes_test.go — #817: la migración elimina los dos índices de
// port_series_raw idénticos a su PRIMARY KEY y no los vuelve a crear.
package db

import "testing"

func countIndex(t *testing.T, d *DB, name string) int {
	t.Helper()
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", name).Scan(&n); err != nil {
		t.Fatalf("contar índice %s: %v", name, err)
	}
	return n
}

func TestOpenDropsRedundantPortSeriesIndexes(t *testing.T) {
	dir := t.TempDir()

	d, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Simula una DB creada antes del fix: los dos índices redundantes existen.
	for _, idx := range []string{"idx_port_series_raw_rpt", "idx_port_series_raw_router_port_ts"} {
		if _, err := d.Exec("CREATE INDEX IF NOT EXISTS " + idx + " ON port_series_raw(router_id, port_id, ts)"); err != nil {
			t.Fatalf("crear %s: %v", idx, err)
		}
	}
	d.Close()

	// El siguiente arranque debe eliminarlos (migración idempotente).
	d2, err := Open(dir)
	if err != nil {
		t.Fatalf("reabrir: %v", err)
	}
	defer d2.Close()

	for _, idx := range []string{"idx_port_series_raw_rpt", "idx_port_series_raw_router_port_ts"} {
		if n := countIndex(t, d2, idx); n != 0 {
			t.Fatalf("%s debería haberse eliminado, pero sigue en sqlite_master", idx)
		}
	}
	// El índice por ts (retención) debe seguir existiendo.
	if n := countIndex(t, d2, "idx_port_series_raw_ts"); n != 1 {
		t.Fatalf("idx_port_series_raw_ts debería seguir existiendo")
	}
	// Y la PK implícita también (es la que cubre router_id/port_id/ts).
	if n := countIndex(t, d2, "sqlite_autoindex_port_series_raw_1"); n != 1 {
		t.Fatalf("el autoindex de la PK debería seguir existiendo")
	}
}
