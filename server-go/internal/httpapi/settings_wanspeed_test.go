// settings_wanspeed_test.go — contrato de GET/PUT /api/settings/wanspeed
// (issue #151): velocidad WAN contratada declarada por el admin.
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// wanSpeedRequest hace una petición con body JSON + cookie de sesión.
func wanSpeedRequest(t *testing.T, method, base, path, cookie, body string) (*http.Response, map[string]any) {
	t.Helper()
	var req *http.Request
	if body != "" {
		req, _ = http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, _ = http.NewRequest(method, base+path, nil)
	}
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	var parsed map[string]any
	raw, _ := io.ReadAll(res.Body)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &parsed)
	}
	return res, parsed
}

// TestWanSpeedDefaultEmpty: sin configurar, GET devuelve null en ambos campos.
func TestWanSpeedDefaultEmpty(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if cookie == "" {
		t.Fatal("login no devolvió cookie")
	}
	res, body := wanSpeedRequest(t, "GET", srv.URL, "/api/settings/wanspeed", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET: status %d, esperado 200", res.StatusCode)
	}
	if body["downMbps"] != nil {
		t.Errorf("downMbps=%v, esperado null", body["downMbps"])
	}
	if body["upMbps"] != nil {
		t.Errorf("upMbps=%v, esperado null", body["upMbps"])
	}
}

// TestWanSpeedRoundtrip: PUT válido guarda y GET lo devuelve.
func TestWanSpeedRoundtrip(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if cookie == "" {
		t.Fatal("login no devolvió cookie")
	}
	res, body := wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/wanspeed", cookie, `{"downMbps":600,"upMbps":300}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT: status %d, esperado 200 (%v)", res.StatusCode, body)
	}
	if body["downMbps"] != float64(600) || body["upMbps"] != float64(300) {
		t.Errorf("PUT echo incorrecto: %v", body)
	}
	res, body = wanSpeedRequest(t, "GET", srv.URL, "/api/settings/wanspeed", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET tras PUT: status %d, esperado 200", res.StatusCode)
	}
	if body["downMbps"] != float64(600) || body["upMbps"] != float64(300) {
		t.Errorf("GET tras PUT: %v, esperado downMbps 600 / upMbps 300", body)
	}
}

// TestWanSpeedInvalidInput: PUT inválido → 400 sin guardar nada.
func TestWanSpeedInvalidInput(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if cookie == "" {
		t.Fatal("login no devolvió cookie")
	}
	cases := []string{
		`{"downMbps":0,"upMbps":300}`,      // down cero
		`{"downMbps":-5,"upMbps":300}`,     // down negativo
		`{"downMbps":600,"upMbps":0}`,      // up cero
		`{"downMbps":100001,"upMbps":300}`, // down sobre el techo
		`{"downMbps":600}`,                 // up ausente
		`{"upMbps":300}`,                   // down ausente
		`not-json`,                         // body roto
	}
	for _, body := range cases {
		res, _ := wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/wanspeed", cookie, body)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("PUT %s: status %d, esperado 400", body, res.StatusCode)
		}
	}
	// Nada se guardó
	res, got := wanSpeedRequest(t, "GET", srv.URL, "/api/settings/wanspeed", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET: status %d", res.StatusCode)
	}
	if got["downMbps"] != nil || got["upMbps"] != nil {
		t.Errorf("el kv no debería haber cambiado tras PUTs inválidos: %v", got)
	}
}

// TestWanSpeedRequiresAuth: sin sesión → 401.
func TestWanSpeedRequiresAuth(t *testing.T) {
	srv := makeTestServer(t)
	res, _ := wanSpeedRequest(t, "GET", srv.URL, "/api/settings/wanspeed", "", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET sin auth: status %d, esperado 401", res.StatusCode)
	}
	res, _ = wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/wanspeed", "", `{"downMbps":600,"upMbps":300}`)
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("PUT sin auth: status %d, esperado 401", res.StatusCode)
	}
}
