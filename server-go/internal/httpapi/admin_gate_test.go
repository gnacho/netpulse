// admin_gate_test.go — auditoría v2.4.0 §2 (issue #7): las rutas que mutan
// la monitorización (crear/revocar agentes, rearme SSH, añadir/borrar
// routers) o exponen credenciales/escaneo (sshkey, discover) exigen rol
// admin; un usuario con rol 'user' recibe 403, y las lecturas equivalentes
// siguen disponibles para cualquier sesión.
package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// createUserAndLogin: alta un usuario con el rol dado vía API (como admin) y
// devuelve su cookie de sesión.
func createUserAndLogin(t *testing.T, base, adminCookie, username, password, role string) string {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/api/users",
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`","role":"`+role+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+adminCookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("alta %s: %v", username, err)
	}
	res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatalf("alta %s: got %d want 201", username, res.StatusCode)
	}
	status, cookie, _ := loginCookie(t, base, username, password)
	if status != 204 || cookie == "" {
		t.Fatalf("login %s: got %d", username, status)
	}
	return cookie
}

// do: petición con cookie de sesión.
func do(t *testing.T, method, base, path, cookie string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, base+path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	res.Body.Close()
	return res
}

func TestRutasAdminDevuelven403ParaRolUser(t *testing.T) {
	ts := makeTestServer(t)
	_, adminCookie, _ := loginCookie(t, ts.URL, "admin", "test1234")
	userCookie := createUserAndLogin(t, ts.URL, adminCookie, "viewer", "clave1234", "user")

	adminOnly := []struct{ method, path string }{
		{"POST", "/api/agents"},
		{"DELETE", "/api/agents/rt2"},
		{"POST", "/api/agents/rt2/rearm"},
		{"POST", "/api/config/routers"},
		{"DELETE", "/api/config/routers/rt2"},
		{"GET", "/api/config/sshkey"},
		{"GET", "/api/config/discover"},
	}
	for _, rt := range adminOnly {
		if got := do(t, rt.method, ts.URL, rt.path, userCookie).StatusCode; got != 403 {
			t.Errorf("%s %s como user: got %d want 403", rt.method, rt.path, got)
		}
	}

	// Las lecturas equivalentes siguen abiertas a cualquier sesión.
	readable := []string{"/api/agents", "/api/config/routers", "/api/overview"}
	for _, p := range readable {
		if got := do(t, "GET", ts.URL, p, userCookie).StatusCode; got != 200 {
			t.Errorf("GET %s como user: got %d want 200", p, got)
		}
	}

	// Y el admin sí pasa el gate (el 201 de POST /api/agents confirma que
	// RequireAdmin deja llegar al handler; el slug no necesita router).
	req, _ := http.NewRequest("POST", ts.URL+"/api/agents", strings.NewReader(`{"slug":"rt-gate"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+adminCookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/agents admin: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 201 {
		t.Errorf("POST /api/agents como admin: got %d want 201", res.StatusCode)
	}
}
