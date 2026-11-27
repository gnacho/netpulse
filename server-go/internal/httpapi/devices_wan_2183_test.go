package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// wanSideAdapter añade un vecino del segmento WAN al fixture demo.
type wanSideAdapter struct{ adapters.Snapshotter }

func (w wanSideAdapter) GetDevices(ctx context.Context) []adapters.Device {
	return append(w.Snapshotter.GetDevices(ctx), adapters.Device{
		ID:       "0c-ef-15-00-00-02",
		MAC:      "0C:EF:15:00:00:02",
		Name:     "0C:EF:15:00:00:02",
		IP:       "203.0.113.248",
		Online:   true,
		Band:     "cable",
		Type:     "unknown",
		RouterID: "gw",
		WanSide:  true,
	})
}

// #2183: el inventario no lista vecinos del segmento WAN (ISP) y se pueden
// pedir explícitamente con includeWan=1. La topología sigue viéndolos porque
// usa el snapshot del overview, no este endpoint.
func TestDevicesExcludesWanSideNeighbours(t *testing.T) {
	ts := makeTestServerWithAdapter(t, false, func(s adapters.Snapshotter) adapters.Snapshotter {
		return wanSideAdapter{Snapshotter: s}
	})
	status, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	if status != http.StatusNoContent {
		t.Fatalf("login: %d", status)
	}

	res := get(t, ts.URL, "/api/devices?pageSize=200", cookie)
	body := readJSON(t, res)
	items := body["items"].([]any)
	for _, it := range items {
		if it.(map[string]any)["mac"] == "0C:EF:15:00:00:02" {
			t.Fatalf("el vecino WAN no puede aparecer en el inventario: %v", it)
		}
	}
	without := int(body["total"].(float64))

	res = get(t, ts.URL, "/api/devices?pageSize=200&includeWan=1", cookie)
	body = readJSON(t, res)
	with := int(body["total"].(float64))
	found := false
	for _, it := range body["items"].([]any) {
		if it.(map[string]any)["mac"] == "0C:EF:15:00:00:02" {
			found = true
		}
	}
	if !found {
		t.Fatalf("includeWan=1 debe devolver el vecino WAN: %v", body["items"])
	}
	if with != without+1 {
		t.Fatalf("includeWan=1 suma solo el vecino WAN: sin=%d con=%d", without, with)
	}
	// typeCounts sigue el mismo criterio de visibilidad.
	sum := 0
	for _, v := range body["typeCounts"].(map[string]any) {
		sum += int(v.(float64))
	}
	if sum != with {
		t.Fatalf("typeCounts con includeWan suma %d, total %d", sum, with)
	}
}
