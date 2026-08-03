package httpapi_test

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/httpapi"
)

// SPEC-65 D65-6: GET /api/system/info — autenticado (cualquier usuario),
// datos reales del proceso con la forma exacta del contrato.
func TestSystemInfo(t *testing.T) {
	srv := makeTestServer(t)

	// Sin sesión → 401
	res := get(t, srv.URL, "/api/system/info", "")
	if res.StatusCode != 401 {
		res.Body.Close()
		t.Fatalf("sin cookie: got %d want 401", res.StatusCode)
	}
	res.Body.Close()

	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test1234")
	res = get(t, srv.URL, "/api/system/info", cookie)
	if res.StatusCode != 200 {
		t.Fatalf("con cookie: got %d want 200", res.StatusCode)
	}
	body := readJSON(t, res)
	// Forma exacta: las 11 claves del contrato, ni una más
	want := map[string]bool{
		"version": true, "goVersion": true, "os": true, "arch": true, "distro": true,
		"kernel": true, "cpuModel": true, "cpuCores": true, "memTotalMb": true,
		"uptimeS": true, "demo": true,
	}
	if len(body) != len(want) {
		t.Fatalf("claves: got %v", body)
	}
	for k := range want {
		if _, ok := body[k]; !ok {
			t.Fatalf("falta clave %q en %v", k, body)
		}
	}
	if body["version"] != httpapi.Version {
		t.Fatalf("version=%v, want %s (misma fuente que update.go)", body["version"], httpapi.Version)
	}
	if goVer, ok := body["goVersion"].(string); !ok || len(goVer) < 3 || goVer[:2] != "go" {
		t.Fatalf("goVersion: %v", body["goVersion"])
	}
	if cores, ok := body["cpuCores"].(float64); !ok || cores < 1 {
		t.Fatalf("cpuCores: %v", body["cpuCores"])
	}
	if up, ok := body["uptimeS"].(float64); !ok || up < 0 {
		t.Fatalf("uptimeS: %v", body["uptimeS"])
	}
	if body["demo"] != true {
		t.Fatalf("demo: %v (el servidor de test es DEMO_MODE=1)", body["demo"])
	}
}
