package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// demoReadOnly (issue #118): en modo demo las mutaciones fuera de la allowlist
// se rechazan con 409 demo_read_only; la allowlist (login, demo/enable,
// refresh, push, users/me) sigue funcionando.
func TestDemoReadOnly(t *testing.T) {
	ts := makeDemoTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test1234")
	if cookie == "" {
		t.Fatal("login no devolvió cookie")
	}

	do := func(method, path, body string) (int, string) {
		t.Helper()
		var req *http.Request
		if body != "" {
			req, _ = http.NewRequest(method, ts.URL+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req, _ = http.NewRequest(method, ts.URL+path, nil)
		}
		req.Header.Set("Cookie", "session="+cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer res.Body.Close()
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, res.Body)
		return res.StatusCode, buf.String()
	}

	// Mutación bloqueada: crear un override de topología en demo
	status, bodyStr := do("POST", "/api/topology/overrides", `{"mac":"aa:bb:cc:dd:ee:ff","kind":"hypervisor"}`)
	if status != http.StatusConflict {
		t.Errorf("POST override en demo: status %d, esperado 409", status)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(bodyStr), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "demo_read_only" {
		t.Errorf("error=%q, esperado demo_read_only", body.Error)
	}

	// Mutación bloqueada: añadir router
	if status, _ := do("POST", "/api/config/routers", `{"host":"10.0.0.9","name":"r9"}`); status != http.StatusConflict {
		t.Errorf("POST router en demo: status %d, esperado 409", status)
	}

	// Mutación bloqueada: plan de orquestación
	if status, _ := do("POST", "/api/plans", `{"router_id":"r1","resource":"x","desired":"y"}`); status != http.StatusConflict {
		t.Errorf("POST plan en demo: status %d, esperado 409", status)
	}

	// Allowlist: refresh (no escribe) debe funcionar
	if status, _ := do("POST", "/api/refresh", ""); status != http.StatusNoContent && status != http.StatusAccepted && status != http.StatusTooManyRequests {
		t.Errorf("POST refresh en demo: status %d, no esperado", status)
	}

	// Allowlist: preferencia de idioma (users/me) debe funcionar
	if status, _ := do("PUT", "/api/users/me/language", `{"language":"es"}`); status != http.StatusOK && status != http.StatusNoContent {
		t.Errorf("PUT language en demo: status %d, esperado 2xx", status)
	}

	// GET sigue funcionando (solo lectura)
	if status, _ := do("GET", "/api/topology/overrides", ""); status != http.StatusOK {
		t.Errorf("GET overrides en demo: status %d, esperado 200", status)
	}
}
