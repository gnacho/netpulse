// device_actions_test.go — issue #439: reserva DHCP + bloqueo de dispositivo.
// Todo con fakes (SSHRunner scripteable + routers en DB temporal); NINGÚN
// router real se toca. Cubre create/update/idempotencia/conflicto, fallos SSH,
// rollback ante fallo de reload, gate admin y validación.
package httpapi_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

const (
	devMAC   = "aa:bb:cc:dd:ee:ff"
	otherMAC = "11:22:33:44:55:66"
)

// --- fake SSHRunner (por subcadena, en orden) ---

type sshRule struct {
	contains string
	out      string
	err      error
}

type scriptedSSH struct {
	mu    sync.Mutex
	cmds  []string
	rules []sshRule
}

func (f *scriptedSSH) Run(_host, cmd string, _ time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, cmd)
	for _, r := range f.rules {
		if strings.Contains(cmd, r.contains) {
			return r.out, r.err
		}
	}
	return "", nil
}

func (f *scriptedSSH) saw(substr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cmds {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func (f *scriptedSSH) count(substr string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.cmds {
		if strings.Contains(c, substr) {
			n++
		}
	}
	return n
}

// --- fixtures uci show ---

const dhcpEmpty = `dhcp.@dnsmasq[0]=dnsmasq
dhcp.@dnsmasq[0].server='127.0.0.1#5353'
dhcp.lan=dhcp
dhcp.lan.interface='lan'
dhcp.lan.start='100'
dhcp.lan.limit='150'
`

const dhcpOtherHost = dhcpEmpty + `dhcp.@host[0]=host
dhcp.@host[0].name='tv-salon'
dhcp.@host[0].mac='11:22:33:44:55:66'
dhcp.@host[0].ip='192.168.1.50'
`

const dhcpDevHost = dhcpEmpty + `dhcp.np_host_aabbccddeeff=host
dhcp.np_host_aabbccddeeff.name='tv-salon'
dhcp.np_host_aabbccddeeff.mac='aa:bb:cc:dd:ee:ff'
dhcp.np_host_aabbccddeeff.ip='192.168.1.60'
`

const fwEmpty = `firewall.@defaults[0]=defaults
firewall.@defaults[0].input='ACCEPT'
firewall.lan=zone
firewall.lan.name='lan'
firewall.lan.network='lan'
firewall.@rule[0]=rule
firewall.@rule[0].name='Allow-DHCP'
firewall.@rule[0].src_mac='00:00:00:00:00:00'
firewall.@rule[0].target='ACCEPT'
`

const fwBlocked = fwEmpty + `firewall.np_block_aabbccddeeff=rule
firewall.np_block_aabbccddeeff.name='np-block-aabbccddeeff'
firewall.np_block_aabbccddeeff.src='lan'
firewall.np_block_aabbccddeeff.dest='*'
firewall.np_block_aabbccddeeff.target='DROP'
firewall.np_block_aabbccddeeff.src_mac='aa:bb:cc:dd:ee:ff'
`

// --- servidor de test ---

type deviceActionsTestServer struct {
	*httptest.Server
	db     *db.DB
	ssh    *scriptedSSH
	gwID   string
	apID   string
	cookie string
}

func makeDeviceActionsTestServer(t *testing.T, ssh *scriptedSSH) *deviceActionsTestServer {
	t.Helper()
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
	}, dataDir)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatalf("users: %v", err)
	}
	gw, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "gateway", Host: "192.168.1.1", Type: "openwrt", IsGateway: true,
	})
	if err != nil {
		t.Fatalf("AddRouter gateway: %v", err)
	}
	ap, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "patio", Host: "192.168.1.2", Type: "openwrt",
	})
	if err != nil {
		t.Fatalf("AddRouter patio: %v", err)
	}
	agents := adapters.NewAgentRegistry(0)
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	var runner httpapi.SSHRunner
	if ssh != nil {
		runner = ssh
	}
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Hub: hub, Secret: secret,
		Agents: agents, Pool: runner, Started: time.Now(),
	})
	srv := httptest.NewServer(handler)
	status, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if status != 204 {
		t.Fatalf("login: %d", status)
	}
	ts := &deviceActionsTestServer{Server: srv, db: d, ssh: ssh, gwID: gw.ID, apID: ap.ID, cookie: cookie}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts
}

