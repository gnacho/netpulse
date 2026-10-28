package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// mintToken crea un API token con la cookie de sesión dada y devuelve el raw.
func mintToken(t *testing.T, base, cookie, scope string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": "scope-test", "scope": scope})
	req, _ := http.NewRequest("POST", base+"/api/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("crear token %s: %v", scope, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("crear token %s: got %d want 201", scope, res.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("crear token %s: %v", scope, err)
	}
	if out.Token == "" {
		t.Fatalf("crear token %s: raw vacío", scope)
	}
	return out.Token
}

// tokenRequest hace una petición autenticada con Bearer.
func tokenRequest(t *testing.T, method, base, path, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

// sessionRequest hace una petición autenticada con cookie de sesión.
func sessionRequest(t *testing.T, method, base, path, cookie, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

func requireStatus(t *testing.T, res *http.Response, want int, label string) {
	t.Helper()
	defer res.Body.Close()
	if res.StatusCode != want {
		t.Fatalf("%s: got %d want %d", label, res.StatusCode, want)
	}
}

func requireScopeError(t *testing.T, res *http.Response, label string) {
	t.Helper()
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("%s: got %d want 403", label, res.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	if body.Error != "insufficient_scope" {
		t.Fatalf("%s: error %q want insufficient_scope", label, body.Error)
	}
}

func TestReadTokenIsReadOnly(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	token := mintToken(t, ts.URL, cookie, "read")

	requireStatus(t, tokenRequest(t, "GET", ts.URL, "/api/overview", token, ""), 200, "GET overview")

	for _, rt := range []struct{ method, path, body string }{
		{"PUT", "/api/settings/backup", `{}`},
		{"POST", "/api/agents", `{"slug":"rt-scope"}`},
		{"DELETE", "/api/devices/aa:bb:cc:dd:ee:ff", ""},
	} {
		requireScopeError(t, tokenRequest(t, rt.method, ts.URL, rt.path, token, rt.body),
			rt.method+" "+rt.path)
	}

	// Las lecturas admin tampoco están disponibles para un token read.
	for _, path := range []string{"/api/settings/backup", "/api/backup/download"} {
		requireScopeError(t, tokenRequest(t, "GET", ts.URL, path, token, ""), "GET "+path)
	}
}

func TestWriteTokenCannotReachAdminRoutes(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	token := mintToken(t, ts.URL, cookie, "write")

	// Una mutación no admin sí está permitida: es el contrato de write.
	requireStatus(t, tokenRequest(t, "PUT", ts.URL, "/api/users/me/language", token, `{"language":"es"}`), 204,
		"PUT users/me/language")

	for _, rt := range []struct{ method, path, body string }{
		{"PUT", "/api/settings/backup", `{}`},
		{"GET", "/api/settings/backup", ""},
		{"POST", "/api/agents", `{"slug":"rt-scope"}`},
	} {
		requireScopeError(t, tokenRequest(t, rt.method, ts.URL, rt.path, token, rt.body),
			rt.method+" "+rt.path)
	}
}

func TestAdminTokenKeepsAdminAccess(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	token := mintToken(t, ts.URL, cookie, "admin")

	requireStatus(t, tokenRequest(t, "GET", ts.URL, "/api/settings/backup", token, ""), 200, "GET settings/backup")
	requireStatus(t, tokenRequest(t, "PUT", ts.URL, "/api/settings/backup", token, `{}`), 200, "PUT settings/backup")
}

func TestWriteTokenCannotCreateAdminToken(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	token := mintToken(t, ts.URL, cookie, "write")

	requireScopeError(t,
		tokenRequest(t, "POST", ts.URL, "/api/tokens", token, `{"name":"admin-esc","scope":"admin"}`),
		"write crea token admin")

	requireStatus(t,
		tokenRequest(t, "POST", ts.URL, "/api/tokens", token, `{"name":"read-child","scope":"read"}`),
		201, "write crea token read")
}

func TestReadTokenCannotCreateTokens(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	token := mintToken(t, ts.URL, cookie, "read")

	requireScopeError(t,
		tokenRequest(t, "POST", ts.URL, "/api/tokens", token, `{"name":"nope","scope":"read"}`),
		"read crea token")
}

func TestSessionWithoutAdminScopeStillCannotCreateAdminToken(t *testing.T) {
	ts := makeTestServer(t)
	_, adminCookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	viewerCookie := createUserAndLogin(t, ts.URL, adminCookie, "viewer2", "clave12345", "user")

	requireStatus(t,
		sessionRequest(t, "POST", ts.URL, "/api/tokens", viewerCookie, `{"name":"admin-esc","scope":"admin"}`),
		403, "viewer crea token admin")
}
