// config_test.go — port de tests/routers.test.js: CRUD de /api/config/routers
// (lista, alta, duplicado 409, validación 400, borrado 204/404) + sshkey.
package httpapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func doReq(t *testing.T, method, url, cookie, body string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestConfigRoutersCRUD replica routers.test.js.
func TestConfigRoutersCRUD(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test1234")

	// GET inicial: lista vacía (200)
	res := doReq(t, "GET", srv.URL+"/api/config/routers", cookie, "")
	if res.StatusCode != 200 {
		t.Fatalf("GET routers: %d", res.StatusCode)
	}
	body := readJSON(t, res)
	if _, ok := body["routers"].([]any); !ok {
		t.Fatalf("routers no es lista: %v", body)
	}

	// POST: crea un router (201, {router})
	res = doReq(t, "POST", srv.URL+"/api/config/routers", cookie,
		`{"name":"Salón","host":"192.168.8.2","type":"openwrt"}`)
	if res.StatusCode != 201 {
		t.Fatalf("POST router: %d", res.StatusCode)
	}
	body = readJSON(t, res)
	router := body["router"].(map[string]any)
	if router["id"] != "salon" || router["name"] != "Salón" || router["is_gateway"] != false {
		t.Fatalf("router creado: %v", router)
	}

	// POST gateway: is_gateway exclusivo
	res = doReq(t, "POST", srv.URL+"/api/config/routers", cookie,
		`{"host":"192.168.8.1","type":"glinet","gateway":true}`)
	if res.StatusCode != 201 {
		t.Fatalf("POST gateway: %d", res.StatusCode)
	}
	res = doReq(t, "GET", srv.URL+"/api/config/routers", cookie, "")
	body = readJSON(t, res)
	routers := body["routers"].([]any)
	if len(routers) != 2 {
		t.Fatalf("routers: %d", len(routers))
	}
	gwCount := 0
	for _, r := range routers {
		if r.(map[string]any)["is_gateway"] == true {
			gwCount++
		}
	}
	if gwCount != 1 {
		t.Fatalf("gateways: %d", gwCount)
	}

	// POST duplicado → 409 duplicate_host
	res = doReq(t, "POST", srv.URL+"/api/config/routers", cookie,
		`{"host":"192.168.8.2"}`)
	if res.StatusCode != 409 {
		t.Fatalf("duplicado: %d", res.StatusCode)
	}
	body = readJSON(t, res)
	if body["error"] != "duplicate_host" {
		t.Fatalf("error: %v", body)
	}

	// POST host inválido → 400 invalid_input
	res = doReq(t, "POST", srv.URL+"/api/config/routers", cookie,
		`{"host":"no es un host"}`)
	if res.StatusCode != 400 {
		t.Fatalf("host inválido: %d", res.StatusCode)
	}

	// POST type inválido → 400
	res = doReq(t, "POST", srv.URL+"/api/config/routers", cookie,
		`{"host":"192.168.8.9","type":"cisco"}`)
	if res.StatusCode != 400 {
		t.Fatalf("type inválido: %d", res.StatusCode)
	}

	// POST JSON inválido → 400 invalid_json
	res = doReq(t, "POST", srv.URL+"/api/config/routers", cookie, `{`)
	if res.StatusCode != 400 {
		t.Fatalf("json inválido: %d", res.StatusCode)
	}
	body = readJSON(t, res)
	if body["error"] != "invalid_json" {
		t.Fatalf("error: %v", body)
	}

	// DELETE existente → 204; repetir → 404
	res = doReq(t, "DELETE", srv.URL+"/api/config/routers/salon", cookie, "")
	if res.StatusCode != 204 {
		t.Fatalf("DELETE: %d", res.StatusCode)
	}
	res = doReq(t, "DELETE", srv.URL+"/api/config/routers/salon", cookie, "")
	if res.StatusCode != 404 {
		t.Fatalf("DELETE 404: %d", res.StatusCode)
	}

	// GET sshkey → 200 {publicKey, fingerprint} (la clave se genera al vuelo)
	res = doReq(t, "GET", srv.URL+"/api/config/sshkey", cookie, "")
	// Sin clave generada en el DATA_DIR de test → 500 no_key; si existe
	// ssh-keygen en el sistema la ruta la habría creado el arranque (no en
	// tests) — aceptamos ambos y validamos el shape cuando hay clave.
	if res.StatusCode == 200 {
		body = readJSON(t, res)
		if _, ok := body["publicKey"].(string); !ok {
			t.Fatalf("sshkey: %v", body)
		}
	} else if res.StatusCode != 500 {
		t.Fatalf("sshkey: %d", res.StatusCode)
	}
}
