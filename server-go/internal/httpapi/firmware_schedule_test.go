// firmware_schedule_test.go — endpoints de programación de upgrades (#494).
// Sin agente conectado en ningún test: nunca se toca un router ni firmware real.
package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/firmware"
)

// findFirmwareItem busca el item del router en la lista de upgrades.
func findFirmwareItem(t *testing.T, arr []any, routerID string) map[string]any {
	t.Helper()
	for _, v := range arr {
		item := v.(map[string]any)
		if item["routerId"] == routerID {
			return item
		}
	}
	t.Fatalf("router %s no aparece en la lista", routerID)
	return nil
}

func deleteReq(t *testing.T, base, path, cookie string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("DELETE", base+path, nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return res
}

// TestFirmwareScheduleAndCancel (#494): sin target → 400 no_target; con target
// → programa (lista expone status scheduled + scheduledFor); DELETE cancela.
func TestFirmwareScheduleAndCancel(t *testing.T) {
	srv, _ := firmwareTestServer(t)
	cookie := adminCookieFor(t, srv)
	rid := addTestRouter(t, srv.db)

	// Sin target: 400 no_target.
	res := postJSON(t, srv.URL, "/api/firmware-upgrades/"+rid+"/schedule", `{"scheduledFor": 9999999999999}`, cookie)
	body := readJSON(t, res)
	if res.StatusCode != 400 || body["error"] != "no_target" {
		t.Fatalf("esperaba 400 no_target, got %d %v", res.StatusCode, body)
	}
	res.Body.Close()

	// Configurar target.
	payload := `{"model":"glinet-flint2","currentVersion":"23.05.3","targetVersion":"23.05.4","targetUrl":"http://x/image.bin","checksum":"abc"}`
	postJSON(t, srv.URL, "/api/firmware-upgrades/"+rid+"/target", payload, cookie).Body.Close()

	// Programar para mañana.
	scheduledFor := time.Now().Add(24 * time.Hour).UnixMilli()
	res = postJSON(t, srv.URL, "/api/firmware-upgrades/"+rid+"/schedule", fmt.Sprintf(`{"scheduledFor": %d}`, scheduledFor), cookie)
	body = readJSON(t, res)
	if res.StatusCode != 200 {
		t.Fatalf("POST schedule: %d %v", res.StatusCode, body)
	}
	res.Body.Close()

	// La lista expone la programación.
	res = get(t, srv.URL, "/api/firmware-upgrades", cookie)
	arr := readJSONArray(t, res)
	res.Body.Close()
	item := findFirmwareItem(t, arr, rid)
	up, ok := item["upgrade"].(map[string]any)
	if !ok {
		t.Fatalf("sin upgrade en el item: %v", item)
	}
	if up["status"] != "scheduled" {
		t.Fatalf("status: %v", up["status"])
	}
	if int64(up["scheduledFor"].(float64)) != scheduledFor {
		t.Fatalf("scheduledFor: %v != %d", up["scheduledFor"], scheduledFor)
	}

	// Cancelar: la lista ya no expone upgrade.
	res = deleteReq(t, srv.URL, "/api/firmware-upgrades/"+rid+"/schedule", cookie)
	body = readJSON(t, res)
	if res.StatusCode != 200 {
		t.Fatalf("DELETE schedule: %d %v", res.StatusCode, body)
	}
	res.Body.Close()
	res = get(t, srv.URL, "/api/firmware-upgrades", cookie)
	arr = readJSONArray(t, res)
	res.Body.Close()
	item = findFirmwareItem(t, arr, rid)
	if _, ok := item["upgrade"].(map[string]any); ok {
		t.Fatalf("tras cancelar no debería haber upgrade: %v", item["upgrade"])
	}
}

// TestFirmwareScheduleInProgress (#494): con un upgrade en curso se rechaza la
// programación (400 upgrade_in_progress).
func TestFirmwareScheduleInProgress(t *testing.T) {
	srv, _ := firmwareTestServer(t)
	cookie := adminCookieFor(t, srv)
	rid := addTestRouter(t, srv.db)

	payload := `{"model":"glinet-flint2","currentVersion":"23.05.3","targetVersion":"23.05.4","targetUrl":"http://x/image.bin","checksum":"abc"}`
	postJSON(t, srv.URL, "/api/firmware-upgrades/"+rid+"/target", payload, cookie).Body.Close()

	// Upgrade en curso (requested) sembrado directamente en el store, sin
	// agente: simula una operación viva.
	if _, err := firmware.NewStore(srv.db.DB).BeginUpgrade(rid, "23.05.4", "http://x/image.bin", "abc"); err != nil {
		t.Fatalf("begin: %v", err)
	}

	res := postJSON(t, srv.URL, "/api/firmware-upgrades/"+rid+"/schedule", `{"scheduledFor": 9999999999999}`, cookie)
	body := readJSON(t, res)
	if res.StatusCode != 400 || body["error"] != "upgrade_in_progress" {
		t.Fatalf("esperaba 400 upgrade_in_progress, got %d %v", res.StatusCode, body)
	}
	res.Body.Close()
}
