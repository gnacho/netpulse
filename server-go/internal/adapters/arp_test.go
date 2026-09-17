// arp_test.go — Fallback de resolución IP vía tabla ARP (#377).
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestBuildDevicesArpFallback(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"patio": {
			cfg: RouterConfig{ID: "patio", Name: "Patio", Host: "192.168.1.2"},
			wireless: map[string]WirelessClient{
				"AA:BB:CC:DD:EE:FF": {SignalDbm: -55, Band: "5 GHz"},
			},
			arp: map[string]string{
				"AA:BB:CC:DD:EE:FF": "192.168.1.50",
			},
		},
	}
	devs := l.buildDevices(polled)
	if len(devs) != 1 {
		t.Fatalf("esperado 1 dispositivo, got %d: %+v", len(devs), devs)
	}
	if devs[0].IP != "192.168.1.50" {
		t.Fatalf("IP esperada 192.168.1.50, got %q (dispositivo %+v)", devs[0].IP, devs[0])
	}
}

// TestBuildDevicesArpDiscovery: una MAC visible SOLO por ARP (sin wireless,
// sin FDB, sin lease, sin device_attrib) aparece como dispositivo cableado
// online con la IP de la tabla ARP y el RouterID del router que la reportó
// (#507).
func TestBuildDevicesArpDiscovery(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"patio": {
			cfg: RouterConfig{ID: "patio", Name: "Patio", Host: "192.168.1.2"},
			arp: map[string]string{
				"AA:BB:CC:DD:EE:FF": "192.168.1.60",
			},
		},
	}
	devs := l.buildDevices(polled)
	if len(devs) != 1 {
		t.Fatalf("esperado 1 dispositivo, got %d: %+v", len(devs), devs)
	}
	d := devs[0]
	if d.MAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("MAC esperada AA:BB:CC:DD:EE:FF, got %q", d.MAC)
	}
	if d.IP != "192.168.1.60" {
		t.Fatalf("IP esperada 192.168.1.60, got %q", d.IP)
	}
	if d.Band != "cable" {
		t.Fatalf("Band esperada cable, got %q", d.Band)
	}
	if !d.Online {
		t.Fatalf("Online esperado true (presencia ARP = activo reciente)")
	}
	if d.RouterID != "patio" {
		t.Fatalf("RouterID esperado patio, got %q", d.RouterID)
	}
}

// TestBuildDevicesArpRouterExcluded: una MAC de bridge/router presente en la
// tabla ARP NO debe aparecer como dispositivo (#507).
func TestBuildDevicesArpRouterExcluded(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"patio": {
			cfg:   RouterConfig{ID: "patio", Name: "Patio", Host: "192.168.1.2"},
			brMac: "AA:BB:CC:DD:EE:FF",
			arp: map[string]string{
				"AA:BB:CC:DD:EE:FF": "192.168.1.60",
			},
		},
	}
	devs := l.buildDevices(polled)
	if len(devs) != 0 {
		t.Fatalf("esperado 0 dispositivos (bridge MAC excluida), got %d: %+v", len(devs), devs)
	}
}

func TestPolledFromAgentArp(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	pl := &probe.Payload{Router: "patio", Version: "0.1.0"}
	pl.Data.System = &probe.SystemData{SysInfo: &probe.SysInfo{Uptime: 100}}
	pl.Data.Arp = map[string]string{"AA:BB:CC:DD:EE:FF": "192.168.1.50"}
	p := l.polledFromAgent(RouterConfig{ID: "patio"}, pl)
	if p == nil {
		t.Fatal("polledFromAgent devolvió nil")
	}
	if p.arp["AA:BB:CC:DD:EE:FF"] != "192.168.1.50" {
		t.Fatalf("arp no propagada: %+v", p.arp)
	}
}

// A MAC whose only evidence is a neighbour entry the kernel has NOT
// confirmed is not a device: the entry outlives the host by minutes, and
// with no port either the map hung it off the gateway bubble.
func TestBuildDevicesIgnoresStaleArpOnlyHosts(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"patio": {
			cfg: RouterConfig{ID: "patio", Name: "Patio", Host: "192.0.2.2"},
			arp: map[string]string{
				"02:00:00:00:00:10": "192.0.2.60", // confirmed
				"02:00:00:00:00:11": "192.0.2.61", // only remembered
			},
			arpStale: map[string]bool{"02:00:00:00:00:11": true},
		},
	}
	devs := l.buildDevices(polled)
	if len(devs) != 1 || devs[0].MAC != "02:00:00:00:00:10" {
		t.Fatalf("only the confirmed host is a device: %+v", devs)
	}
}

// Stale is about presence, never about other evidence: a host with a lease
// stays, and its address still comes from the stale entry.
func TestBuildDevicesStaleArpStillResolvesIPs(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"patio": {
			cfg:      RouterConfig{ID: "patio", Name: "Patio", Host: "192.0.2.2"},
			wireless: map[string]WirelessClient{"02:00:00:00:00:11": {SignalDbm: -55, Band: "5 GHz"}},
			arp:      map[string]string{"02:00:00:00:00:11": "192.0.2.61"},
			arpStale: map[string]bool{"02:00:00:00:00:11": true},
		},
	}
	devs := l.buildDevices(polled)
	if len(devs) != 1 || devs[0].IP != "192.0.2.61" {
		t.Fatalf("an associated client keeps its address: %+v", devs)
	}
}

// Two routers see the same host: confirmed anywhere is confirmed.
func TestBuildDevicesStaleOnOneRouterOnly(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"patio": {
			cfg:      RouterConfig{ID: "patio", Name: "Patio", Host: "192.0.2.2"},
			arp:      map[string]string{"02:00:00:00:00:10": "192.0.2.60"},
			arpStale: map[string]bool{"02:00:00:00:00:10": true},
		},
		"salon": {
			cfg: RouterConfig{ID: "salon", Name: "Salon", Host: "192.0.2.3"},
			arp: map[string]string{"02:00:00:00:00:10": "192.0.2.60"},
		},
	}
	if devs := l.buildDevices(polled); len(devs) != 1 {
		t.Fatalf("confirmed by one router is enough: %+v", devs)
	}
}

// A source that cannot report states (an older agent, or the /proc/net/arp
// fallback) marks nothing stale, and the whole table counts as presence.
func TestPolledFromAgentArpStale(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	pl := &probe.Payload{Router: "patio", Version: "0.1.0"}
	pl.Data.System = &probe.SystemData{SysInfo: &probe.SysInfo{Uptime: 100}}
	pl.Data.Arp = map[string]string{"02:00:00:00:00:10": "192.0.2.60"}
	if p := l.polledFromAgent(RouterConfig{ID: "patio"}, pl); len(p.arpStale) != 0 {
		t.Fatalf("an agent without arpStale marks nothing stale: %+v", p.arpStale)
	}

	pl.Data.ArpStale = []string{"02:00:00:00:00:10"}
	p := l.polledFromAgent(RouterConfig{ID: "patio"}, pl)
	if !p.arpStale["02:00:00:00:00:10"] {
		t.Fatalf("arpStale not propagated: %+v", p.arpStale)
	}
}
