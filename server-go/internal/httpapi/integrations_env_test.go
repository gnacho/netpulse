package httpapi

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/rintegration"
)

// seedVendor devuelve un vendor de la semilla embebida del catálogo (#1697).
func seedVendor(t *testing.T, id string) rintegration.Vendor {
	t.Helper()
	v, ok := rintegration.NewCatalog(nil).Lookup(id)
	if !ok {
		t.Fatalf("vendor %q missing from the embedded seed", id)
	}
	return v
}

func TestPusherEnvWritesIntervalWithUnit(t *testing.T) {
	s := &server{}
	v := seedVendor(t, "routeros")
	row := rintegration.Row{Slug: "lab", Host: "10.0.0.1", Username: "u", IntervalS: 30}
	env := s.pusherEnv(v, row, "secret", "https://np.example", "tok")
	if got := env["NETPULSE_INTERVAL"]; got != "30s" {
		t.Fatalf("NETPULSE_INTERVAL = %q, want 30s", got)
	}
	if got := env["ROUTEROS_HOST"]; got != "10.0.0.1" {
		t.Fatalf("ROUTEROS_HOST = %q, want 10.0.0.1", got)
	}

	asus := seedVendor(t, "asuswrt")
	sshEnv := s.pusherEnv(asus, row, "secret", "https://np.example", "tok")
	if got := sshEnv["ASUSWRT_HOST"]; got != "127.0.0.1" {
		t.Fatalf("ASUSWRT_HOST = %q, want 127.0.0.1", got)
	}
	if got := sshEnv["NETPULSE_INTERVAL"]; got != "30s" {
		t.Fatalf("ssh NETPULSE_INTERVAL = %q, want 30s", got)
	}
}
