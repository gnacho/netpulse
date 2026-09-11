// owut_test.go - endpoint POST /api/firmware-upgrades/{routerId}/owut-check
// (#695 Fase 1). Con el SSHRunner fakeable scriptedSSH (mismo fake que
// device_actions_test.go): NINGÚN router real se toca.
package httpapi_test

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// owutCheckOKOut simula `owut check` con upgrade disponible y buildable (exit 0).
const owutCheckOKOut = `ASU-Server     https://sysupgrade.openwrt.org
3 packages are out-of-date
It is safe to proceed with an upgrade (re-run with '--verbose' for details)
__owut_exit__=0
`

// makeOwutTestServer monta un servidor con el pool SSH fakeado y el store de
// firmware activo, añade un router openwrt y devuelve servidor + router id.
func makeOwutTestServer(t *testing.T, ssh *scriptedSSH) (*testServer, string) {
	t.Helper()
	auth.SetTrustProxy(true)
	t.Cleanup(func() { auth.SetTrustProxy(false) })
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
	r, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "RouterFW", Host: "192.168.1.50", Type: "openwrt",
	})
	if err != nil {
		t.Fatalf("add router: %v", err)
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
		Firmware: firmware.NewStore(d.DB),
	})
	srv := httptest.NewServer(handler)
	ts := &testServer{Server: srv, db: d, secret: secret}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts, r.ID
}

func TestOwutCheckUpgradeAvailable(t *testing.T) {
	ssh := &scriptedSSH{rules: []sshRule{
		{contains: "command -v owut", out: "/usr/bin/owut\n"},
		{contains: "owut check", out: owutCheckOKOut},
	}}
	ts, rid := makeOwutTestServer(t, ssh)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-check", "{}", cookie)
	body := readJSON(t, res)
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %v", res.StatusCode, body)
	}
	if body["owutAvailable"] != true {
		t.Fatalf("owutAvailable: %v", body["owutAvailable"])
	}
	if body["checkRan"] != true {
		t.Fatalf("checkRan: %v", body["checkRan"])
	}
	if body["upgradeAvailable"] != true {
		t.Fatalf("upgradeAvailable: %v", body["upgradeAvailable"])
	}
	if body["buildable"] != true {
		t.Fatalf("buildable: %v", body["buildable"])
	}
	if raw, _ := body["rawOutput"].(string); strings.Contains(raw, "__owut_exit__") {
		t.Fatalf("rawOutput debe ir sin el marcador __owut_exit__: %q", raw)
	}
	if !ssh.saw("owut check") {
		t.Fatalf("no se lanzó owut check; comandos: %v", ssh.cmdsSnapshot())
	}
	if !ssh.sawHost("192.168.1.50", "owut check") {
		t.Fatalf("owut check no se lanzó contra el host del router: %v", ssh.cmdsSnapshot())
	}
}

func TestOwutCheckAbsent(t *testing.T) {
	ssh := &scriptedSSH{rules: []sshRule{
		{contains: "command -v owut", err: errors.New("exit status 127")}, // exit != 0 → no hay owut
	}}
	ts, rid := makeOwutTestServer(t, ssh)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-check", "{}", cookie)
	body := readJSON(t, res)
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %v", res.StatusCode, body)
	}
	if body["owutAvailable"] != false {
		t.Fatalf("owutAvailable: %v", body["owutAvailable"])
	}
	if ssh.saw("owut check") {
		t.Fatalf("no debería lanzarse owut check sin owut: %v", ssh.cmdsSnapshot())
	}
}

func TestOwutCheckNoSSHPool(t *testing.T) {
	// Sin Pool (demo/sin clave): el endpoint responde 503 no_ssh.
	ts, rid := makeOwutTestServer(t, nil)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-check", "{}", cookie)
	body := readJSON(t, res)
	if res.StatusCode != 503 {
		t.Fatalf("status %d, esperaba 503: %v", res.StatusCode, body)
	}
	if body["error"] != "no_ssh" {
		t.Fatalf("error: %v", body["error"])
	}
}
