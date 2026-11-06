package discover

// #1895: el descubrimiento ofrecía como nuevas unidades que ya estaban dadas
// de alta porque "configured" se calculaba comparando la cadena literal del
// host configurado con la IP del barrido.

import (
	"context"
	"errors"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

func TestHostKeyNormaliza(t *testing.T) {
	cases := map[string]string{
		"192.168.1.2":      "192.168.1.2",
		"192.168.1.2:2222": "192.168.1.2",
		"AP.Lan.":          "ap.lan",
		"  ap.lan  ":       "ap.lan",
		"[::1]:22":         "::1",
		"":                 "",
	}
	for in, want := range cases {
		if got := hostKey(in); got != want {
			t.Errorf("hostKey(%q) = %q, quiero %q", in, got, want)
		}
	}
}

func TestConfiguredIndexReconoceUnidadesExistentes(t *testing.T) {
	routers := []adapters.RouterConfig{
		{ID: "gw", Host: "192.168.1.1"},
		{ID: "ap", Host: "ap.lan"},
		{ID: "rt3", Host: "RT3.LAN."},
		{ID: "legacy", Host: "192.168.1.4:2222"},
		{ID: "roto", Host: "no-resuelve.lan"},
	}
	lookup := func(_ context.Context, host string) ([]string, error) {
		switch host {
		case "ap.lan":
			return []string{"192.168.1.2"}, nil
		case "rt3.lan":
			return []string{"192.168.1.3"}, nil
		default:
			return nil, errors.New("sin DNS")
		}
	}
	got := configuredIndex(context.Background(), routers, lookup)

	for ip, want := range map[string]bool{
		"192.168.1.1":  true,  // gateway por IP literal
		"192.168.1.2":  true,  // AP configurado por nombre
		"192.168.1.3":  true,  // nombre con mayúsculas y punto final
		"192.168.1.4":  true,  // IP con puerto en el alta
		"192.168.1.50": false, // unidad realmente nueva
	} {
		if got[hostKey(ip)] != want {
			t.Errorf("configured[%s] = %v, quiero %v", ip, got[hostKey(ip)], want)
		}
	}
	// El nombre que no resuelve sigue marcado por su cadena literal, pero no
	// inventa ninguna IP.
	if !got["no-resuelve.lan"] {
		t.Error("el nombre sin DNS debería seguir marcado por cadena literal")
	}
}
