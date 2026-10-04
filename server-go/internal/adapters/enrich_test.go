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

// Sin LLDP en la boca se conserva el comportamiento anterior: primera MAC
// de unidad ordenada (determinista entre polls, #1036).
func TestEnrichEthPortRouterLinkWithoutLldp(t *testing.T) {
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
	if got.ConnectedTo != "sw2" {
		t.Fatalf("ConnectedTo = %q, want sw2 (primera MAC ordenada)", got.ConnectedTo)
	}
	if got.PeerKind != "router-link" {
		t.Fatalf("PeerKind = %q, want router-link", got.PeerKind)
	}
	if got.Detail != "" {
		t.Fatalf("Detail = %q, want empty", got.Detail)
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
