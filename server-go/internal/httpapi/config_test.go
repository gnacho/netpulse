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
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

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

// TestRotateSSHKey cubre POST /api/config/sshkey/rotate (#242):
//   - 403 sin admin
//   - 200 y la nueva pública difiere de la anterior (cuando había clave)
//   - la respuesta incluye publicKey + fingerprint
func TestRotateSSHKey(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// Sin admin → 403.
	res := doReq(t, "POST", srv.URL+"/api/config/sshkey/rotate", "", "")
	if res.StatusCode != 403 && res.StatusCode != 401 {
		t.Fatalf("rotate sin admin: %d (esperaba 403)", res.StatusCode)
	}

	// Clave previa (puede no existir en el DATA_DIR de test).
	prev := ""
	res = doReq(t, "GET", srv.URL+"/api/config/sshkey", cookie, "")
	if res.StatusCode == 200 {
		prev = readJSON(t, res)["publicKey"].(string)
	}

	res = doReq(t, "POST", srv.URL+"/api/config/sshkey/rotate", cookie, "")
	if res.StatusCode != 200 {
		t.Fatalf("rotate: %d", res.StatusCode)
	}
	body := readJSON(t, res)
	pub, ok := body["publicKey"].(string)
	if !ok || pub == "" {
		t.Fatalf("rotate: sin publicKey: %v", body)
	}
	if _, ok := body["fingerprint"].(string); !ok {
		t.Fatalf("rotate: sin fingerprint: %v", body)
	}
	if prev != "" && prev == pub {
		t.Fatalf("rotate: la clave no cambió (prev==new)")
	}
}

// TestConfigRoutersUpdate cubre PUT /api/config/routers/{id}:
//   - 404 si no existe
//   - editar name (200, campo cambiado)
//   - editar host inválido (400)
//   - editar host duplicado con otro router (409)
//   - promover a gateway (200, solo uno con el flag)
//   - marcar agent_only (200, flag cambiado)
//   - PUT no-admin → 401/403 (lo cubre admin_gate_test.go)
func TestConfigRoutersUpdate(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// Setup: dos routers (rt1 no-gateway, gw gateway).
	res := doReq(t, "POST", srv.URL+"/api/config/routers", cookie,
		`{"name":"rt1","host":"192.168.8.10","type":"openwrt"}`)
	if res.StatusCode != 201 {
		t.Fatalf("POST rt1: %d", res.StatusCode)
	}
	res = doReq(t, "POST", srv.URL+"/api/config/routers", cookie,
		`{"name":"gw","host":"192.168.8.1","type":"glinet","gateway":true}`)
	if res.StatusCode != 201 {
		t.Fatalf("POST gw: %d", res.StatusCode)
	}

	// PUT inexistente → 404
	res = doReq(t, "PUT", srv.URL+"/api/config/routers/no-existe", cookie,
		`{"name":"foo"}`)
	if res.StatusCode != 404 {
		t.Fatalf("PUT 404: %d", res.StatusCode)
	}

	// PUT name → 200, name cambiado
	res = doReq(t, "PUT", srv.URL+"/api/config/routers/rt1", cookie,
		`{"name":"Salón Editado","host":"192.168.8.10","type":"openwrt","gateway":false,"agent_only":false}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT name: %d", res.StatusCode)
	}
	body := readJSON(t, res)
	rt := body["router"].(map[string]any)
	if rt["name"] != "Salón Editado" {
		t.Errorf("name: %v", rt["name"])
	}

	// PUT host inválido → 400
	res = doReq(t, "PUT", srv.URL+"/api/config/routers/rt1", cookie,
		`{"host":"no es un host"}`)
	if res.StatusCode != 400 {
		t.Fatalf("PUT host inválido: %d", res.StatusCode)
	}

	// PUT host duplicado con gw → 409
	res = doReq(t, "PUT", srv.URL+"/api/config/routers/rt1", cookie,
		`{"host":"192.168.8.1"}`)
	if res.StatusCode != 409 {
		t.Fatalf("PUT host duplicado: %d", res.StatusCode)
	}

	// PUT promover rt1 a gateway → 200, ahora rt1 es el único gateway
	res = doReq(t, "PUT", srv.URL+"/api/config/routers/rt1", cookie,
		`{"host":"192.168.8.10","gateway":true}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT gateway: %d", res.StatusCode)
	}
	res = doReq(t, "GET", srv.URL+"/api/config/routers", cookie, "")
	body = readJSON(t, res)
	gwCount := 0
	rt1Gw := false
	for _, r := range body["routers"].([]any) {
		rm := r.(map[string]any)
		if rm["is_gateway"] == true {
			gwCount++
			if rm["id"] == "rt1" {
				rt1Gw = true
			}
		}
	}
	if gwCount != 1 || !rt1Gw {
		t.Fatalf("gateway mutado: gwCount=%d rt1Gw=%v", gwCount, rt1Gw)
	}

	// PUT agent_only=true → 200, agent_only cambiado
	res = doReq(t, "PUT", srv.URL+"/api/config/routers/rt1", cookie,
		`{"host":"192.168.8.10","agent_only":true}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT agent_only: %d", res.StatusCode)
	}
	body = readJSON(t, res)
	rt = body["router"].(map[string]any)
	if rt["agent_only"] != true {
		t.Errorf("agent_only: %v", rt["agent_only"])
	}
}

// TestConfigRoutersFirmwareTarget cubre firmware_target (issue #241):
// alta con target, PUT que lo actualiza y persistencia en la lista.
func TestConfigRoutersFirmwareTarget(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// POST con firmware_target → 201 y el router devuelto lo lleva.
	res := doReq(t, "POST", srv.URL+"/api/config/routers", cookie,
		`{"name":"rt1","host":"192.168.8.10","type":"openwrt","firmware_target":"25.12.5"}`)
	if res.StatusCode != 201 {
		t.Fatalf("POST router: %d", res.StatusCode)
	}
	body := readJSON(t, res)
	router := body["router"].(map[string]any)
	if router["firmware_target"] != "25.12.5" {
		t.Fatalf("firmware_target tras alta: %v", router)
	}

	// PUT cambia firmware_target → 200 y el campo cambia.
	res = doReq(t, "PUT", srv.URL+"/api/config/routers/rt1", cookie,
		`{"host":"192.168.8.10","firmware_target":"24.10.1"}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT firmware_target: %d", res.StatusCode)
	}
	body = readJSON(t, res)
	rt := body["router"].(map[string]any)
	if rt["firmware_target"] != "24.10.1" {
		t.Fatalf("firmware_target tras PUT: %v", rt)
	}

	// GET persiste el valor en la lista.
	res = doReq(t, "GET", srv.URL+"/api/config/routers", cookie, "")
	if res.StatusCode != 200 {
		t.Fatalf("GET routers: %d", res.StatusCode)
	}
	body = readJSON(t, res)
	found := ""
	for _, r := range body["routers"].([]any) {
		rm := r.(map[string]any)
		if rm["id"] == "rt1" {
			found, _ = rm["firmware_target"].(string)
		}
	}
	if found != "24.10.1" {
		t.Fatalf("firmware_target persistido=%q, esperaba 24.10.1", found)
	}
}

