package httpapi

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestKV(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatalf("create kv: %v", err)
	}
	return db
}

// TestKVGetBoolDefault: ausente → false (opt-in off por defecto, #121).
func TestKVGetBoolDefault(t *testing.T) {
	db := newTestKV(t)
	if kvGetBool(db, orchestrationKey) {
		t.Error("esperaba false para clave ausente")
	}
}

// TestKVSetBoolRoundtrip: set true → get true; set false → get false.
func TestKVSetBoolRoundtrip(t *testing.T) {
	db := newTestKV(t)
	if err := kvSetBool(db, orchestrationKey, true); err != nil {
		t.Fatalf("kvSetBool(true): %v", err)
	}
	if !kvGetBool(db, orchestrationKey) {
		t.Error("esperaba true tras set true")
	}
	if err := kvSetBool(db, orchestrationKey, false); err != nil {
		t.Fatalf("kvSetBool(false): %v", err)
	}
	if kvGetBool(db, orchestrationKey) {
		t.Error("esperaba false tras set false")
	}
}

// TestKVGetBoolLegacyValue: un valor "true" en el kv también cuenta como activo.
func TestKVGetBoolLegacyValue(t *testing.T) {
	db := newTestKV(t)
	if _, err := db.Exec(`INSERT INTO kv (key, value) VALUES (?, 'true')`, orchestrationKey); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !kvGetBool(db, orchestrationKey) {
		t.Error("esperaba true para valor 'true'")
	}
}