func deviceReq(t *testing.T, method, base, path, cookie, body string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

func decodeBody(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	defer res.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return m
}

// newSSH crea el fake con el `uci show` scripteado (dhcp + firewall) y reglas
// extra (fallos). Por defecto todo tiene éxito.
func newSSH(dhcp, fw string, extra ...sshRule) *scriptedSSH {
	// Los fallos (extra) van PRIMERO para que ganen sobre las salidas por
	// defecto cuando comparten subcadena (p. ej. "uci show dhcp").
	rules := append([]sshRule{}, extra...)
	rules = append(rules,
		sshRule{contains: "uci show dhcp", out: dhcp},
		sshRule{contains: "uci show firewall", out: fw},
	)
	return &scriptedSSH{rules: rules}
}

// --- Reserva DHCP ---

func TestReservationCreate(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie,
		`{"ip":"192.168.1.60","hostname":"tv-salon"}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT reservation: %d (body %v)", res.StatusCode, decodeBody(t, res))
	}
	res.Body.Close()
	if !ssh.saw("uci set dhcp.np_host_aabbccddeeff=host") {
		t.Error("falta la creación de la sección host")
	}
	if !ssh.saw("uci set dhcp.np_host_aabbccddeeff.mac='aa:bb:cc:dd:ee:ff'") {
		t.Error("falta la mac en la sección host")
	}
	if !ssh.saw("uci set dhcp.np_host_aabbccddeeff.ip='192.168.1.60'") {
		t.Error("falta la ip en la sección host")
	}
	if !ssh.saw("uci commit dhcp") {
		t.Error("falta uci commit dhcp")
	}
	if !ssh.saw("/etc/init.d/dnsmasq restart") {
		t.Error("falta el reinicio de dnsmasq")
	}
}

func TestReservationUpdate(t *testing.T) {
	ssh := newSSH(dhcpDevHost, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie,
		`{"ip":"192.168.1.70"}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT reservation: %d", res.StatusCode)
	}
	res.Body.Close()
	if !ssh.saw("uci set dhcp.np_host_aabbccddeeff.ip='192.168.1.70'") {
		t.Error("falta la actualización de ip en la sección existente")
	}
}

