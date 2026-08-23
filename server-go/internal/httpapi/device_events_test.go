// device_events_test.go — contrato del endpoint GET /api/device-events
// (issue #184): eventos offline/online de dispositivos.
package httpapi_test

import (
	"net/http"
	"testing"
)

// TestDeviceEventsProtegido: sin sesión → 401; con sesión → 200 con lista.
func TestDeviceEventsProtegido(t *testing.T) {
	srv := makeTestServer(t)

	res := get(t, srv.URL, "/api/device-events", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sin cookie: got %d want 401", res.StatusCode)
	}
	body := readJSON(t, res)
	if body["error"] != "unauthorized" {
		t.Fatalf("error: %v", body)
	}

	// Login y siembra de 2 eventos (la tabla la crea el schema al arrancar).
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if _, err := srv.db.Exec(
		"INSERT INTO device_events (ts_ms, mac, router_id, state, signal_dbm) VALUES (?,?,?,?,?), (?,?,?,?,?)",
		1000, "AA:BB:CC:DD:EE:01", "rt1", "offline", nil,
		2000, "AA:BB:CC:DD:EE:01", "rt1", "online", -45,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res2 := get(t, srv.URL, "/api/device-events", cookie)
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("con cookie: got %d want 200", res2.StatusCode)
	}
	body2 := readJSON(t, res2)
	events, ok := body2["events"].([]any)
	if !ok || len(events) != 2 {
		t.Fatalf("events: %v", body2["events"])
	}
	// Orden DESC por ts_ms: primero online (2000).
	first := events[0].(map[string]any)
	if first["state"] != "online" || first["mac"] != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("primero: %v", first)
	}
}

// TestDeviceEventsFiltros: mac y state filtran; limit acota; sin auth → 401.
func TestDeviceEventsFiltros(t *testing.T) {
	srv := makeTestServer(t)
	if _, err := srv.db.Exec(
		"INSERT INTO device_events (ts_ms, mac, router_id, state) VALUES (?,?,?,?), (?,?,?,?), (?,?,?,?)",
		1000, "AA:BB:CC:DD:EE:01", "rt1", "offline",
		2000, "AA:BB:CC:DD:EE:01", "rt1", "online",
		3000, "AA:BB:CC:DD:EE:02", "rt2", "offline",
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// Filtro state=online → 1.
	res := get(t, srv.URL, "/api/device-events?state=online", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("state filter: got %d want 200", res.StatusCode)
	}
	if events := readJSON(t, res)["events"].([]any); len(events) != 1 {
		t.Fatalf("state=online: %d, want 1", len(events))
	}

	// Filtro mac → 2.
	res = get(t, srv.URL, "/api/device-events?mac=AA:BB:CC:DD:EE:01", cookie)
	if events := readJSON(t, res)["events"].([]any); len(events) != 2 {
		t.Fatalf("mac filter: %d, want 2", len(events))
	}

	// Filtro router=rt2 → 1.
	res = get(t, srv.URL, "/api/device-events?router=rt2", cookie)
	if events := readJSON(t, res)["events"].([]any); len(events) != 1 {
		t.Fatalf("router filter: %d, want 1", len(events))
	}

	// limit=1 → 1.
	res = get(t, srv.URL, "/api/device-events?limit=1", cookie)
	if events := readJSON(t, res)["events"].([]any); len(events) != 1 {
		t.Fatalf("limit=1: %d, want 1", len(events))
	}

	// since=2000 → solo ts>=2000 (2 eventos).
	res = get(t, srv.URL, "/api/device-events?since=2000", cookie)
	if events := readJSON(t, res)["events"].([]any); len(events) != 2 {
		t.Fatalf("since=2000: %d, want 2", len(events))
	}
}
