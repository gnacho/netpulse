// presence_api_test.go — contrato de los endpoints de presencia (#771).
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/deviceevents"
)

// TestDevicePresence devuelve los tramos fusionados de una MAC con eventos
// reales en BD: intervalo cerrado por offline + tramo abierto actual.
func TestDevicePresence(t *testing.T) {
	ts := makeDeviceActionsTestServer(t, nil)
	defer ts.Server.Close()

	mac := "AA:BB:CC:DD:EE:07"
	base := time.Now().Add(-2 * time.Hour).UnixMilli()
	if err := deviceevents.Insert(ts.db.DB, deviceevents.Event{TsMs: base, MAC: mac, RouterID: "gw", State: deviceevents.StateOnline}); err != nil {
		t.Fatalf("insert online: %v", err)
	}
	if err := deviceevents.Insert(ts.db.DB, deviceevents.Event{TsMs: base + 3600_000, MAC: mac, RouterID: "gw", State: deviceevents.StateOffline}); err != nil {
		t.Fatalf("insert offline: %v", err)
	}
	if err := deviceevents.Insert(ts.db.DB, deviceevents.Event{TsMs: base + 3700_000, MAC: mac, RouterID: "gw", State: deviceevents.StateOnline}); err != nil {
		t.Fatalf("insert online 2: %v", err)
	}

	resp := deviceReq(t, "GET", ts.Server.URL, "/api/devices/"+mac+"/presence?days=7", ts.cookie, "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET presence: %d, want 200", resp.StatusCode)
	}
	var body struct {
		MAC       string `json:"mac"`
		Intervals []struct {
			RouterID string `json:"routerId"`
			StartMs  int64  `json:"startMs"`
			EndMs    *int64 `json:"endMs"`
		} `json:"intervals"`
		Events []struct {
			State string `json:"state"`
		} `json:"events"`
		Connects24h int `json:"connects24h"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Intervals) != 2 {
		t.Fatalf("intervals = %d, want 2 (cerrado + abierto)", len(body.Intervals))
	}
	if body.Intervals[0].EndMs == nil {
		t.Fatal("el primer tramo debe estar cerrado por el offline")
	}
	if body.Intervals[1].EndMs != nil {
		t.Fatal("el segundo tramo debe quedar abierto (endMs null)")
	}
	if len(body.Events) != 3 {
		t.Fatalf("events = %d, want 3", len(body.Events))
	}

	// MAC inválida → 400.
	resp2 := deviceReq(t, "GET", ts.Server.URL, "/api/devices/xx/presence", ts.cookie, "")
	defer resp2.Body.Close()
	io.Copy(io.Discard, resp2.Body)
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET presence MAC inválida: %d, want 400", resp2.StatusCode)
	}
}

// TestPresenceSettingsRoundtrip: PUT retención → GET la devuelve; fuera de
// rango → 400.
func TestPresenceSettingsRoundtrip(t *testing.T) {
	ts := makeDeviceActionsTestServer(t, nil)
	defer ts.Server.Close()

	resp := deviceReq(t, "PUT", ts.Server.URL, "/api/settings/presence", ts.cookie, `{"retention_days":14}`)
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("PUT presence settings: %d, want 200", resp.StatusCode)
	}

	resp2 := deviceReq(t, "GET", ts.Server.URL, "/api/settings/presence", ts.cookie, "")
	defer resp2.Body.Close()
	var body struct {
		RetentionDays int `json:"retention_days"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.RetentionDays != 14 {
		t.Fatalf("retention_days = %d, want 14", body.RetentionDays)
	}

	resp3 := deviceReq(t, "PUT", ts.Server.URL, "/api/settings/presence", ts.cookie, `{"retention_days":999}`)
	defer resp3.Body.Close()
	io.Copy(io.Discard, resp3.Body)
	if resp3.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT retención inválida: %d, want 400", resp3.StatusCode)
	}
}

// TestPresenceRoaming lista las MACs con más de un connect en 24 h.
func TestPresenceRoaming(t *testing.T) {
	ts := makeDeviceActionsTestServer(t, nil)
	defer ts.Server.Close()

	now := time.Now().UnixMilli()
	for i := 0; i < 3; i++ {
		if _, err := ts.db.Exec(`INSERT INTO roam_events (ts_ms, router_id, type, mac, content_hash)
			VALUES (?, 'gw', 'connected', 'AA:BB:CC:DD:EE:08', ?)`, now-int64(i)*3600_000, "c8-"+string(rune('0'+i))); err != nil {
			t.Fatalf("insert roam: %v", err)
		}
	}

	resp := deviceReq(t, "GET", ts.Server.URL, "/api/presence/roaming", ts.cookie, "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET roaming: %d, want 200", resp.StatusCode)
	}
	var body struct {
		Items []struct {
			MAC      string `json:"mac"`
			Connects int    `json:"connects"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Connects != 3 {
		t.Fatalf("roaming items = %+v, want 1 con 3", body.Items)
	}
}
