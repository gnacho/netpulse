package roamevents

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestParseLogreadFlint2 usa líneas reales de logread del Flint2 (capturadas
// vía SSH durante el diseño de Fase 14.5).
func TestParseLogreadFlint2(t *testing.T) {
	lines := []string{
		"Sat Aug  8 19:21:45 2026 daemon.notice hostapd: wlan1: BEACON-RESP-RX 1e:4e:75:a0:6f:6f 35 04",
		"Sat Aug  8 19:21:58 2026 daemon.warn dawn: Client / BSSID = 12:E6:F7:45:D4:40 / 1E:C1:05:C9:48:0D: BEACON REQUEST failed",
	}
	// Línea 1: BEACON-RESP-RX → no casa ningún patrón (descartada).
	ev, ok := ParseLogreadLine(lines[0], "gateway")
	if ok {
		t.Errorf("BEACON-RESP-RX debería descartarse, got %+v", ev)
	}
	// Línea 2: dawn Client/BSSID → dawn_decision.
	ev, ok = ParseLogreadLine(lines[1], "gateway")
	if !ok {
		t.Fatalf("dawn line debería parsear")
	}
	if ev.Type != TypeDawnDecision {
		t.Errorf("type = %q, want %q", ev.Type, TypeDawnDecision)
	}
	if ev.MAC != "12:E6:F7:45:D4:40" {
		t.Errorf("mac = %q", ev.MAC)
	}
	if ev.RouterID != "gateway" {
		t.Errorf("routerID = %q", ev.RouterID)
	}
	if ev.Detail == "" || !contains(ev.Detail, "BEACON REQUEST failed") {
		t.Errorf("detail = %q", ev.Detail)
	}
}

// TestParseUsteer cubre las líneas de usteer (user.info usteer:) con
// "station MAC connected/disconnected to/from node".
func TestParseUsteer(t *testing.T) {
	cases := []struct {
		line string
		want string
		mac  string
	}{
		{
			"Sat Aug  8 19:21:58 2026 user.info usteer: station 04:95:e6:76:55:a1 connected to node 04:95:e6:76:55:b2",
			TypeConnected,
			"04:95:e6:76:55:a1",
		},
		{
			"Sat Aug  8 19:21:59 2026 user.info usteer: station 04:95:e6:76:55:a1 disconnected from node 04:95:e6:76:55:b2",
			TypeDisconnected,
			"04:95:e6:76:55:a1",
		},
	}
	for _, tc := range cases {
		ev, ok := ParseLogreadLine(tc.line, "rt3")
		if !ok {
			t.Fatalf("línea usteer debería parsear: %q", tc.line)
		}
		if ev.Type != tc.want {
			t.Errorf("type = %q, want %q", ev.Type, tc.want)
		}
		if ev.MAC != tc.mac {
			t.Errorf("mac = %q, want %q", ev.MAC, tc.mac)
		}
		if ev.Detail == "" || !contains(ev.Detail, "node") {
			t.Errorf("detail = %q", ev.Detail)
		}
	}
}

// TestParseHostapdConnected cubre las dos variantes (con y sin segundo iface).
func TestParseHostapdConnected(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		want  string
		iface string
	}{
		{
			name:  "long-form",
			line:  "Sat Aug  8 19:21:45 2026 daemon.notice hostapd: wlan0: AP-STA-CONNECTED wlan0 04:95:e6:76:55:a1",
			want:  TypeConnected,
			iface: "wlan0",
		},
		{
			name:  "short-form",
			line:  "Sat Aug  8 19:21:45 2026 daemon.notice hostapd: AP-STA-CONNECTED 04:95:e6:76:55:a1",
			want:  TypeConnected,
			iface: "",
		},
		{
			name:  "disconnect",
			line:  "Sat Aug  8 19:21:45 2026 daemon.notice hostapd: wlan1: AP-STA-DISCONNECTED wlan1 04:95:e6:76:55:a1",
			want:  TypeDisconnected,
			iface: "wlan1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, ok := ParseLogreadLine(c.line, "rt1")
			if !ok {
				t.Fatalf("no parseó")
			}
			if ev.Type != c.want {
				t.Errorf("type = %q, want %q", ev.Type, c.want)
			}
			if ev.Iface != c.iface {
				t.Errorf("iface = %q, want %q", ev.Iface, c.iface)
			}
			if ev.MAC != "04:95:e6:76:55:a1" {
				t.Errorf("mac = %q", ev.MAC)
			}
		})
	}
}

