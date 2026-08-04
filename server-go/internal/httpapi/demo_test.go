// demo_test.go — issue #4: POST /api/demo/enable (solo admin). El
// testServer arranca con DEMO_MODE=1 (httpapi_test.go), así que el
// endpoint responde 409 already_demo; la lógica de reescritura del .env
// (setDemoModeInEnv) se cubre a nivel de unidad sobre un fichero temporal.
package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestDemoEnable403ParaRolUser(t *testing.T) {
	ts := makeTestServer(t)
	_, adminCookie, _ := loginCookie(t, ts.URL, "admin", "test1234")
	userCookie := createUserAndLogin(t, ts.URL, adminCookie, "viewerdemo", "clave1234", "user")

	res := do(t, "POST", ts.URL, "/api/demo/enable", userCookie)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("demo/enable rol user: got %d want 403", res.StatusCode)
	}
}

func TestDemoEnable401SinSesion(t *testing.T) {
	ts := makeTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/demo/enable", strings.NewReader("{}"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sin sesión: got %d want 401", res.StatusCode)
	}
}

func TestDemoEnable409CuandoYaDemo(t *testing.T) {
	ts := makeTestServer(t) // DEMO_MODE=1
	_, adminCookie, _ := loginCookie(t, ts.URL, "admin", "test1234")
	res := do(t, "POST", ts.URL, "/api/demo/enable", adminCookie)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("ya en demo: got %d want 409", res.StatusCode)
	}
}
