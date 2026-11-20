package adapters

import "testing"

// #1150: con switches encadenados (gateway -> sw1 -> sw2) la boca de una
// unidad aprende las MACs de bridge de VARIAS unidades, y la primera MAC
// ordenada puede nombrar al switch LEJANO. El vecino LLDP anuncia al par
// DIRECTO: su chassis MAC (unidad conocida) o su sysName manda.
func TestEnrichEthPortRouterLinkPrefersLldpNeighbor(t *testing.T) {
	deps := portEnrichDeps{
		routerByMac: map[string]string{
			"AA:AA:AA:00:00:02": "sw2", // ordena ANTES que sw1: el bug la elegía
			"BB:BB:BB:00:00:01": "sw1",
		},
		portMacs: map[string][]string{
			"lan1": {"AA:AA:AA:00:00:02", "BB:BB:BB:00:00:01"},
		},
		lldp: []LldpNeighbor{
			{Port: "lan1", ChassisMac: "bb:bb:bb:00:00:01", Mgmt: "192.168.1.11"},
		},
	}
	port := EthPort{ID: "lan1", Label: "lan1", Up: true}

	got := enrichEthPort(port, "lan1", deps)
	if got.ConnectedTo != "sw1" {
		t.Fatalf("ConnectedTo = %q, want sw1 (vecino directo por LLDP)", got.ConnectedTo)
	}
	if got.PeerKind != "router-link-lldp" {
		t.Fatalf("PeerKind = %q, want router-link-lldp", got.PeerKind)
	}
	if got.DeviceMac != "BB:BB:BB:00:00:01" {
		t.Fatalf("DeviceMac = %q, want la MAC del chassis vecino", got.DeviceMac)
	}
	if got.Detail != "192.168.1.11 · LLDP" {
		t.Fatalf("Detail = %q, want mgmt-ip · LLDP", got.Detail)
	}
}

// #1976: sin LLDP y con dos unidades visibles en la misma boca no hay forma
// de saber cuál está enfrente. Antes se nombraba la primera MAC ordenada
// (sw2 por orden alfabético) y el mapa enseñaba un cable que no existe. Sin
// datos para desambiguar, la boca se queda sin par.
func TestEnrichEthPortRouterLinkAmbiguousStaysEmpty(t *testing.T) {
	deps := portEnrichDeps{
		routerByMac: map[string]string{
			"AA:AA:AA:00:00:02": "sw2",
			"BB:BB:BB:00:00:01": "sw1",
		},
		portMacs: map[string][]string{
			"lan1": {"AA:AA:AA:00:00:02", "BB:BB:BB:00:00:01"},
		},
	}
	port := EthPort{ID: "lan1", Label: "lan1", Up: true}

	got := enrichEthPort(port, "lan1", deps)
	if got.ConnectedTo != "" || got.PeerKind != "" || got.DeviceMac != "" {
		t.Fatalf("boca ambigua con par inventado: %+v", got)
	}
	if got.Detail != "" {
		t.Fatalf("Detail = %q, want empty", got.Detail)
	}
}

// #1976: con el FDB de las candidatas sí se puede desambiguar. sw1 aprende a
// test-gw en un puerto dedicado; sw2 lo aprende en el uplink que también
// lleva a sw1. El par directo es sw1.
func TestEnrichEthPortRouterLinkPicksDedicatedPort(t *testing.T) {
	const (
		self = "CC:CC:CC:00:00:01"
		sw1  = "BB:BB:BB:00:00:01"
		sw2  = "AA:AA:AA:00:00:02"
	)
	deps := portEnrichDeps{
		routerByMac: map[string]string{sw2: "sw2", sw1: "sw1"},
		portMacs:    map[string][]string{"eth0": {sw2, sw1}},
		unitByMac:   map[string]string{sw1: "sw1", sw2: "sw2"},
		selfBrMac:   self,
		fdbByUnit: map[string]map[string]string{
			// Puerto dedicado: solo test-gw.
			"sw1": {self: "port1"},
			// Uplink hacia sw1: lleva a test-gw y a sw1 a la vez.
			"sw2": {self: "uplink", sw1: "uplink"},
		},
	}
	got := enrichEthPort(EthPort{ID: "eth0", Label: "ETH 0", Up: true}, "eth0", deps)
	if got.ConnectedTo != "sw1" {
		t.Fatalf("ConnectedTo = %q, want sw1 (puerto dedicado)", got.ConnectedTo)
	}
	if got.DeviceMac != sw1 || got.PeerKind != "router-link" {
		t.Fatalf("par = %q %q, want %s router-link", got.ConnectedTo, got.PeerKind, sw1)
	}
}

// Si las dos candidatas lo aprenden en un puerto dedicado, el empate no se
// rompe adivinando.
func TestEnrichEthPortRouterLinkTieStaysEmpty(t *testing.T) {
	const (
		self = "CC:CC:CC:00:00:01"
		sw1  = "BB:BB:BB:00:00:01"
		sw2  = "AA:AA:AA:00:00:02"
	)
	deps := portEnrichDeps{
		routerByMac: map[string]string{sw2: "sw2", sw1: "sw1"},
		portMacs:    map[string][]string{"eth0": {sw2, sw1}},
		unitByMac:   map[string]string{sw1: "sw1", sw2: "sw2"},
		selfBrMac:   self,
		fdbByUnit: map[string]map[string]string{
			"sw1": {self: "port1"},
			"sw2": {self: "port2"},
		},
	}
	got := enrichEthPort(EthPort{ID: "eth0", Label: "ETH 0", Up: true}, "eth0", deps)
	if got.ConnectedTo != "" || got.PeerKind != "" {
		t.Fatalf("empate resuelto a ciegas: %+v", got)
	}
}

// El vecino LLDP no es una unidad conocida (chassis sin match): manda su
// sysName anunciado sobre la MAC ordenada.
func TestEnrichEthPortRouterLinkLldpUnknownChassis(t *testing.T) {
	deps := portEnrichDeps{
		routerByMac: map[string]string{
			"AA:AA:AA:00:00:02": "sw2",
		},
		portMacs: map[string][]string{
			"eth5": {"AA:AA:AA:00:00:02"},
		},
		lldp: []LldpNeighbor{
			{Port: "eth5", Chassis: "downstream-sw", Mgmt: "10.0.0.9"},
		},
	}
	port := EthPort{ID: "eth5", Label: "eth5", Up: true}

	got := enrichEthPort(port, "eth5", deps)
	if got.ConnectedTo != "downstream-sw" {
		t.Fatalf("ConnectedTo = %q, want downstream-sw (sysName LLDP)", got.ConnectedTo)
	}
	if got.Detail != "10.0.0.9 · LLDP" {
		t.Fatalf("Detail = %q, want mgmt-ip · LLDP", got.Detail)
	}
}
