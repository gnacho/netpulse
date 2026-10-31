package httpapi_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// TestPasswordChangeWithOnboxEnvStillSucceeds (#1701): tras un cambio de
// contraseña correcto el servidor intenta borrar auth_pass de UCI (bootstrap
// on-box). En un entorno sin el binario uci (test, CT, Docker) el intento es
// best-effort: ambas rutas (propia y admin) deben devolver 204 igualmente.
func TestPasswordChangeWithOnboxEnvStillSucceeds(t *testing.T) {
	t.Setenv("NETPULSE_ONBOX", "1")
	ts := makeUpgradeTestServer(t)

	status, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	if status != 204 {
		t.Fatalf("login: %d", status)
	}

	// Ruta admin: crear usuario y cambiar SU contraseña.
	req, _ := http.NewRequest("POST", ts.URL+"/api/users", strings.NewReader(`{"username":"ux1701","password":"secreto123456"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/users: %v", err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/users: %d, want 201", res.StatusCode)
	}
	created := readJSON(t, res)
	user, _ := created["user"].(map[string]any)
	if user == nil {
		t.Fatal("la respuesta de POST /api/users no lleva user")
	}
	userID, _ := user["id"].(float64)
	targetURL := ts.URL + "/api/users/" + strconv.FormatFloat(userID, 'f', -1, 64) + "/password"
	putBody := `{"password":"nueva-1701-ok"}`
	req, _ = http.NewRequest("PUT", targetURL, strings.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+cookie)
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/users/{id}/password: %v", err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT /api/users/{id}/password: %d, want 204", res2.StatusCode)
	}

	// Ruta propia: PUT /api/auth/password.
	req, _ = http.NewRequest("PUT", ts.URL+"/api/auth/password", strings.NewReader(`{"current":"test123456","password":"propia-1701-ok"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+cookie)
	res3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/auth/password: %v", err)
	}
	defer res3.Body.Close()
	if res3.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT /api/auth/password: %d, want 204", res3.StatusCode)
	}
}
