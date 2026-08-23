// settings_knownmacs_test.go — contrato de /api/settings/known-macs
// (issue #196): allowlist de dispositivos confiables que no alertan como
// "desconocido" y cuyo nombre se usa como alias.
package httpapi_test

import (
	"net/http"
	"testing"
)

// TestKnownMacsRoundtrip: GET vacío → PUT → GET lo devuelve → DELETE.
func TestKnownMacsRoundtrip(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if cookie == "" {
		t.Fatal("login no devolvió cookie")
	}

	res, body := wanSpeedRequest(t, "GET", srv.URL, "/api/settings/known-macs", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET: status %d, esperado 200", res.StatusCode)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 0 {
		t.Fatalf("GET inicial: %v, esperado items vacío", body)
	}

	res, body = wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/known-macs", cookie,
		`{"mac":"A4:7E:FA:65:0C:AA","name":"Withings","note":"báscula"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT: status %d, esperado 200 (%v)", res.StatusCode, body)
	}

	res, body = wanSpeedRequest(t, "GET", srv.URL, "/api/settings/known-macs", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET tras PUT: status %d", res.StatusCode)
	}
	items, ok = body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("GET tras PUT: %v, esperado 1 item", body)
	}
	it := items[0].(map[string]any)
	if it["mac"] != "A4:7E:FA:65:0C:AA" || it["name"] != "Withings" {
		t.Fatalf("item: %v", it)
	}

	res, _ = wanSpeedRequest(t, "DELETE", srv.URL, "/api/settings/known-macs/A4:7E:FA:65:0C:AA", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("DELETE: status %d, esperado 200", res.StatusCode)
	}
	res, body = wanSpeedRequest(t, "GET", srv.URL, "/api/settings/known-macs", cookie, "")
	items, _ = body["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("GET tras DELETE: %v, esperado vacío", body)
	}
}

// TestKnownMacsInvalidInput: MAC mal formada → 400; PUT sin admin → 401.
func TestKnownMacsInvalidInput(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	for _, body := range []string{
		`{"mac":"A4:7E:FA:65:0C","name":"x"}`,       // corta
		`{"mac":"A4:7E-FA:65:0C:AA","name":"x"}`,    // separador mal
		`{"mac":"G4:7E:FA:65:0C:AA","name":"x"}`,    // hex inválido
		`not-json`,                                  // body roto
	} {
		res, _ := wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/known-macs", cookie, body)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("PUT %s: status %d, esperado 400", body, res.StatusCode)
		}
	}

	// Sin sesión admin → 401 (PUT y DELETE)
	res, _ := wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/known-macs", "", `{"mac":"AA:BB:CC:DD:EE:FF","name":"x"}`)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("PUT sin auth: status %d, esperado 401", res.StatusCode)
	}
	res, _ = wanSpeedRequest(t, "DELETE", srv.URL, "/api/settings/known-macs/AA:BB:CC:DD:EE:FF", "", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("DELETE sin auth: status %d, esperado 401", res.StatusCode)
	}
}