func TestReservationIdempotent(t *testing.T) {
	ssh := newSSH(dhcpDevHost, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie,
		`{"ip":"192.168.1.60"}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT reservation idempotente: %d", res.StatusCode)
	}
	res.Body.Close()
	// La misma IP no debe generar conflicto (es la propia MAC).
	if ssh.saw("uci add") {
		t.Error("no debe añadir secciones nuevas en update idempotente")
	}
}

func TestReservationConflict(t *testing.T) {
	ssh := newSSH(dhcpOtherHost, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie,
		`{"ip":"192.168.1.50"}`)
	if res.StatusCode != 409 {
		t.Fatalf("conflicto: got %d want 409", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["error"] != "ip_conflict" {
		t.Errorf("error code: %v", body["error"])
	}
	if ssh.saw("uci commit dhcp") {
		t.Error("no debe escribir nada en conflicto")
	}
}

func TestReservationGet(t *testing.T) {
	ssh := newSSH(dhcpDevHost, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "GET", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie, "")
	if res.StatusCode != 200 {
		t.Fatalf("GET reservation: %d", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["reserved"] != true || body["ip"] != "192.168.1.60" {
		t.Errorf("reserved payload: %v", body)
	}
}

func TestReservationGetNotReserved(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "GET", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie, "")
	body := decodeBody(t, res)
	if body["reserved"] != false {
		t.Errorf("reserved debería ser false: %v", body)
	}
}

func TestReservationNoGateway(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)
	if !routerstore.RemoveRouter(ts.db.DB, ts.gwID) {
		t.Fatal("no se pudo quitar el gateway")
	}

	res := deviceReq(t, "GET", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie, "")
	if res.StatusCode != 400 {
		t.Fatalf("sin gateway: got %d want 400", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["error"] != "no_gateway" {
		t.Errorf("error code: %v", body["error"])
	}
}

func TestReservationSSHReadFailure(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty, sshRule{contains: "uci show dhcp", err: errors.New("ssh down")})
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "GET", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie, "")
	if res.StatusCode != 500 {
		t.Fatalf("ssh read failure: got %d want 500", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["error"] != "ssh_error" {
		t.Errorf("error code: %v", body["error"])
	}
}

func TestReservationApplyFailure(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty, sshRule{contains: "uci commit dhcp", err: errors.New("commit fail")})
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie,
		`{"ip":"192.168.1.60"}`)
	if res.StatusCode != 500 {
		t.Fatalf("apply failure: got %d want 500", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["error"] != "apply_error" {
		t.Errorf("error code: %v", body["error"])
	}
	if ssh.saw("/etc/init.d/dnsmasq restart") {
		t.Error("no debe reiniciar dnsmasq si el commit falló")
	}
}

func TestReservationReloadFailureRollsBack(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty, sshRule{contains: "restart", err: errors.New("reload fail")})
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie,
		`{"ip":"192.168.1.60"}`)
	if res.StatusCode != 500 {
		t.Fatalf("reload failure: got %d want 500", res.StatusCode)
	}
	res.Body.Close()
	if !ssh.saw("uci delete dhcp.np_host_aabbccddeeff") {
		t.Error("falta el rollback (borrado de la sección recién creada)")
	}
}

func TestReservationDelete(t *testing.T) {
	ssh := newSSH(dhcpDevHost, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "DELETE", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie, "")
	if res.StatusCode != 200 {
		t.Fatalf("DELETE reservation: %d", res.StatusCode)
	}
	res.Body.Close()
	if !ssh.saw("uci delete dhcp.np_host_aabbccddeeff") {
		t.Error("falta el borrado de la sección host")
	}
	if !ssh.saw("/etc/init.d/dnsmasq restart") {
		t.Error("falta el reinicio de dnsmasq")
	}
}

func TestReservationDeleteNotFound(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "DELETE", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie, "")
	if res.StatusCode != 404 {
		t.Fatalf("DELETE not reserved: got %d want 404", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["error"] != "not_reserved" {
		t.Errorf("error code: %v", body["error"])
	}
}

// --- Bloqueo ---

func TestBlockAdd(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/block", ts.cookie,
		`{"router":"`+ts.apID+`"}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT block: %d", res.StatusCode)
	}
	res.Body.Close()
	if !ssh.saw("uci set firewall.np_block_aabbccddeeff=rule") {
		t.Error("falta la sección rule nombrada")
	}
	if !ssh.saw("uci set firewall.np_block_aabbccddeeff.src_mac='aa:bb:cc:dd:ee:ff'") {
		t.Error("falta src_mac")
	}
	if !ssh.saw("uci set firewall.np_block_aabbccddeeff.target='DROP'") {
		t.Error("falta target DROP")
	}
	if !ssh.saw("/etc/init.d/firewall restart") {
		t.Error("falta el reinicio del firewall")
	}
}

