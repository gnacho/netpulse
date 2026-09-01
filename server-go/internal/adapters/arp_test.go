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