// TestAdGuardConfigPort cubre PUT /api/config/adguard (#420):
//   - guardar un puerto distinto de 3000 en modo standard
//   - GET devuelve el puerto guardado, no 3000
//   - el puerto explícito tiene prioridad sobre el que pueda venir en host:port
func TestAdGuardConfigPort(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	// PUT modo standard con puerto 80
	res := doReq(t, "PUT", srv.URL+"/api/config/adguard", cookie,
		`{"mode":"standard","host":"adguard.example.com","port":80,"user":"root","password":"secret"}`)
	if res.StatusCode != 204 {
		body := readJSON(t, res)
		t.Fatalf("PUT adguard: %d %v", res.StatusCode, body)
	}

	// GET debe devolver puerto 80
	res = doReq(t, "GET", srv.URL+"/api/config/adguard", cookie, "")
	if res.StatusCode != 200 {
		t.Fatalf("GET adguard: %d", res.StatusCode)
	}
	body := readJSON(t, res)
	if body["port"].(float64) != 80 {
		t.Fatalf("port esperado 80, got %v", body["port"])
	}

	// Puerto explícito tiene prioridad sobre host:port
	res = doReq(t, "PUT", srv.URL+"/api/config/adguard", cookie,
		`{"mode":"standard","host":"adguard.example.com:8080","port":9090,"user":"root","password":"secret"}`)
	if res.StatusCode != 204 {
		body := readJSON(t, res)
		t.Fatalf("PUT adguard con host:port: %d %v", res.StatusCode, body)
	}
	res = doReq(t, "GET", srv.URL+"/api/config/adguard", cookie, "")
	body = readJSON(t, res)
	if body["port"].(float64) != 9090 {
		t.Fatalf("port esperado 9090, got %v", body["port"])
	}

	// Puerto fuera de rango → 400
	res = doReq(t, "PUT", srv.URL+"/api/config/adguard", cookie,
		`{"mode":"standard","host":"adguard.example.com","port":70000,"user":"root","password":"secret"}`)
	if res.StatusCode != 400 {
		t.Fatalf("port fuera de rango: esperado 400, got %d", res.StatusCode)
	}

	// Modo glinet con host vacío debe seguir permitiéndose (no regresión)
	res = doReq(t, "PUT", srv.URL+"/api/config/adguard", cookie,
		`{"mode":"glinet","host":"","port":0,"user":"root","password":"secret"}`)
	if res.StatusCode != 204 {
		body := readJSON(t, res)
		t.Fatalf("PUT glinet host vacío: %d %v", res.StatusCode, body)
	}
	res = doReq(t, "GET", srv.URL+"/api/config/adguard", cookie, "")
	body = readJSON(t, res)
	if body["mode"] != "glinet" || body["host"] != "" || body["port"] != float64(0) {
		t.Fatalf("glinet host vacío no se guardó: %+v", body)
	}
}