func TestBlockIdempotent(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwBlocked)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/block", ts.cookie,
		`{"router":"`+ts.apID+`"}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT block ya bloqueado: %d", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["already"] != true {
		t.Errorf("already: %v", body["already"])
	}
	if ssh.saw("uci commit firewall") {
		t.Error("no debe reescribir si ya está bloqueado")
	}
}

func TestBlockGet(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwBlocked)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "GET", ts.URL, "/api/devices/"+devMAC+"/block?router="+ts.apID, ts.cookie, "")
	body := decodeBody(t, res)
	if body["blocked"] != true {
		t.Errorf("blocked debería ser true: %v", body)
	}
}

func TestBlockRemove(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwBlocked)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "DELETE", ts.URL, "/api/devices/"+devMAC+"/block", ts.cookie,
		`{"router":"`+ts.apID+`"}`)
	if res.StatusCode != 200 {
		t.Fatalf("DELETE block: %d", res.StatusCode)
	}
	res.Body.Close()
	if !ssh.saw("uci delete firewall.np_block_aabbccddeeff") {
		t.Error("falta el borrado de la regla")
	}
	if !ssh.saw("/etc/init.d/firewall restart") {
		t.Error("falta el reinicio del firewall")
	}
}

func TestBlockUnblockIdempotent(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "DELETE", ts.URL, "/api/devices/"+devMAC+"/block", ts.cookie,
		`{"router":"`+ts.apID+`"}`)
	if res.StatusCode != 200 {
		t.Fatalf("DELETE block no bloqueado: %d", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["already"] != true {
		t.Errorf("already: %v", body["already"])
	}
}

func TestBlockReloadFailureRollsBack(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwBlocked, sshRule{contains: "restart", err: errors.New("reload fail")})
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "DELETE", ts.URL, "/api/devices/"+devMAC+"/block", ts.cookie,
		`{"router":"`+ts.apID+`"}`)
	if res.StatusCode != 500 {
		t.Fatalf("reload failure: got %d want 500", res.StatusCode)
	}
	res.Body.Close()
	if !ssh.saw("uci set firewall.np_block_aabbccddeeff=rule") {
		t.Error("falta el rollback (re-crear la regla)")
	}
}

// --- Validación y gate admin ---

func TestReservationInvalidMAC(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "GET", ts.URL, "/api/devices/aa:bb:cc:dd:ee/reservation", ts.cookie, "")
	if res.StatusCode != 400 {
		t.Fatalf("mac inválida: got %d want 400", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["error"] != "invalid_mac" {
		t.Errorf("error code: %v", body["error"])
	}
}

func TestReservationInvalidIP(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/reservation", ts.cookie,
		`{"ip":"no-es-ip"}`)
	if res.StatusCode != 400 {
		t.Fatalf("ip inválida: got %d want 400", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["error"] != "invalid_ip" {
		t.Errorf("error code: %v", body["error"])
	}
}

func TestBlockMissingRouter(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/block", ts.cookie, `{}`)
	if res.StatusCode != 400 {
		t.Fatalf("router ausente: got %d want 400", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["error"] != "missing_router" {
		t.Errorf("error code: %v", body["error"])
	}
}

func TestBlockUnknownRouter(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	res := deviceReq(t, "PUT", ts.URL, "/api/devices/"+devMAC+"/block", ts.cookie,
		`{"router":"no-existe"}`)
	if res.StatusCode != 400 {
		t.Fatalf("router desconocido: got %d want 400", res.StatusCode)
	}
	body := decodeBody(t, res)
	if body["error"] != "no_router" {
		t.Errorf("error code: %v", body["error"])
	}
}

func TestDeviceActionsAdminOnly(t *testing.T) {
	ssh := newSSH(dhcpEmpty, fwEmpty)
	ts := makeDeviceActionsTestServer(t, ssh)

	// Sin sesión → 401 (requireAuth global).
	res := deviceReq(t, "GET", ts.URL, "/api/devices/"+devMAC+"/reservation", "", "")
	if res.StatusCode != 401 {
		t.Errorf("sin sesión: got %d want 401", res.StatusCode)
	}
	res.Body.Close()

	// Rol user → 403 (RequireAdmin).
	_, adminCookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	userCookie := createUserAndLogin(t, ts.URL, adminCookie, "viewer", "clave12345", "user")
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/devices/" + devMAC + "/reservation"},
		{"PUT", "/api/devices/" + devMAC + "/reservation"},
		{"GET", "/api/devices/" + devMAC + "/block?router=" + ts.apID},
		{"PUT", "/api/devices/" + devMAC + "/block"},
	} {
		if got := do(t, rt.method, ts.URL, rt.path, userCookie).StatusCode; got != 403 {
			t.Errorf("%s %s como user: got %d want 403", rt.method, rt.path, got)
		}
	}
}
