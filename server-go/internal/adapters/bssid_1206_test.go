// bssid_1206_test.go — #1206: los BSSID propios de la flota (reportados por
// el agente, #1087) son infraestructura y no salen como clientes en la tabla
// de dispositivos. Antes de #1145 ni llegaban a la lista; con la retención
// empezaron a verse.
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestBuildDevicesExcludesOwnBssids(t *testing.T) {
	reg := NewAgentRegistry(0)
	reg.Ingest(&probe.Payload{
		Router:  "patio",
		Version: "3.0.10",
		Data: probe.PayloadData{
			Wireless: &probe.WirelessData{
				OwnBssids: []probe.OwnBSSID{{Iface: "wlan0", BSSID: "AA:BB:CC:00:00:01"}},
			},
		},
	})
	l := NewLive(nil, nil, nil, nil)
	l.SetAgents(reg)
	polled := map[string]*routerPolled{
		"patio": {
			cfg: RouterConfig{ID: "patio", Name: "Patio", Host: "192.168.1.2"},
			wireless: map[string]WirelessClient{
				"AA:BB:CC:00:00:01": {SignalDbm: -40, Band: "5 GHz"}, // el BSSID propio del AP
				"AA:BB:CC:DD:EE:FF": {SignalDbm: -55, Band: "5 GHz"}, // cliente real
			},
		},
	}
	devs := l.buildDevices(polled)
	for _, d := range devs {
		if d.MAC == "AA:BB:CC:00:00:01" {
			t.Fatalf("el BSSID propio salió como cliente: %+v", devs)
		}
	}
	found := false
	for _, d := range devs {
		if d.MAC == "AA:BB:CC:DD:EE:FF" {
			found = true
		}
	}
	if !found {
		t.Fatalf("el cliente real desapareció: %+v", devs)
	}
}
