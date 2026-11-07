package routerstore

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// #1968: the integration form asks for the full URL, but the unit only accepts
// an IP or a hostname. The address is taken out of the URL and anything that
// does not end up being a valid host is returned untouched.
func TestBareHostFromIntegrationForm(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://192.168.1.1", "192.168.1.1"},
		{"https://fw.mandor.lan", "fw.mandor.lan"},
		{"https://192.168.1.1:8443", "192.168.1.1"},
		{"http://opnsense.local/", "opnsense.local"},
		{"https://user:pass@fw.local:8443/ui", "fw.local"},
		{"192.168.1.1", "192.168.1.1"},
		{"  fw.local  ", "fw.local"},
		{"fw.local:8443", "fw.local"},
		{"", ""},
		// IPv6 does not fit the form contract (it rejects ':'), so it keeps the
		// original value instead of becoming something the form still rejects.
		{"https://[2001:db8::1]:443", "https://[2001:db8::1]:443"},
		{"https:///path", "https:///path"},
	}
	for _, c := range cases {
		if got := BareHost(c.in); got != c.want {
			t.Errorf("BareHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAddRouterStoresTheBareHost(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	cfg, err := AddRouter(d.DB, AddInput{Name: "opnsense", Host: "https://192.168.1.1:8443", Type: "external", AgentOnly: true})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if cfg.Host != "192.168.1.1" {
		t.Fatalf("host de la unidad = %q, esperaba 192.168.1.1", cfg.Host)
	}
	listed := ListRouters(d.DB)
	if len(listed) != 1 || listed[0].Host != "192.168.1.1" {
		t.Fatalf("ListRouters devolvio %+v", listed)
	}
}

func TestNormalizeExternalHostsFixesExistingRows(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	cfg, err := AddRouter(d.DB, AddInput{Name: "fw", Host: "192.168.1.1", Type: "external", AgentOnly: true})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// Fila tal como la dejaba el alta anterior al arreglo.
	if _, err := d.DB.Exec(`UPDATE routers SET host = ? WHERE id = ?`, "https://192.168.1.1:8443", cfg.ID); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if n := NormalizeExternalHosts(d.DB); n != 1 {
		t.Fatalf("normalizadas = %d, esperaba 1", n)
	}
	if got := ListRouters(d.DB)[0].Host; got != "192.168.1.1" {
		t.Fatalf("host tras normalizar = %q", got)
	}
	if n := NormalizeExternalHosts(d.DB); n != 0 {
		t.Fatalf("segunda pasada cambio %d filas, no es idempotente", n)
	}

	// Una unidad que no es external no se toca: su host lo valida el formulario.
	local, err := AddRouter(d.DB, AddInput{Name: "rt1", Host: "192.168.1.2", Type: "openwrt"})
	if err != nil {
		t.Fatalf("add local: %v", err)
	}
	if _, err := d.DB.Exec(`UPDATE routers SET host = ? WHERE id = ?`, "https://192.168.1.2", local.ID); err != nil {
		t.Fatalf("seed local row: %v", err)
	}
	if n := NormalizeExternalHosts(d.DB); n != 0 {
		t.Fatalf("normalizo %d unidades que no son external", n)
	}
}
