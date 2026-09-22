// settings_services_test.go — GET/PUT /api/settings/services (#813): el toggle
// de Servicios controla la monitorización de AdGuard (default: activo).
package httpapi_test

import (
	"net/http"
	"testing"
)

func TestServicesAdguardToggle(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if cookie == "" {
		t.Fatal("login no devolvió cookie")
	}

	// Default: activo (clave kv ausente en una instalación nueva).
	res, body := wanSpeedRequest(t, "GET", srv.URL, "/api/settings/services", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET: status %d, esperado 200", res.StatusCode)
	}
	if body["adguard"] != true {
		t.Fatalf("adguard default: %v, esperado true", body["adguard"])
	}

	// Desactivar.
	res, body = wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/services", cookie, `{"adguard":false}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT false: status %d (%v)", res.StatusCode, body)
	}
	if body["adguard"] != false {
		t.Fatalf("PUT false echo: %v", body)
	}
	_, body = wanSpeedRequest(t, "GET", srv.URL, "/api/settings/services", cookie, "")
	if body["adguard"] != false {
		t.Fatalf("GET tras desactivar: %v, esperado false", body["adguard"])
	}

	// Reactivar.
	res, _ = wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/services", cookie, `{"adguard":true}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT true: status %d", res.StatusCode)
	}
	_, body = wanSpeedRequest(t, "GET", srv.URL, "/api/settings/services", cookie, "")
	if body["adguard"] != true {
		t.Fatalf("GET tras reactivar: %v, esperado true", body["adguard"])
	}

	// Sin el campo adguard → 400.
	res, _ = wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/services", cookie, `{}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT sin adguard: status %d, esperado 400", res.StatusCode)
	}

	// Sin sesión → 401.
	res, _ = wanSpeedRequest(t, "GET", srv.URL, "/api/settings/services", "", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET sin auth: status %d, esperado 401", res.StatusCode)
	}
}
