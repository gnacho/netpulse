// ntfy_settings_test.go - contrato del canal ntfy (#766): GET sin token,
// PUT con validación y conservación, test que publica contra un server falso.
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/httpapi"
)

func TestNtfySettingsContract(t *testing.T) {
	// Server ntfy falso: registra publicaciones.
	var published int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mi-topic" {
			w.WriteHeader(404)
			return
		}
		published++
		w.WriteHeader(200)
	}))
	defer fake.Close()

	ts, _ := makeOwutTestServer(t, nil)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	// GET inicial: defaults, sin token.
	res := getJSONPath(t, ts.URL, "/api/settings/ntfy", cookie)
	body := readJSON(t, res)
	if body["server"] != "https://ntfy.sh" || body["tokenSet"] != false {
		t.Fatalf("GET inicial: %v", body)
	}
	if _, present := body["token"]; present {
		t.Fatalf("el token jamas sale: %v", body)
	}

	// PUT valido apuntando al server falso.
	res = putJSONPath(t, ts.URL, "/api/settings/ntfy",
		`{"enabled":true,"topic":"mi-topic","server":"`+fake.URL+`","token":"tk1"}`, cookie)
	if res.StatusCode != 200 {
		t.Fatalf("PUT: %d %v", res.StatusCode, readJSON(t, res))
	}

	// GET: topic/server visibles, tokenSet true, sin token.
	res = getJSONPath(t, ts.URL, "/api/settings/ntfy", cookie)
	body = readJSON(t, res)
	if body["topic"] != "mi-topic" || body["tokenSet"] != true || body["enabled"] != true {
		t.Fatalf("GET tras PUT: %v", body)
	}

	// Test: publica contra el falso.
	res = postJSON(t, ts.URL, "/api/settings/ntfy/test", "{}", cookie)
	if res.StatusCode != 200 {
		t.Fatalf("test: %d %v", res.StatusCode, readJSON(t, res))
	}
	if published != 1 {
		t.Fatalf("el falso debia recibir 1 publicacion: %d", published)
	}

	// PUT invalido (topic con espacios): 400.
	res = putJSONPath(t, ts.URL, "/api/settings/ntfy", `{"enabled":true,"topic":"tema con espacios"}`, cookie)
	if res.StatusCode != 400 {
		t.Fatalf("topic invalido: %d", res.StatusCode)
	}

	// Clear: limpia el canal.
	res = putJSONPath(t, ts.URL, "/api/settings/ntfy", `{"clear":true}`, cookie)
	if res.StatusCode != 200 {
		t.Fatalf("clear: %d", res.StatusCode)
	}
	res = getJSONPath(t, ts.URL, "/api/settings/ntfy", cookie)
	body = readJSON(t, res)
	if body["topic"] != "" || body["tokenSet"] != false || body["enabled"] != false {
		t.Fatalf("tras clear: %v", body)
	}
}

// TestNtfySettingsTestFailsVisibly: un server caido devuelve el error con
// mensaje (boton de prueba honesto).
func TestNtfySettingsTestFailsVisibly(t *testing.T) {
	ts, _ := makeOwutTestServer(t, nil)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	_ = putJSONPath(t, ts.URL, "/api/settings/ntfy",
		`{"enabled":true,"topic":"x","server":"http://127.0.0.1:1"}`, cookie)
	res := postJSON(t, ts.URL, "/api/settings/ntfy/test", "{}", cookie)
	if res.StatusCode == 200 {
		t.Fatalf("server caido debe fallar el test")
	}
	body := readJSON(t, res)
	if msg, _ := body["message"].(string); msg == "" {
		t.Fatalf("el fallo debe llevar mensaje: %v", body)
	}
}

// TestNtfySettingsTestRequiresEnabled: el endpoint de test no publica con el
// canal desactivado (misma decisión que el worker).
func TestNtfySettingsTestRequiresEnabled(t *testing.T) {
	ts, _ := makeOwutTestServer(t, nil)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := putJSONPath(t, ts.URL, "/api/settings/ntfy",
		`{"enabled":false,"topic":"mi-topic"}`, cookie)
	if res.StatusCode != 200 {
		t.Fatalf("PUT desactivado: %d", res.StatusCode)
	}
	res = postJSON(t, ts.URL, "/api/settings/ntfy/test", "{}", cookie)
	if res.StatusCode == 200 {
		t.Fatalf("test con canal desactivado debe fallar")
	}
	body := readJSON(t, res)
	if msg, _ := body["message"].(string); msg == "" {
		t.Fatalf("el fallo debe llevar mensaje: %v", body)
	}
}

var _ = httpapi.Version
