// settings_integrations_test.go - GET/PUT /api/settings/integrations (#968):
// toggles server-side de ntfy/telegram/proxmox (ausente = activo) y mqtt
// (reutiliza la clave existente mqtt.enabled). El PUT es partial-safe: solo
// se escriben las claves presentes en el body.
package httpapi_test

import (
	"net/http"
	"testing"
)

func TestIntegrationsDefaultsOn(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if cookie == "" {
		t.Fatal("login no devolvió cookie")
	}

	res, body := wanSpeedRequest(t, "GET", srv.URL, "/api/settings/integrations", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET: status %d, esperado 200", res.StatusCode)
	}
	for _, k := range []string{"ntfy", "telegram", "proxmox"} {
		if body[k] != true {
			t.Fatalf("%s default: %v, esperado true (ausente = activo)", k, body[k])
		}
	}
	// MQTT por defecto apagado (la clave mqtt.enabled no existe aún).
	if body["mqtt"] != false {
		t.Fatalf("mqtt default: %v, esperado false", body["mqtt"])
	}

	// Sin sesión → 401.
	res, _ = wanSpeedRequest(t, "GET", srv.URL, "/api/settings/integrations", "", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET sin auth: status %d, esperado 401", res.StatusCode)
	}
}

func TestIntegrationsPutIsPartialSafe(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if cookie == "" {
		t.Fatal("login no devolvió cookie")
	}

	// Apagar ntfy no toca telegram ni proxmox.
	res, body := wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/integrations", cookie, `{"ntfy":false}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT ntfy=false: status %d (%v)", res.StatusCode, body)
	}
	if body["ntfy"] != false || body["telegram"] != true || body["proxmox"] != true {
		t.Fatalf("echo tras PUT parcial: %v", body)
	}

	// Apagar telegram tampoco reactiva ntfy.
	res, body = wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/integrations", cookie, `{"telegram":false}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT telegram=false: status %d (%v)", res.StatusCode, body)
	}
	if body["ntfy"] != false || body["telegram"] != false || body["proxmox"] != true {
		t.Fatalf("echo tras segundo PUT parcial: %v", body)
	}

	// La vista persiste en kv (GET lo relee).
	_, body = wanSpeedRequest(t, "GET", srv.URL, "/api/settings/integrations", cookie, "")
	if body["ntfy"] != false || body["telegram"] != false || body["proxmox"] != true {
		t.Fatalf("GET tras PUTs: %v", body)
	}

	// Body vacío → 400.
	res, _ = wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/integrations", cookie, `{}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT vacío: status %d, esperado 400", res.StatusCode)
	}
}

// TestIntegrationsMirrorChannelEnabled (#1016/#1017): el toggle de la fila es
// la única fuente de verdad - el PUT espeja ntfy.enabled/telegram.enabled
// para que el path de envío y el integrationGate nunca desincronicen.
func TestIntegrationsMirrorChannelEnabled(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if cookie == "" {
		t.Fatal("login no devolvió cookie")
	}

	kvVal := func(key string) string {
		var v string
		if err := srv.db.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&v); err != nil {
			return ""
		}
		return v
	}

	res, _ := wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/integrations", cookie, `{"ntfy":false,"telegram":true}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT: status %d", res.StatusCode)
	}
	if v := kvVal("ntfy.enabled"); v != "false" {
		t.Fatalf("ntfy.enabled espejo: %q, esperado false", v)
	}
	if v := kvVal("telegram.enabled"); v != "true" {
		t.Fatalf("telegram.enabled espejo: %q, esperado true", v)
	}

	// Proxmox NO tiene clave de canal que espejar.
	res, _ = wanSpeedRequest(t, "PUT", srv.URL, "/api/settings/integrations", cookie, `{"proxmox":false}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT proxmox: status %d", res.StatusCode)
	}
	if v := kvVal("ntfy.enabled"); v != "false" {
		t.Fatalf("ntfy.enabled tras PUT de proxmox: %q, esperado false intacto", v)
	}
}