// TestParseUnknownLine cubre líneas no reconocidas (deben descartarse).
func TestParseUnknownLine(t *testing.T) {
	cases := []string{
		"",
		"unrelated log line",
		"random daemon.notice foo: bar baz",
	}
	for _, line := range cases {
		_, ok := ParseLogreadLine(line, "rt1")
		if ok {
			t.Errorf("línea %q no debería parsear", line)
		}
	}
}

// TestParseSyslogTimeYearWrap cubre el salto de año: evento de 31 dic
// parseado en enero debe caer al año pasado.
func TestParseSyslogTimeYearWrap(t *testing.T) {
	// Suponemos que hoy es agosto: evento "Dec 31 23:59:59" → año pasado.
	ts := parseSyslogTime("Dec", "31", "23", "59", "59")
	if ts == 0 {
		t.Fatal("ts = 0")
	}
	parsed := time.UnixMilli(ts)
	now := time.Now()
	if parsed.After(now) {
		t.Errorf("evento Dec 31 debería ser pasado, got %v (now %v)", parsed, now)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// openMemDB abre SQLite en memoria y crea solo el schema de roam_events.
// (db.Open arranca goroutines de mantenimiento que cuelgan el test runner.)
func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	schema := `
CREATE TABLE roam_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts_ms INTEGER NOT NULL,
  router_id TEXT NOT NULL,
  type TEXT NOT NULL,
  mac TEXT, iface TEXT, detail TEXT,
  content_hash TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_roam_events_dedup ON roam_events(content_hash);
CREATE INDEX idx_roam_events_ts ON roam_events(ts_ms DESC);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// TestInsertEventDedup verifica que una línea idéntica no se inserta dos
// veces (gracias a content_hash UNIQUE) y que un evento distinto sí entra.
func TestInsertEventDedup(t *testing.T) {
	db := openMemDB(t)
	ev := Event{
		TsMs: 1700000000000, RouterID: "rt1", Type: TypeConnected,
		MAC: "AA:BB:CC:DD:EE:FF", Iface: "wlan0",
	}
	if err := InsertEvent(db, ev); err != nil {
		t.Fatalf("insert 1: %v", err)
	}
	if err := InsertEvent(db, ev); err != nil {
		t.Fatalf("insert 2 (dedup): %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM roam_events").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("dedup esperado 1 fila, got %d", n)
	}

	// Evento distinto (mac diferente) en mismo minuto → se inserta.
	ev2 := ev
	ev2.MAC = "11:22:33:44:55:66"
	if err := InsertEvent(db, ev2); err != nil {
		t.Fatalf("insert distinto: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM roam_events").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("tras evento distinto esperado 2 filas, got %d", n)
	}
}

// TestListEventsFilters cubre los filtros del endpoint.
func TestListEventsFilters(t *testing.T) {
	db := openMemDB(t)
	events := []Event{
		{TsMs: 1000, RouterID: "rt1", Type: TypeConnected, MAC: "AA:AA:AA:AA:AA:01", Iface: "wlan0"},
		{TsMs: 2000, RouterID: "rt2", Type: TypeDisconnected, MAC: "AA:AA:AA:AA:AA:02", Iface: "wlan1"},
		{TsMs: 3000, RouterID: "rt1", Type: TypeDawnDecision, MAC: "AA:AA:AA:AA:AA:03"},
	}
	for _, ev := range events {
		if err := InsertEvent(db, ev); err != nil {
			t.Fatal(err)
		}
	}

	// Sin filtro: 3 eventos, orden DESC por ts.
	got, err := ListEvents(db, 100, 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("sin filtro: %d, want 3", len(got))
	}
	if got[0].TsMs != 3000 {
		t.Errorf("primero debería ser ts=3000 (DESC), got %d", got[0].TsMs)
	}

	got, _ = ListEvents(db, 100, 0, "rt1", "")
	if len(got) != 2 {
		t.Errorf("filtro router rt1: %d, want 2", len(got))
	}

	got, _ = ListEvents(db, 100, 0, "", TypeConnected)
	if len(got) != 1 {
		t.Errorf("filtro tipo connected: %d, want 1", len(got))
	}

	got, _ = ListEvents(db, 100, 2000, "", "")
	if len(got) != 2 {
		t.Errorf("filtro since 2000: %d, want 2", len(got))
	}
}

