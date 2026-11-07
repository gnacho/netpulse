package adapters

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// #1893: el mismo router llega como "test-gw.lan" (host de la config) y como
// "test-gw.lan:22" (SSHAddr). El puerto por defecto no distingue hosts.
func TestPoolKeyCollapsesTheDefaultPort(t *testing.T) {
	cases := []struct{ in, want string }{
		{"test-gw.lan", "test-gw.lan"},
		{"test-gw.lan:22", "test-gw.lan"},
		{"192.168.1.172:22", "192.168.1.172"},
		{"test-gw.lan:2222", "test-gw.lan:2222"},
		{"[2001:db8::1]:22", "2001:db8::1"},
		{"[2001:db8::1]:2222", "[2001:db8::1]:2222"},
	}
	for _, c := range cases {
		if got := poolKey(c.in); got != c.want {
			t.Errorf("poolKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Una credencial por unidad, aunque llegue con las dos formas.
func TestSetPasswordUsesOneEntryPerHost(t *testing.T) {
	p := &SSHPool{conns: map[string]*sshConn{}, creds: map[string]sshCred{}}
	p.SetPassword("test-gw.lan:22", "root", "s3cret")
	p.SetPassword("test-gw.lan", "root", "s3cret")
	if len(p.creds) != 1 {
		t.Fatalf("creds = %d entradas, esperaba 1 (%v)", len(p.creds), p.creds)
	}
	if _, ok := p.creds["test-gw.lan"]; !ok {
		t.Fatalf("la credencial no quedo bajo la clave normalizada: %v", p.creds)
	}
}

// Y una sola conexión: el segundo dial reutiliza la entrada (o su backoff) en
// vez de abrir un handshake nuevo al mismo equipo.
func TestDialCollapsesHostAndDefaultPort(t *testing.T) {
	kh := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(kh, nil, 0o600); err != nil {
		t.Fatalf("known_hosts: %v", err)
	}
	dials := 0
	p := &SSHPool{
		khPath: kh,
		conns:  map[string]*sshConn{},
		creds:  map[string]sshCred{},
		dialTCP: func(_, _ string, _ *ssh.ClientConfig) (*ssh.Client, error) {
			dials++
			return nil, errors.New("unreachable")
		},
	}
	if _, err := p.dial("test-gw.lan:22"); err == nil {
		t.Fatal("el primer dial debia fallar")
	}
	if _, err := p.dial("test-gw.lan"); err == nil {
		t.Fatal("el segundo dial debia caer en el backoff")
	}
	if dials != 1 {
		t.Fatalf("diales = %d, esperaba 1: host y host:22 son el mismo equipo", dials)
	}
	if len(p.conns) != 1 {
		t.Fatalf("conns = %d entradas, esperaba 1", len(p.conns))
	}
}
