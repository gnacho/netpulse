package httpapi

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/rintegration"
)

func TestPusherEnvWritesIntervalWithUnit(t *testing.T) {
	s := &server{}
	v, ok := rintegration.LookupVendor("routeros")
	if !ok {
		t.Fatal("routeros vendor missing")
	}
	row := rintegration.Row{Slug: "lab", Host: "10.0.0.1", Username: "u", IntervalS: 30}
	env := s.pusherEnv(v, row, "secret", "https://np.example", "tok")
	if got := env["NETPULSE_INTERVAL"]; got != "30s" {
		t.Fatalf("NETPULSE_INTERVAL = %q, want 30s", got)
	}
	if got := env["ROUTEROS_HOST"]; got != "10.0.0.1" {
		t.Fatalf("ROUTEROS_HOST = %q, want 10.0.0.1", got)
	}

	asus, ok := rintegration.LookupVendor("asuswrt")
	if !ok {
		t.Fatal("asuswrt vendor missing")
	}
	sshEnv := s.pusherEnv(asus, row, "secret", "https://np.example", "tok")
	if got := sshEnv["ASUSWRT_HOST"]; got != "127.0.0.1" {
		t.Fatalf("ASUSWRT_HOST = %q, want 127.0.0.1", got)
	}
	if got := sshEnv["NETPULSE_INTERVAL"]; got != "30s" {
		t.Fatalf("ssh NETPULSE_INTERVAL = %q, want 30s", got)
	}
}
