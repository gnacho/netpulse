// roam_events_test.go — filtro ?mac de GET /api/roam-events.
package httpapi_test

import (
	"net/http"
	"testing"
)

// TestRoamEventsNormalizesMAC: ?mac acepta guiones/minúsculas (también en
// lista separada por comas) y rechaza valores que no son una MAC.
func TestRoamEventsNormalizesMAC(t *testing.T) {
	srv := makeTestServer(t)
	if _, err := srv.db.Exec(
		"INSERT INTO roam_events (ts_ms, router_id, type, mac, iface, detail, content_hash) VALUES (?,?,?,?,?,?,?)",
		1000, "rt1", "assoc", "AA:BB:CC:DD:EE:01", "phy0-ap0", "", "h1",
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	for _, q := range []string{
		"aa-bb-cc-dd-ee-01",
		"aa:bb:cc:dd:ee:01",
		"aa-bb-cc-dd-ee-01,11:22:33:44:55:66",
	} {
		res := get(t, srv.URL, "/api/roam-events?mac="+q, cookie)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("mac=%s: got %d want 200", q, res.StatusCode)
		}
		events, _ := readJSON(t, res)["events"].([]any)
		if len(events) != 1 {
			t.Fatalf("mac=%s: esperaba 1 evento, obtuve %v", q, events)
		}
	}

	res := get(t, srv.URL, "/api/roam-events?mac=nope", cookie)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("mac inválida: got %d want 400", res.StatusCode)
	}
}
