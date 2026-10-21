// topology_child_speed_test.go - velocidad del enlace desde la boca del hijo
// (#1429).
package adapters

import "testing"

// El caso de crowedavid: enlace FDB sw1↔ap1 sin velocidad en el lado del
// padre (la boca del switch no reporta o no casa); el HIJO (OpenWrt) sabe la
// velocidad negociada de su propia boca de uplink (childPort de la evidencia
// FDB).
func TestFillUplinkSpeedsFromChild(t *testing.T) {
	polled := map[string]*routerPolled{
		"ap1": {ports: []EthPort{
			{ID: "lan1", Up: true, Speed: "1 Gbps"},
			{ID: "wan", Up: true, Speed: "1 Gbps"},
		}},
		"ap2": {ports: []EthPort{
			{ID: "lan1", Up: true, Speed: "2.5 Gbps"},
		}},
	}
	evidence := map[string]topoParent{
		"ap1": {parent: "sw1", port: "1/0/5", childPort: "wan"},
		"ap2": {parent: "sw1", port: "1/0/7", childPort: "lan1"},
	}
	links := []TopoLink{
		{From: "sw1", To: "ap1", Kind: "uplink", Port: "1/0/5"}, // el padre no aportó velocidad
		{From: "sw1", To: "ap2", Kind: "uplink", Port: "1/0/7"},
	}
	fillUplinkSpeedsFromChild(polled, evidence, links)
	if links[0].SpeedMbps != 1000 {
		t.Fatalf("sw1-ap1 desde la boca del hijo (wan 1G): %d, want 1000", links[0].SpeedMbps)
	}
	if links[1].SpeedMbps != 2500 {
		t.Fatalf("sw1-ap2 desde la boca del hijo (lan1 2.5G): %d, want 2500", links[1].SpeedMbps)
	}
}

// El lado del padre manda cuando ambos extremos reportan (son el mismo
// cable; el dato del lado que sondea el enlace es el directo).
func TestFillUplinkSpeedsFromChildKeepsParent(t *testing.T) {
	polled := map[string]*routerPolled{
		"ap1": {ports: []EthPort{{ID: "wan", Up: true, Speed: "100 Mbps"}}},
	}
	evidence := map[string]topoParent{"ap1": {parent: "sw1", port: "1/0/5", childPort: "wan"}}
	links := []TopoLink{{From: "sw1", To: "ap1", Kind: "uplink", Port: "1/0/5", SpeedMbps: 1000}}
	fillUplinkSpeedsFromChild(polled, evidence, links)
	if links[0].SpeedMbps != 1000 {
		t.Fatalf("el lado del padre no se pisa: %d, want 1000", links[0].SpeedMbps)
	}
}

// Sin childPort en la evidencia (uplink resuelto por otro camino) no hay
// nada que mirar en el hijo: la "—" honesta se respeta.
func TestFillUplinkSpeedsFromChildNoEvidence(t *testing.T) {
	polled := map[string]*routerPolled{
		"ap1": {ports: []EthPort{{ID: "wan", Up: true, Speed: "1 Gbps"}}},
	}
	links := []TopoLink{{From: "sw1", To: "ap1", Kind: "uplink", Port: "1/0/5"}}
	fillUplinkSpeedsFromChild(polled, nil, links)
	if links[0].SpeedMbps != 0 {
		t.Fatalf("sin evidencia no se inventa: %d, want 0", links[0].SpeedMbps)
	}
}

// Boca del hijo caída (Down): no afirma velocidad sobre un puerto que no
// está levantado, aunque la tabla guarde un valor.
func TestFillUplinkSpeedsFromChildDownPort(t *testing.T) {
	polled := map[string]*routerPolled{
		"ap1": {ports: []EthPort{{ID: "wan", Up: false, Speed: "1 Gbps"}}},
	}
	evidence := map[string]topoParent{"ap1": {parent: "sw1", port: "1/0/5", childPort: "wan"}}
	links := []TopoLink{{From: "sw1", To: "ap1", Kind: "uplink", Port: "1/0/5"}}
	fillUplinkSpeedsFromChild(polled, evidence, links)
	if links[0].SpeedMbps != 0 {
		t.Fatalf("boca del hijo caída: %d, want 0", links[0].SpeedMbps)
	}
}

// #1471: el uplink resuelto por LLDP también trae la boca del HIJO (el puerto
// local desde el que la unidad ve a su padre), así que la velocidad se puede
// leer de la tabla del propio hijo cuando el padre no la reporta. Antes de este
// cambio solo la evidencia FDB guardaba childPort y estos enlaces se quedaban
// sin velocidad.
func TestLldpUplinkSpeedFromChildPort(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {brMac: "AA:BB:CC:00:00:01", cfg: RouterConfig{ID: "gw", Name: "gateway"}},
		"ap1": {brMac: "AA:BB:CC:00:00:02", cfg: RouterConfig{ID: "ap1", Name: "ap1"},
			lldp:  []LldpNeighbor{{Port: "lan1", Chassis: "gateway", PortDesc: "lan2"}},
			ports: []EthPort{{ID: "lan1", Up: true, Speed: "2.5 Gbps"}}},
	}
	ev := fleetLldpEvidence(polled, "gw")
	if got := ev["ap1"]; got.parent != "gw" || got.port != "lan2" || got.childPort != "lan1" {
		t.Fatalf("ap1: %+v, want gw/lan2 con childPort lan1", got)
	}
	links := []TopoLink{{From: "gw", To: "ap1", Kind: "uplink", Port: "lan2"}}
	fillUplinkSpeedsFromChild(polled, ev, links)
	if links[0].SpeedMbps != 2500 {
		t.Fatalf("velocidad %d Mbps, want 2500 (tabla del hijo vía LLDP)", links[0].SpeedMbps)
	}
}

// #1471: la boca del padre sigue mandando cuando existe (mismo cable, dato
// directo del lado que sondea el enlace); la del hijo solo rellena el hueco.
func TestLldpUplinkSpeedParentWins(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {brMac: "AA:BB:CC:00:00:01", cfg: RouterConfig{ID: "gw", Name: "gateway"}},
		"ap1": {brMac: "AA:BB:CC:00:00:02", cfg: RouterConfig{ID: "ap1", Name: "ap1"},
			lldp:  []LldpNeighbor{{Port: "lan1", Chassis: "gateway", PortDesc: "lan2"}},
			ports: []EthPort{{ID: "lan1", Up: true, Speed: "2.5 Gbps"}}},
	}
	ev := fleetLldpEvidence(polled, "gw")
	links := []TopoLink{{From: "gw", To: "ap1", Kind: "uplink", Port: "lan2", SpeedMbps: 1000}}
	fillUplinkSpeedsFromChild(polled, ev, links)
	if links[0].SpeedMbps != 1000 {
		t.Fatalf("velocidad %d Mbps, want 1000 (la del padre no se pisa)", links[0].SpeedMbps)
	}
}
