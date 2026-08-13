package deviceevents

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// openMemDB abre SQLite en memoria con el schema mínimo de device_events.
// (db.Open arranca goroutines de mantenimiento que cuelgan el test runner.)
func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	schema := `
CREATE TABLE device_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts_ms INTEGER NOT NULL,
  mac TEXT NOT NULL,
  router_id TEXT,
  state TEXT NOT NULL,
  signal_dbm INTEGER,
  detail TEXT
);
CREATE INDEX idx_device_events_ts ON device_events(ts_ms DESC);
CREATE INDEX idx_device_events_mac_ts ON device_events(mac, ts_ms DESC);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// TestInsertAndList cubre insert + lectura con todos los filtros y orden DESC.
func TestInsertAndList(t *testing.T) {
	db := openMemDB(t)
	events := []Event{
		{TsMs: 1000, MAC: "AA:AA:AA:AA:AA:01", RouterID: "rt1", State: "offline", SignalDbm: nil, Detail: ""},
		{TsMs: 2000, MAC: "AA:AA:AA:AA:AA:02", RouterID: "rt2", State: "online", SignalDbm: intPtr(-45), Detail: "resumed"},
		{TsMs: 3000, MAC: "AA:AA:AA:AA:AA:01", RouterID: "rt1", State: "online", SignalDbm: intPtr(-50), Detail: "back"},
	}
	for _, ev := range events {
		if err := Insert(db, ev); err != nil {
			t.Fatalf("Insert(%+v): %v", ev, err)
		}
	}

	// Sin filtro: 3 eventos, orden DESC por ts.
	got, err := ListEvents(db, 100, 0, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("sin filtro: %d, want 3", len(got))
	}
	if got[0].TsMs != 3000 {
		t.Errorf("primero debería ser ts=3000 (DESC), got %d", got[0].TsMs)
	}
	if got[0].State != "online" || got[0].MAC != "AA:AA:AA:AA:AA:01" {
		t.Errorf("fila ts=3000 mal: %+v", got[0])
	}

	// Filtro mac.
	got, err = ListEvents(db, 100, 0, "", "AA:AA:AA:AA:AA:01", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("filtro mac: %d, want 2", len(got))
	}

	// Filtro state.
	got, err = ListEvents(db, 100, 0, "", "", "offline")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].State != "offline" {
		t.Fatalf("filtro state: %+v, want 1 offline", got)
	}

	// Filtro router.
	got, err = ListEvents(db, 100, 0, "rt2", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RouterID != "rt2" {
		t.Fatalf("filtro router: %+v, want 1 rt2", got)
	}

	// Filtro since.
	got, err = ListEvents(db, 100, 2000, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("filtro since: %d, want 2", len(got))
	}

	// limit acota.
	got, err = ListEvents(db, 1, 0, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("limit 1: %d, want 1", len(got))
	}

	// Sin datos → slice no-nil.
	if _, err := db.Exec("DELETE FROM device_events"); err != nil {
		t.Fatal(err)
	}
	got, err = ListEvents(db, 100, 0, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Errorf("ListEvents vacío debería devolver []Event{} no-nil, got nil")
	}
}

func intPtr(v int) *int { return &v }
