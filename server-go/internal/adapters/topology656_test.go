// topology656_test.go — Issue #656: reconciliación del FDB de satélites.
//
// Un dumb AP/bridge comparte el segmento L2 con el gateway, así que el
// gateway también ve (en su FDB) las MACs de los clientes cableados del
// satélite. Antes, ese solape descartaba al satélite y los clientes se
// atribuían al router principal. El fix atribuye al satélite las MACs que
// aprende en sus bocas locales NO-uplink, preservando la exclusión del
// uplink (infraPorts) y la fuente específica de los routers agent-only.
package adapters

import (
	"testing"
)

// busca un Device por MAC; falla si no existe.
func mustDevice(t *testing.T, devs []Device, mac string) Device {
	t.Helper()
	for _, d := range devs {
		if d.MAC == mac {
			return d
		}
	}
	t.Fatalf("dispositivo %s no encontrado en %+v", mac, devs)
	return Device{}
}

// TestBuildDevicesDumbAPWiredClient: cliente Ethernet cableado a un dumb AP
// (mismo L2 que el gateway) se atribuye al SATÉLITE, no al router principal.
func TestBuildDevicesDumbAPWiredClient(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Name: "Main", Host: "192.168.1.1", IsGateway: true},
	}, nil)
	polled := map[string]*routerPolled{
		"gateway": {
			cfg:   RouterConfig{ID: "gateway", Name: "Main", IsGateway: true},
			brMac: "GG:GG:GG:GG:GG:GG",
			fdb: map[string]string{
				"GG:GG:GG:GG:GG:GG": "lan1", // bridge del gateway (excluida)
				"11:11:11:11:11:11": "lan1", // cliente directo del gateway
				"66:66:66:66:66:66": "lan2", // cliente del dumb AP (también vedo por el gateway)
			},
		},
		"dumbap": {
			cfg:   RouterConfig{ID: "dumbap", Name: "Luizjana2", Host: "192.168.1.2"},
			brMac: "AA:AA:AA:AA:AA:AA",
			fdb: map[string]string{
				"66:66:66:66:66:66": "lan1", // boca LOCAL del dumb AP (no-infra)
				"GG:GG:GG:GG:GG:GG": "lan2", // MAC bridge del gateway → uplink
				"AA:AA:AA:AA:AA:AA": "lan2", // bridge propio → uplink
				"11:11:11:11:11:11": "lan2", // cliente del gateway, tránsito por el uplink
			},
		},
	}
	devs := l.buildDevices(polled)
	if len(devs) != 2 {
		t.Fatalf("esperado 2 dispositivos, got %d: %+v", len(devs), devs)
	}
	// El cliente cableado al dumb AP debe atribuirse al dumb AP.
	if d := mustDevice(t, devs, "66:66:66:66:66:66"); d.RouterID != "dumbap" {
		t.Fatalf("cliente del dumb AP: RouterID esperado dumbap, got %q", d.RouterID)
	}
	// El cliente directo del gateway sigue en el gateway (aparece en el uplink
	// del dumb AP, que es infra → tránsito, no se lo apropia).
	if d := mustDevice(t, devs, "11:11:11:11:11:11"); d.RouterID != "gateway" {
		t.Fatalf("cliente del gateway: RouterID esperado gateway, got %q", d.RouterID)
	}
}

// TestBuildDevicesOwnBridgeExcluded: la MAC de bridge del propio satélite y la
// del gateway no aparecen nunca como dispositivos.
func TestBuildDevicesOwnBridgeExcluded(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Name: "Main", IsGateway: true},
	}, nil)
	polled := map[string]*routerPolled{
		"gateway": {cfg: RouterConfig{ID: "gateway", IsGateway: true}, brMac: "GG:GG:GG:GG:GG:GG",
			fdb: map[string]string{"GG:GG:GG:GG:GG:GG": "lan1"}},
		"dumbap": {cfg: RouterConfig{ID: "dumbap"}, brMac: "AA:AA:AA:AA:AA:AA",
			fdb: map[string]string{"GG:GG:GG:GG:GG:GG": "lan2", "AA:AA:AA:AA:AA:AA": "lan2"}},
	}
	devs := l.buildDevices(polled)
	if len(devs) != 0 {
		t.Fatalf("esperado 0 dispositivos (todas las MACs son de bridge), got %d: %+v", len(devs), devs)
	}
}

// TestBuildDevicesAgentOnlySpecific: un switch agent-only sigue siendo la
// fuente más específica de sus clientes, aunque el gateway también los vea
// (excepción preservada; #291).
func TestBuildDevicesAgentOnlySpecific(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Name: "Main", IsGateway: true},
	}, nil)
	polled := map[string]*routerPolled{
		"gateway": {
			cfg:   RouterConfig{ID: "gateway", IsGateway: true},
			brMac: "GG:GG:GG:GG:GG:GG",
			fdb: map[string]string{
				"GG:GG:GG:GG:GG:GG": "lan1",
				"77:77:77:77:77:77": "lan2", // cliente del switch (visto también por el gateway)
			},
		},
		"switch16": {
			cfg:       RouterConfig{ID: "switch16", Name: "SW-16", Host: "192.168.1.6", AgentOnly: true},
			brMac:     "BB:BB:BB:BB:BB:BB",
			agentKind: "external",
			fdb: map[string]string{
				"77:77:77:77:77:77": "lan3", // boca local del switch
				"BB:BB:BB:BB:BB:BB": "lan1", // bridge propio → uplink
				"GG:GG:GG:GG:GG:GG": "lan1", // MAC bridge del gateway → uplink
			},
		},
	}
	devs := l.buildDevices(polled)
	if len(devs) != 1 {
		t.Fatalf("esperado 1 dispositivo, got %d: %+v", len(devs), devs)
	}
	if d := mustDevice(t, devs, "77:77:77:77:77:77"); d.RouterID != "switch16" {
		t.Fatalf("cliente del switch agent-only: RouterID esperado switch16, got %q", d.RouterID)
	}
}

// TestBuildDevicesStableOrder: buildDevices devuelve los dispositivos en orden
// determinista (por MAC), para que los anillos de la topología no salten al
// refrescar (#656).
func TestBuildDevicesStableOrder(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "gw", Name: "GW", IsGateway: true}}, nil)
	polled := map[string]*routerPolled{
		"gw": {
			cfg:   RouterConfig{ID: "gw", Name: "GW", IsGateway: true},
			brMac: "GG:GG:GG:GG:GG:GG",
			fdb: map[string]string{
				"GG:GG:GG:GG:GG:GG": "lan1",
				"CC:CC:CC:CC:CC:CC": "lan1",
				"AA:AA:AA:AA:AA:AA": "lan1",
				"BB:BB:BB:BB:BB:BB": "lan1",
			},
		},
	}
	devs := l.buildDevices(polled)
	if len(devs) != 3 {
		t.Fatalf("esperado 3 dispositivos, got %d: %+v", len(devs), devs)
	}
	macs := []string{devs[0].MAC, devs[1].MAC, devs[2].MAC}
	want := []string{"AA:AA:AA:AA:AA:AA", "BB:BB:BB:BB:BB:BB", "CC:CC:CC:CC:CC:CC"}
	for i := range want {
		if macs[i] != want[i] {
			t.Fatalf("orden inestable: got %v, want %v", macs, want)
		}
	}
}
