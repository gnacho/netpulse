package adapters

import (
	"testing"
	"time"
)

// TestBuildDevicesNamesFromReverseDNS: a static-IP host seen only in ARP
// takes its PTR name once the background lookup lands; a device with a
// DHCP hostname keeps it and is never looked up.
func TestBuildDevicesNamesFromReverseDNS(t *testing.T) {
	f := &fakeRDNS{
		ptr: map[string][]string{
			"172.16.16.4":  {"switch-garage.home.arpa."},
			"172.16.16.70": {"should-not-win.home.arpa."},
		},
		forward: map[string][]string{
			"switch-garage.home.arpa":  {"172.16.16.4"},
			"should-not-win.home.arpa": {"172.16.16.70"},
		},
	}
	l := NewLive(nil, nil, nil, nil)
	l.rdns = newTestRDNS(f, time.Now)
	polled := map[string]*routerPolled{
		"gw": {
			cfg: RouterConfig{ID: "gw", Name: "Gateway", Host: "172.16.16.1", IsGateway: true},
			arp: map[string]string{
				"5C:E2:8C:00:00:04": "172.16.16.4",
			},
			leases: []DhcpLease{
				{MAC: "AA:BB:CC:00:00:70", IP: "172.16.16.70", Hostname: "laptop"},
			},
		},
	}
	byMAC := func(devs []Device) map[string]Device {
		m := map[string]Device{}
		for _, d := range devs {
			m[d.MAC] = d
		}
		return m
	}

	first := byMAC(l.buildDevices(polled, ""))
	if got := first["5C:E2:8C:00:00:04"].Name; got != "5C:E2:8C:00:00:04" {
		t.Fatalf("first poll must not wait on DNS: got %q", got)
	}
	l.rdns.wg.Wait()

	devs := byMAC(l.buildDevices(polled, ""))
	if got := devs["5C:E2:8C:00:00:04"].Name; got != "switch-garage" {
		t.Fatalf("static host: got %q, want switch-garage", got)
	}
	if got := devs["AA:BB:CC:00:00:70"].Name; got != "laptop" {
		t.Fatalf("leased host: got %q, want laptop", got)
	}
	if n := f.count("172.16.16.70"); n != 0 {
		t.Fatalf("named device was looked up %d times", n)
	}
}