func TestConfigRoutersSNMP(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	res := doReq(t, "POST", srv.URL+"/api/config/routers", cookie,
		`{"name":"sw1","host":"192.168.8.20","type":"managed-switch","snmp_enabled":true,"snmp_community":"public","snmp_port":161}`)
	if res.StatusCode != 201 {
		t.Fatalf("POST router: %d", res.StatusCode)
	}
	body := readJSON(t, res)
	router := body["router"].(map[string]any)
	if router["snmp_enabled"] != true {
		t.Fatalf("snmp_enabled tras alta: %v", router)
	}
	if router["snmp_community"] != "public" {
		t.Fatalf("snmp_community tras alta: %v", router)
	}
	if router["snmp_port"] != float64(161) {
		t.Fatalf("snmp_port tras alta: %v", router)
	}

	res = doReq(t, "PUT", srv.URL+"/api/config/routers/sw1", cookie,
		`{"host":"192.168.8.20","snmp_enabled":false,"snmp_community":"secret"}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT snmp: %d", res.StatusCode)
	}
	body = readJSON(t, res)
	rt := body["router"].(map[string]any)
	if rt["snmp_enabled"] != false {
		t.Fatalf("snmp_enabled tras PUT: %v", rt)
	}
	if rt["snmp_community"] != "secret" {
		t.Fatalf("snmp_community tras PUT: %v", rt)
	}

	res = doReq(t, "POST", srv.URL+"/api/config/routers", cookie,
		`{"name":"sw2","host":"192.168.8.21","type":"managed-switch","snmp_port":99999}`)
	if res.StatusCode != 400 {
		t.Fatalf("POST invalid snmp_port: %d; want 400", res.StatusCode)
	}
}

// TestProxmoxConfig (#561): PUT/GET /api/config/proxmox
//   - guardar url+token, GET no devuelve el secret (solo tokenSet)
//   - url vacía desactiva (limpia kv)
//   - url inválida → 400
func TestProxmoxConfig(t *testing.T) {
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")

	res := doReq(t, "PUT", srv.URL+"/api/config/proxmox", cookie,
		`{"url":"https://192.168.1.100:8006","tokenId":"root@pam!netpulse","secret":"uuid-1234"}`)
	if res.StatusCode != 204 {
		body := readJSON(t, res)
		t.Fatalf("PUT proxmox: %d %v", res.StatusCode, body)
	}

	res = doReq(t, "GET", srv.URL+"/api/config/proxmox", cookie, "")
	if res.StatusCode != 200 {
		t.Fatalf("GET proxmox: %d", res.StatusCode)
	}
	body := readJSON(t, res)
	if body["url"] != "https://192.168.1.100:8006" || body["tokenId"] != "root@pam!netpulse" {
		t.Fatalf("GET proxmox body: %v", body)
	}
	if _, present := body["secret"]; present {
		t.Fatalf("GET proxmox no debe devolver el secret: %v", body)
	}
	if body["tokenSet"] != true {
		t.Fatalf("tokenSet esperado true: %v", body)
	}

	// Actualizar solo el secret conserva url/tokenId.
	res = doReq(t, "PUT", srv.URL+"/api/config/proxmox", cookie,
		`{"secret":"uuid-9999"}`)
	if res.StatusCode != 204 {
		body := readJSON(t, res)
		t.Fatalf("PUT proxmox secret: %d %v", res.StatusCode, body)
	}
	res = doReq(t, "GET", srv.URL+"/api/config/proxmox", cookie, "")
	body = readJSON(t, res)
	if body["url"] != "https://192.168.1.100:8006" || body["tokenId"] != "root@pam!netpulse" {
		t.Fatalf("GET tras update secret: %v", body)
	}

	// URL inválida → 400
	res = doReq(t, "PUT", srv.URL+"/api/config/proxmox", cookie,
		`{"url":"no-es-url","tokenId":"a!b","secret":"c"}`)
	if res.StatusCode != 400 {
		t.Fatalf("url inválida: esperado 400, got %d", res.StatusCode)
	}

	// Desactivar: url vacía limpia todo.
	res = doReq(t, "PUT", srv.URL+"/api/config/proxmox", cookie, `{"url":""}`)
	if res.StatusCode != 204 {
		body := readJSON(t, res)
		t.Fatalf("PUT proxmox desactivar: %d %v", res.StatusCode, body)
	}
	res = doReq(t, "GET", srv.URL+"/api/config/proxmox", cookie, "")
	body = readJSON(t, res)
	if body["url"] != "" || body["tokenId"] != "" || body["tokenSet"] != false {
		t.Fatalf("GET tras desactivar: %v", body)
	}
}
