package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

// #1791: el siguiente salto WAN del gateway no es un cliente LAN. Su MAC
// aparece en ARP y, sin este filtro, buildDevices la publica como un
// dispositivo desconocido.
func TestBuildDevicesAbsorbsDefaultRoute(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"gw": {
			cfg:     RouterConfig{ID: "gw", IsGateway: true},
			wanInfo: probe.WanInfo{Gateway: "192.168.2.1"},
			arp: map[string]string{
				"04:F4:1C:F3:46:65": "192.168.2.1",  // siguiente salto WAN
				"AA:BB:CC:00:00:09": "192.168.1.50", // cliente real
			},
			arpStale: map[string]bool{},
		},
	}
	devs := l.buildDevices(polled, "")
	if len(devs) != 1 || devs[0].MAC != "AA:BB:CC:00:00:09" {
		t.Fatalf("devices: %+v, want solo el cliente real", devs)
	}
}
