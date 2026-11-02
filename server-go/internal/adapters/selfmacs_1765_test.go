package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

// #1765: el propio equipo responde por más MACs que la del router guardado en
// la flota. El agente las reporta y el servidor las absorbe: sin esto, la MAC
// del puerto CPU de un switch o la de una VLAN se aprende en la FDB de un
// vecino y sale como cliente desconocido.
func TestBuildDevicesAbsorbsAgentSelfMacs(t *testing.T) {
	cfg := RouterConfig{ID: "gateway", Host: "192.0.2.1", Name: "openwrt", IsGateway: true, AgentOnly: true}
	l := NewLive(nil, nil, []RouterConfig{cfg}, nil)
	payload := &probe.Payload{
		Router: "gateway", Ts: 1, Version: "2.51.0",
		Data: probe.PayloadData{
			System: &probe.SystemData{
				BridgeMAC: "94:83:C4:00:00:01",
				IfaceMACs: []string{"94:83:C4:00:00:01", "04:F4:1C:F3:46:66"},
			},
			FDB: &probe.FDBData{MACs: map[string]string{
				"04:F4:1C:F3:46:66": "lan2",
				"AA:BB:CC:00:00:09": "lan2",
			}},
		},
	}
	devices := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, payload)}, "")
	var real, self bool
	for _, d := range devices {
		switch d.MAC {
		case "04:F4:1C:F3:46:66":
			self = true
		case "AA:BB:CC:00:00:09":
			real = true
		}
	}
	if self {
		t.Error("la MAC propia de la interfaz salió como cliente")
	}
	if !real {
		t.Errorf("el cliente real no aparece (%d devices)", len(devices))
	}
}
