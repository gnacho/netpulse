// integrations_sshpass_test.go - #1708: el fichero compañero de contraseña
// SSH (<slug>.sshpass) del modo ssh: se escribe al editar con contraseña,
// se conserva al editar sin ella, muere con la integración y su fallo de
// autenticación sale con código propio (ssh_auth_failed).
package httpapi_test

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/rintegration"
)

// sshPassFakePool registra la secuencia de eventos (set/run) para poder
// afirmar el orden: la credencial se aplica ANTES de cada comando SSH.
type sshPassFakePool struct {
	mu     sync.Mutex
	events []string
	runs   int
	fail   bool
}

func (f *sshPassFakePool) Run(host, cmd string, _ time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs++
	if f.runs == 1 {
		f.events = append(f.events, "run:read")
	} else {
		f.events = append(f.events, fmt.Sprintf("run:call-%d", f.runs))
	}
	if f.fail {
		return "", &adapters.SSHAuthError{
			Host: host,
			Err:  errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password], no supported methods remain"),
		}
	}
	if f.runs == 1 {
		return "ASUSWRT_HOST=127.0.0.1\nASUSWRT_USER=admin\nASUSWRT_PASS=old\nNETPULSE_INTERVAL=45s\n", nil
	}
	return "", nil
}

func (f *sshPassFakePool) SetPassword(host, user, password string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, fmt.Sprintf("set:%s:%s", user, password))
}

func (f *sshPassFakePool) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.events...)
}

func seedSSHIntegration(t *testing.T, d *db.DB, mgr *rintegration.Manager) {
	t.Helper()
	store := rintegration.NewStore(d.DB)
	row := bootIntegrationRow("wrt-pass", "asuswrt", "ssh", true, "running")
	if err := store.Upsert(row); err != nil {
		t.Fatal(err)
	}
}

// TestIntegrationEditWritesSSHPassFile: editar con contraseña reescribe el
// fichero compañero y aplica la credencial al pool antes de conectar.
func TestIntegrationEditWritesSSHPassFile(t *testing.T) {
	pool := &sshPassFakePool{}
	ts, mgr := makeIntegrationsTestServerFull(t, func(d *db.DB, mgr *rintegration.Manager) {
		seedSSHIntegration(t, d, mgr)
		if err := mgr.WriteSSHPassword("wrt-pass", "old"); err != nil {
			t.Fatal(err)
		}
	}, nil, pool)

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := putJSONPath(t, ts.URL, "/api/integrations/wrt-pass", `{"password":"nueva","intervalS":45}`, cookie)
	body := readJSON(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT con password: status %d (%v)", res.StatusCode, body)
	}

	pass, err := mgr.ReadSSHPassword("wrt-pass")
	if err != nil || pass != "nueva" {
		t.Fatalf("fichero tras editar = %q (err %v), quiero \"nueva\"", pass, err)
	}
	events := pool.recorded()
	if len(events) < 3 {
		t.Fatalf("secuencia corta: %v", events)
	}
	if events[0] != "set:admin:nueva" {
		t.Fatalf("la credencial debe aplicarse antes de conectar: %v", events)
	}
	if events[1] != "run:read" {
		t.Fatalf("la edición debe leer el env remoto tras aplicar la credencial: %v", events)
	}
	if events[len(events)-1] != "run:call-2" {
		t.Fatalf("la edición debe reescribir el env en una segunda llamada: %v", events)
	}
}

// TestIntegrationEditKeepsSSHPassWhenEmpty: editar sin contraseña conserva
// la guardada y la sigue aplicando al pool (la conexión sigue siendo por
// contraseña).
func TestIntegrationEditKeepsSSHPassWhenEmpty(t *testing.T) {
	pool := &sshPassFakePool{}
	ts, mgr := makeIntegrationsTestServerFull(t, func(d *db.DB, mgr *rintegration.Manager) {
		seedSSHIntegration(t, d, mgr)
		if err := mgr.WriteSSHPassword("wrt-pass", "guardada"); err != nil {
			t.Fatal(err)
		}
	}, nil, pool)

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := putJSONPath(t, ts.URL, "/api/integrations/wrt-pass", `{"intervalS":60}`, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT sin password: status %d", res.StatusCode)
	}

	pass, err := mgr.ReadSSHPassword("wrt-pass")
	if err != nil || pass != "guardada" {
		t.Fatalf("el fichero debe conservarse: %q (err %v)", pass, err)
	}
	for _, ev := range pool.recorded() {
		if ev == "set:admin:guardada" {
			return
		}
	}
	t.Fatalf("el pool debe recibir la credencial conservada: %v", pool.recorded())
}

// TestIntegrationDeleteRemovesSSHPassFile: borrar la integración quita el
// fichero compañero y limpia la credencial del pool.
func TestIntegrationDeleteRemovesSSHPassFile(t *testing.T) {
	pool := &sshPassFakePool{}
	ts, mgr := makeIntegrationsTestServerFull(t, func(d *db.DB, mgr *rintegration.Manager) {
		seedSSHIntegration(t, d, mgr)
		if err := mgr.WriteSSHPassword("wrt-pass", "old"); err != nil {
			t.Fatal(err)
		}
	}, nil, pool)

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := deleteJSONPath(t, ts.URL, "/api/integrations/wrt-pass", cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE: status %d", res.StatusCode)
	}

	if pass, err := mgr.ReadSSHPassword("wrt-pass"); err != nil || pass != "" {
		t.Fatalf("el fichero debe desaparecer con la integración: %q (err %v)", pass, err)
	}
	for _, ev := range pool.recorded() {
		if ev == "set::" {
			return
		}
	}
	t.Fatalf("la credencial del pool debe limpiarse al borrar: %v", pool.recorded())
}

// TestIntegrationSSHAuthErrorIsItsOwnCode: el rechazo de autenticación
// reporta ssh_auth_failed, no ssh_failed, para que la UI pida revisar la
// contraseña.
func TestIntegrationSSHAuthErrorIsItsOwnCode(t *testing.T) {
	pool := &sshPassFakePool{fail: true}
	ts, _ := makeIntegrationsTestServerFull(t, func(d *db.DB, mgr *rintegration.Manager) {
		seedSSHIntegration(t, d, mgr)
		if err := mgr.WriteSSHPassword("wrt-pass", "mala"); err != nil {
			t.Fatal(err)
		}
	}, nil, pool)

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := putJSONPath(t, ts.URL, "/api/integrations/wrt-pass", `{"intervalS":60}`, cookie)
	body := readJSON(t, res)
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, quiero 502 (%v)", res.StatusCode, body)
	}
	if body["error"] != "ssh_auth_failed" {
		t.Fatalf("código de error = %v, quiero ssh_auth_failed", body["error"])
	}
}
