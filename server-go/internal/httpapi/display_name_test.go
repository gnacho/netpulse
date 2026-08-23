package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// SPEC-65 D65-5: display name por usuario — roundtrip completo.
func TestDisplayNameRoundtrip(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// Al inicio: displayName "" (no puesto)
	res := get(t, srv.URL, "/api/auth/me", cookie)
	body := readJSON(t, res)
	if body["displayName"] != "" {
		t.Fatalf("displayName inicial: %v", body)
	}

	put := func(payload string) (int, map[string]any) {
		req, _ := http.NewRequest("PUT", srv.URL+"/api/users/me/display-name", strings.NewReader(payload))
		req.Header.Set("Cookie", "session="+cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT display-name: %v", err)
		}
		if res.StatusCode == 204 {
			res.Body.Close()
			return res.StatusCode, nil
		}
		return res.StatusCode, readJSON(t, res)
	}

	// Guardar (con espacios alrededor: se hace trim) → 204 y aparece en /me
	st, _ := put(`{"displayName":"  Nacho 🚀  "}`)
	if st != 204 {
		t.Fatalf("PUT display-name: got %d want 204", st)
	}
	body = readJSON(t, get(t, srv.URL, "/api/auth/me", cookie))
	if body["displayName"] != "Nacho 🚀" {
		t.Fatalf("displayName tras guardar: %v", body)
	}
	// Persiste en DB (lectura directa, misma fila que sirve /me)
	var dn string
	if err := srv.db.QueryRow("SELECT display_name FROM users WHERE username = 'admin'").Scan(&dn); err != nil || dn != "Nacho 🚀" {
		t.Fatalf("display_name en DB: %q err=%v", dn, err)
	}

	// 40 runes exactos → 204; 41 runes → 400
	st, _ = put(`{"displayName":"` + strings.Repeat("á", 40) + `"}`)
	if st != 204 {
		t.Fatalf("40 runes: got %d want 204", st)
	}
	st, body = put(`{"displayName":"` + strings.Repeat("a", 41) + `"}`)
	if st != 400 || body["error"] != "invalid_input" {
		t.Fatalf("41 runes: got %d %v want 400", st, body)
	}

	// HTML → 400
	st, body = put(`{"displayName":"<b>x</b>"}`)
	if st != 400 || body["error"] != "invalid_input" {
		t.Fatalf("con <>: got %d %v want 400", st, body)
	}
	st, _ = put(`{"displayName":"a>b"}`)
	if st != 400 {
		t.Fatalf("con >: got %d want 400", st)
	}

	// Body inválido → 400
	st, _ = put(`{"displayName":42}`)
	if st != 400 {
		t.Fatalf("tipo inválido: got %d want 400", st)
	}
	st, _ = put(`{}`)
	if st != 400 {
		t.Fatalf("sin displayName: got %d want 400", st)
	}

	// Vacío permitido (= volver al username) → 204 y displayName ""
	st, _ = put(`{"displayName":"   "}`)
	if st != 204 {
		t.Fatalf("vacío: got %d want 204", st)
	}
	body = readJSON(t, get(t, srv.URL, "/api/auth/me", cookie))
	if body["displayName"] != "" {
		t.Fatalf("displayName tras vaciar: %v", body)
	}
}

// GET /api/users incluye displayName por usuario (admin).
func TestListUsersIncluyeDisplayName(t *testing.T) {
	srv := makeTestServer(t)
	_, adminCookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// Alta de una usuaria no-admin y display name propio (fuera del gate admin)
	req, _ := http.NewRequest("POST", srv.URL+"/api/users", strings.NewReader(`{"username":"ana","password":"clave-ana-1","role":"user"}`))
	req.Header.Set("Cookie", "session="+adminCookie)
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatalf("crear ana: got %d want 201", res.StatusCode)
	}
	_, anaCookie, _ := loginCookie(t, srv.URL, "ana", "clave-ana-1")
	req, _ = http.NewRequest("PUT", srv.URL+"/api/users/me/display-name", strings.NewReader(`{"displayName":"Ana"}`))
	req.Header.Set("Cookie", "session="+anaCookie)
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 204 {
		t.Fatalf("display-name no-admin: got %d want 204", res.StatusCode)
	}

	res = get(t, srv.URL, "/api/users", adminCookie)
	body := readJSON(t, res)
	users, ok := body["users"].([]any)
	if !ok {
		t.Fatalf("users: %v", body)
	}
	names := map[string]any{}
	for _, u := range users {
		m := u.(map[string]any)
		names[fmt.Sprint(m["username"])] = m["displayName"]
	}
	if names["ana"] != "Ana" {
		t.Fatalf("displayName de ana en /api/users: %v", names["ana"])
	}
	if names["admin"] != "" {
		t.Fatalf("displayName de admin debería ser \"\": %v", names["admin"])
	}
}
