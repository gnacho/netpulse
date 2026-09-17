// device_reservation_test.go — a client with a fixed address of its own takes
// no lease, so the name pinned in /etc/config/dhcp is the only one there is.
// End to end through the server: agent payload → polledFromAgent →
// buildDevices, which is where an earlier attempt broke (the reservation map
// was declared and read but never filled, so every such client stayed a MAC).
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestBuildDevicesNamesFromReservation(t *testing.T) {
	cfg := RouterConfig{ID: "gateway", Host: "192.0.2.1", Name: "openwrt", IsGateway: true, AgentOnly: true}
	l := NewLive(nil, nil, []RouterConfig{cfg}, nil)
	payload := &probe.Payload{
		Router: "gateway", Ts: 1, Version: "2.28.24",
		Data: probe.PayloadData{
			// Wired, seen only in the bridge FDB: no lease anywhere.
			FDB: &probe.FDBData{MACs: map[string]string{"02:00:00:00:00:01": "lan2"}},
			DHCP: &probe.DHCPData{
				Reservations: []probe.DhcpReservation{
					{MAC: "02:00:00:00:00:01", Name: "printer", IP: "192.0.2.10"},
				},
			},
		},
	}
	devices := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, payload)})
	var found bool
	for _, d := range devices {
		if d.MAC != "02:00:00:00:00:01" {
			continue
		}
		found = true
		if d.Name != "printer" {
			t.Errorf("name = %q, want printer (from the reservation)", d.Name)
		}
		if d.IP != "192.0.2.10" {
			t.Errorf("ip = %q, want the reserved address", d.IP)
		}
	}
	if !found {
		t.Fatalf("the wired client never made it into the device list (%d devices)", len(devices))
	}
}

func TestBuildDevicesLeaseHostnameWinsOverReservation(t *testing.T) {
	cfg := RouterConfig{ID: "gateway", Host: "192.0.2.1", Name: "openwrt", IsGateway: true, AgentOnly: true}
	l := NewLive(nil, nil, []RouterConfig{cfg}, nil)
	payload := &probe.Payload{
		Router: "gateway", Ts: 1, Version: "2.28.24",
		Data: probe.PayloadData{
			FDB: &probe.FDBData{MACs: map[string]string{"02:00:00:00:00:02": "lan2"}},
			DHCP: &probe.DHCPData{
				Leases: []probe.DhcpLease{{MAC: "02:00:00:00:00:02", IP: "192.0.2.30", Hostname: "laptop"}},
				Reservations: []probe.DhcpReservation{
					{MAC: "02:00:00:00:00:02", Name: "old-name", IP: "192.0.2.99"},
				},
			},
		},
	}
	devices := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, payload)})
	for _, d := range devices {
		if d.MAC == "02:00:00:00:00:02" && (d.Name != "laptop" || d.IP != "192.0.2.30") {
			t.Fatalf("the lease must win: %+v", d)
		}
	}
}
