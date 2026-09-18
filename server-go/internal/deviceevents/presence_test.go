package deviceevents

import (
	"database/sql"
	"testing"
	"time"
)

// openTestDB crea SQLite en memoria con el schema mínimo de eventos.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, err = d.Exec(`CREATE TABLE device_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT, ts_ms INTEGER NOT NULL, mac TEXT NOT NULL,
		router_id TEXT, state TEXT NOT NULL, signal_dbm INTEGER, detail TEXT);
	CREATE TABLE roam_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT, ts_ms INTEGER NOT NULL, router_id TEXT NOT NULL,
		type TEXT NOT NULL, mac TEXT, iface TEXT, detail TEXT, content_hash TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func insertDev(t *testing.T, db *sql.DB, ts int64, mac, router, state string) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO device_events (ts_ms, mac, router_id, state) VALUES (?,?,?,?)", ts, mac, router, state); err != nil {
		t.Fatalf("insert device_event: %v", err)
	}
}

func insertRoam(t *testing.T, db *sql.DB, ts int64, mac, router, typ string) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO roam_events (ts_ms, router_id, type, mac, content_hash) VALUES (?,?,?,?,?)", ts, router, typ, mac, typ+router); err != nil {
		t.Fatalf("insert roam_event: %v", err)
	}
}

func TestIntervalsOnlineOffline(t *testing.T) {
	db := openTestDB(t)
	base := time.Now().Add(-time.Hour).UnixMilli()
	mac := "AA:BB:CC:DD:EE:01"
	insertDev(t, db, base, mac, "rt1", StateOnline)
	end := base + 30*60*1000
	insertDev(t, db, end, mac, "rt1", StateOffline)

	ivs, err := Intervals(db, mac, 0)
	if err != nil {
		t.Fatalf("Intervals: %v", err)
	}
	if len(ivs) != 1 {
		t.Fatalf("len(intervals) = %d, want 1", len(ivs))
	}
	if ivs[0].RouterID != "rt1" || ivs[0].StartMs != base {
		t.Fatalf("intervalo = %+v", ivs[0])
	}
	if ivs[0].EndMs == nil || *ivs[0].EndMs != end {
		t.Fatalf("EndMs = %v, want %d", ivs[0].EndMs, end)
	}
}

func TestIntervalsOpenEndsNil(t *testing.T) {
	db := openTestDB(t)
	mac := "AA:BB:CC:DD:EE:02"
	insertDev(t, db, 1000, mac, "rt1", StateOnline)
	ivs, err := Intervals(db, mac, 0)
	if err != nil {
		t.Fatalf("Intervals: %v", err)
	}
	if len(ivs) != 1 || ivs[0].EndMs != nil {
		t.Fatalf("intervalo abierto: %+v", ivs)
	}
}

func TestIntervalsRoamSplitsInterval(t *testing.T) {
	db := openTestDB(t)
	mac := "AA:BB:CC:DD:EE:03"
	base := int64(1000000)
	insertDev(t, db, base, mac, "rt1", StateOnline)
	// El cliente salta a rt2: CONNECTED en rt2 cierra el tramo y abre otro.
	insertRoam(t, db, base+10*60*1000, mac, "rt2", "connected")

	ivs, err := Intervals(db, mac, 0)
	if err != nil {
		t.Fatalf("Intervals: %v", err)
	}
	if len(ivs) != 2 {
		t.Fatalf("len(intervals) = %d, want 2 (roam divide)", len(ivs))
	}
	if ivs[0].RouterID != "rt1" || ivs[1].RouterID != "rt2" {
		t.Fatalf("routers = [%s %s], want [rt1 rt2]", ivs[0].RouterID, ivs[1].RouterID)
	}
	if ivs[0].EndMs == nil || ivs[1].StartMs != base+10*60*1000 {
		t.Fatalf("corte de roam incorrecto: %+v / %+v", ivs[0], ivs[1])
	}
	if ivs[1].EndMs != nil {
		t.Fatalf("el segundo tramo debe quedar abierto: %+v", ivs[1])
	}
}

func TestIntervalsSameRouterOnlineIsNoop(t *testing.T) {
	db := openTestDB(t)
	mac := "AA:BB:CC:DD:EE:04"
	insertDev(t, db, 1000, mac, "rt1", StateOnline)
	insertDev(t, db, 2000, mac, "rt1", StateOnline) // reconexión rápida: no-op
	ivs, err := Intervals(db, mac, 0)
	if err != nil {
		t.Fatalf("Intervals: %v", err)
	}
	if len(ivs) != 1 {
		t.Fatalf("len(intervals) = %d, want 1 (mismo router no divide)", len(ivs))
	}
}

func TestRoamCountsAndPrune(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UnixMilli()
	old := now - 40*24*3600*1000
	// MAC flap: 5 connects recientes; MAC estable: 1 (no entra, HAVING > 1).
	for i := 0; i < 5; i++ {
		insertRoam(t, db, now-int64(i)*3600*1000, "AA:BB:CC:DD:EE:05", "rt1", "connected")
	}
	insertRoam(t, db, now, "AA:BB:CC:DD:EE:06", "rt1", "connected")
	insertRoam(t, db, old, "AA:BB:CC:DD:EE:05", "rt1", "connected") // viejo: se poda

	rc, err := RoamCounts(db, now-24*3600*1000, 20)
	if err != nil {
		t.Fatalf("RoamCounts: %v", err)
	}
	if len(rc) != 1 || rc[0].Connects != 5 {
		t.Fatalf("RoamCounts = %+v, want 1 entrada con 5", rc)
	}
	n, err := CountConnects(db, "AA:BB:CC:DD:EE:05", now-24*3600*1000)
	if err != nil || n != 5 {
		t.Fatalf("CountConnects = %d, %v; want 5", n, err)
	}

	cutoff := now - 30*24*3600*1000
	deleted, err := Prune(db, cutoff)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 { // solo el roam viejo (device_events no tiene filas viejas)
		t.Fatalf("Prune deleted = %d, want 1", deleted)
	}
	rc, _ = RoamCounts(db, 0, 20)
	if len(rc) != 1 || rc[0].Connects != 5 {
		t.Fatalf("tras poda RoamCounts = %+v", rc)
	}
}
