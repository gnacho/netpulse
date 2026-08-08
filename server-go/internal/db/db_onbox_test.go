// db_onbox_test.go — Fase 9 R6: WithRollbackJournal aplica DELETE+FULL y el
// Open por defecto conserva WAL+NORMAL (regresión del comportamiento actual).
package db

import "testing"

// pragmaStr lee un PRAGMA que devuelve texto (journal_mode).
func pragmaStr(t *testing.T, d *DB, pragma string) string {
	t.Helper()
	var v string
	if err := d.QueryRow("PRAGMA " + pragma).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", pragma, err)
	}
	return v
}

// pragmaInt lee un PRAGMA numérico (synchronous: 1=NORMAL, 2=FULL).
func pragmaInt(t *testing.T, d *DB, pragma string) int {
	t.Helper()
	var v int
	if err := d.QueryRow("PRAGMA " + pragma).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", pragma, err)
	}
	return v
}

func TestOpenRollbackJournal(t *testing.T) {
	d, err := Open(t.TempDir(), WithRollbackJournal())
	if err != nil {
		t.Fatalf("Open(WithRollbackJournal): %v", err)
	}
	defer d.Close()

	if got := pragmaStr(t, d, "journal_mode"); got != "delete" {
		t.Errorf("journal_mode = %q, esperaba delete", got)
	}
	if got := pragmaInt(t, d, "synchronous"); got != 2 {
		t.Errorf("synchronous = %d, esperaba 2 (FULL)", got)
	}
	if !d.rollbackJournal {
		t.Error("rollbackJournal debe quedar activo en el struct (guardas de checkpoint)")
	}
	// Maintenance no debe fallar sin WAL (checkpoint omitido).
	d.Maintenance()
}

func TestOpenPorDefectoSigueEnWAL(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	if got := pragmaStr(t, d, "journal_mode"); got != "wal" {
		t.Errorf("journal_mode = %q, esperaba wal (regresión)", got)
	}
	if got := pragmaInt(t, d, "synchronous"); got != 1 {
		t.Errorf("synchronous = %d, esperaba 1 (NORMAL, regresión)", got)
	}
}

func TestOpenConversionWALaDelete(t *testing.T) {
	// Una DB creada en WAL (CT) y reabierta on-box debe convertirse sola.
	dir := t.TempDir()
	d1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open WAL: %v", err)
	}
	if _, err := d1.Exec("INSERT INTO kv (key, value) VALUES ('t', '1')"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := d1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d2, err := Open(dir, WithRollbackJournal())
	if err != nil {
		t.Fatalf("reOpen DELETE: %v", err)
	}
	defer d2.Close()
	if got := pragmaStr(t, d2, "journal_mode"); got != "delete" {
		t.Errorf("journal_mode tras conversión = %q, esperaba delete", got)
	}
	var v string
	if err := d2.QueryRow("SELECT value FROM kv WHERE key = 't'").Scan(&v); err != nil || v != "1" {
		t.Errorf("dato pre-conversión perdido: v=%q err=%v", v, err)
	}
}
