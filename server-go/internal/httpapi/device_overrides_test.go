// device_overrides_test.go — issue #797: nombre visible y tipo de dispositivo
// como overrides persistidos (además del icono existente, #437).
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func newOverrideTestServer(t *testing.T) *server {
	t.Helper()
	dataDir := t.TempDir()
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return &server{db: d}
}

func doOverridePut(t *testing.T, s *server, mac, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT", "/api/devices/"+mac+"/override", strings.NewReader(body))
	req.SetPathValue("mac", mac)
	rec := httptest.NewRecorder()
	s.handleDeviceOverridePut(rec, req)
	return rec
}

func doOverrideGet(t *testing.T, s *server, mac string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/devices/"+mac+"/override", nil)
	req.SetPathValue("mac", mac)
	rec := httptest.NewRecorder()
	s.handleDeviceOverrideGet(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("get decode: %v", err)
	}
	return out
}

func TestDeviceOverrideNameTypeRoundtrip(t *testing.T) {
	s := newOverrideTestServer(t)
	mac := "aa:bb:cc:dd:ee:ff"
	rec := doOverridePut(t, s, mac, `{"icon":"tv","name":"TV Salón","type":"tv"}`)
	if rec.Code != 200 {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	got := doOverrideGet(t, s, mac)
	if got["icon"] != "tv" || got["name"] != "TV Salón" || got["type"] != "tv" {
		t.Errorf("roundtrip incompleto: %+v", got)
	}
}

func TestDeviceOverridePartialPutPreservesOthers(t *testing.T) {
	s := newOverrideTestServer(t)
	mac := "aa:bb:cc:dd:ee:ff"
	doOverridePut(t, s, mac, `{"icon":"tv","name":"TV Salón","type":"tv"}`)
	// Cliente antiguo que solo envía icon (#772): name/type no se tocan.
	rec := doOverridePut(t, s, mac, `{"icon":"speaker"}`)
	if rec.Code != 200 {
		t.Fatalf("put parcial: %d", rec.Code)
	}
	got := doOverrideGet(t, s, mac)
	if got["icon"] != "speaker" || got["name"] != "TV Salón" || got["type"] != "tv" {
		t.Errorf("el put parcial pisó otros campos: %+v", got)
	}
	// Limpiar solo el nombre.
	doOverridePut(t, s, mac, `{"name":""}`)
	got = doOverrideGet(t, s, mac)
	if got["name"] != "" || got["icon"] != "speaker" || got["type"] != "tv" {
		t.Errorf("limpiar name no funcionó: %+v", got)
	}
}

func TestDeviceOverrideClearAllDeletesRow(t *testing.T) {
	s := newOverrideTestServer(t)
	mac := "aa:bb:cc:dd:ee:ff"
	doOverridePut(t, s, mac, `{"icon":"tv","name":"TV","type":"tv"}`)
	rec := doOverridePut(t, s, mac, `{"icon":"","name":"","type":""}`)
	if rec.Code != 200 {
		t.Fatalf("clear: %d", rec.Code)
	}
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM device_overrides WHERE mac = ?", mac).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("la fila debió borrarse con los tres campos vacíos")
	}
}

func TestDeviceOverrideValidation(t *testing.T) {
	s := newOverrideTestServer(t)
	mac := "aa:bb:cc:dd:ee:ff"
	if rec := doOverridePut(t, s, mac, `{"type":"router-magico"}`); rec.Code != 400 {
		t.Errorf("tipo inválido debió dar 400, dio %d", rec.Code)
	}
	if rec := doOverridePut(t, s, mac, `{"icon":"icono-falso"}`); rec.Code != 400 {
		t.Errorf("icon inválido debió dar 400, dio %d", rec.Code)
	}
	if rec := doOverridePut(t, s, "not-a-mac", `{"name":"x"}`); rec.Code != 400 {
		t.Errorf("mac inválida debió dar 400, dio %d", rec.Code)
	}
	// Nombre con salto de línea: rechazado.
	if rec := doOverridePut(t, s, mac, `{"name":"a\nb"}`); rec.Code != 400 {
		t.Errorf("name con newline debió dar 400, dio %d", rec.Code)
	}
	// Tipo válido del clasificador: aceptado.
	if rec := doOverridePut(t, s, mac, `{"type":"movil"}`); rec.Code != 200 {
		t.Errorf("tipo válil debió dar 200, dio %d", rec.Code)
	}
}

// #1145: DELETE /api/devices/{mac} borra la fila del registro y 404 si no
// existe. El handler requiere admin (el test llama al handler directamente
// con PathValue, como el resto de tests de handlers con PathValue).
func TestDeviceDelete(t *testing.T) {
	s := panelTestServer(t)
	if _, err := s.db.Exec("INSERT INTO device_seen (mac, first_seen, last_seen, name) VALUES ('AA:BB:CC:DD:EE:FF', 1, 2, 'watch')", ); err != nil {
		t.Fatalf("seed: %v", err)
	}

	r := httptest.NewRequest(http.MethodDelete, "/api/devices/aa-bb-cc-dd-ee-ff", nil)
	r.SetPathValue("mac", "aa-bb-cc-dd-ee-ff")
	rec := httptest.NewRecorder()
	s.handleDeviceDelete(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM device_seen WHERE mac = 'AA:BB:CC:DD:EE:FF'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rows = %d, want 0 (borrado)", n)
	}

	// MAC desconocida: 404.
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodDelete, "/api/devices/11-22-33-44-55-66", nil)
	r.SetPathValue("mac", "11-22-33-44-55-66")
	s.handleDeviceDelete(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// #1151: link con validaciones (self 400, ok 200, ya enlazada 409, target
// alias 409) y unlink (204 / 404).
func TestDeviceLinkPutDelete(t *testing.T) {
	s := panelTestServer(t)
	do := func(method, mac, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/devices/"+mac+"/link", strings.NewReader(body))
		r.SetPathValue("mac", mac)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		if method == http.MethodPut {
			s.handleDeviceLinkPut(rec, r)
		} else {
			s.handleDeviceLinkDelete(rec, r)
		}
		return rec
	}

	// self-link: 400
	if rec := do(http.MethodPut, "AA:AA:AA:00:00:01", `{"target":"AA:AA:AA:00:00:01"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("self link = %d, want 400", rec.Code)
	}
	// ok: 200
	if rec := do(http.MethodPut, "AA:AA:AA:00:00:02", `{"target":"AA:AA:AA:00:00:01"}`); rec.Code != http.StatusOK {
		t.Fatalf("link = %d, want 200", rec.Code)
	}
	// ya enlazada: 409
	if rec := do(http.MethodPut, "AA:AA:AA:00:00:02", `{"target":"AA:AA:AA:00:00:09"}`); rec.Code != http.StatusConflict {
		t.Fatalf("relink = %d, want 409", rec.Code)
	}
	// target es a su vez alias: 409
	if rec := do(http.MethodPut, "AA:AA:AA:00:00:03", `{"target":"AA:AA:AA:00:00:02"}`); rec.Code != http.StatusConflict {
		t.Fatalf("target alias = %d, want 409", rec.Code)
	}
	// unlink: 204 y 404
	if rec := do(http.MethodDelete, "AA:AA:AA:00:00:02", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("unlink = %d, want 204", rec.Code)
	}
	if rec := do(http.MethodDelete, "AA:AA:AA:00:00:02", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unlink de nuevo = %d, want 404", rec.Code)
	}
}
