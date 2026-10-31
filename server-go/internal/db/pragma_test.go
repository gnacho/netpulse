package db

import "testing"

// Los pragmas del arranque gobiernan el volumen de escrituras en disco
// (issue #1700: 45 GB/día en un USB con DELETE+FULL+temp en fichero frente
// a ~13,7 con WAL y ~4,6 sin spills a temporales). Estos tests anclan el
// contrato: WAL+temp_store=MEMORY por defecto en TODOS los despliegues,
// DELETE+FULL solo con el escape hatch, y temp_store configurable.

func pragmaJournalMode(t *testing.T, d *DB) string {
	t.Helper()
	var mode string
	if err := d.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	return mode
}

func pragmaTempStore(t *testing.T, d *DB) int {
	t.Helper()
	var mode int
	if err := d.QueryRow("PRAGMA temp_store").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA temp_store: %v", err)
	}
	return mode
}

// TestOpenDefaultsWALAndMemoryTempStore: todo despliegue (on-box incluido,
// que ya no pasa WithRollbackJournal desde #1700) arranca en WAL +
// temp_store=MEMORY.
func TestOpenDefaultsWALAndMemoryTempStore(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if got := pragmaJournalMode(t, d); got != "wal" {
		t.Fatalf("journal_mode = %q, want wal", got)
	}
	if got := pragmaTempStore(t, d); got != 2 {
		t.Fatalf("temp_store = %d, want 2 (MEMORY)", got)
	}
}

// TestOpenRollbackJournalEscapeHatch: NETPULSE_DB_ROLLBACK_JOURNAL=1
// (WithRollbackJournal) restaura DELETE (el FULL se aplica junto a él) y
// mantiene temp_store en MEMORY.
func TestOpenRollbackJournalEscapeHatch(t *testing.T) {
	d, err := Open(t.TempDir(), WithRollbackJournal())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if got := pragmaJournalMode(t, d); got != "delete" {
		t.Fatalf("journal_mode = %q, want delete", got)
	}
	if got := pragmaTempStore(t, d); got != 2 {
		t.Fatalf("temp_store = %d, want 2 (MEMORY)", got)
	}
}

// TestOpenTempStoreFileOption: WithTempStore("file") baja el pragma a FILE
// (camino de diagnóstico con el comportamiento clásico de spills a disco).
func TestOpenTempStoreFileOption(t *testing.T) {
	d, err := Open(t.TempDir(), WithTempStore("file"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if got := pragmaJournalMode(t, d); got != "wal" {
		t.Fatalf("journal_mode = %q, want wal", got)
	}
	if got := pragmaTempStore(t, d); got != 1 {
		t.Fatalf("temp_store = %d, want 1 (FILE)", got)
	}
}

// TestOpenTempStoreDefaultLeavesSQLiteBehavior: WithTempStore("default") no
// aplica pragma (temp_store 0 = decisión del propio SQLite).
func TestOpenTempStoreDefaultLeavesSQLiteBehavior(t *testing.T) {
	d, err := Open(t.TempDir(), WithTempStore("default"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if got := pragmaTempStore(t, d); got != 0 {
		t.Fatalf("temp_store = %d, want 0 (default)", got)
	}
}

// TestOpenWithTempStoreCaseInsensitive: el valor llega normalizado desde
// config, pero el caller puede pasar mayúsculas.
func TestOpenWithTempStoreCaseInsensitive(t *testing.T) {
	d, err := Open(t.TempDir(), WithTempStore("MEMORY"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if got := pragmaTempStore(t, d); got != 2 {
		t.Fatalf("temp_store = %d, want 2 (MEMORY)", got)
	}
}
