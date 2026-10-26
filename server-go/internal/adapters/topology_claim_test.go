package adapters

import "testing"

// #1417: flota con switches gestionados sondeados por SNMP como "external"
// (el montaje de #1137). El gateway los ve por LLDP en su puerto de uplink;
// como su Type no es "managed-switch", inferLldpLinks trata el enlace como
// router↔router y marca el puerto como resuelto SIN crear distnode ni anclar
// a los clientes cableados del puerto. La inferencia de hojas #1325 tampoco
// cubre estos switches (filtra Type=="managed-switch"). Resultado: los
// clientes cableados del switch quedan sin AttachTo y el mapa los cuelga del
// gateway. claimSwitchFdbClients reclama para cada switch de flota los
// clientes cableados (atribuidos al gateway) que SU FDB aprendió en un
// puerto que no es su root port.

const (
	claimGwMac   = "AA:BB:CC:00:00:01"
	claimSw1Mac  = "22:22:22:22:22:01"
	claimSw2Mac  = "22:22:22:22:22:02"
	claimSw16Mac = "22:22:22:22:22:16"
)

func claimFlotaPolled() map[string]*routerPolled {
	return map[string]*routerPolled{
		"gateway": {
			cfg:   RouterConfig{ID: "gateway", Type: "openwrt", IsGateway: true},
			brMac: claimGwMac,
			fdb: map[string]string{
				"11:11:11:11:11:01": "lan1", // sonos1 (detrás de sw1)
				"11:11:11:11:11:02": "lan1", // sonos2 (detrás de sw1)
				"11:11:11:11:11:03": "lan1", // sonos3 (detrás de sw1)
				"11:11:11:11:11:04": "lan1", // sonos4 (detrás de sw2)
				"11:11:11:11:11:09": "lan4", // cableado directo del gateway
				claimSw1Mac:         "lan1",
				claimSw2Mac:         "lan1",
			},
			lldp: []LldpNeighbor{{Port: "lan1", ChassisMac: claimSw1Mac, Chassis: "sw1", Mgmt: "192.168.1.2"}},
		},
		"sw1": {
			cfg:   RouterConfig{ID: "sw1", Type: "external", SnmpEnabled: true, Host: "192.168.1.2"},
			brMac: claimSw1Mac,
			fdb: map[string]string{
				claimGwMac:          "Port 1", // root port (hacia el gateway)
				"11:11:11:11:11:01": "Port 5",
				"11:11:11:11:11:02": "Port 5",
				"11:11:11:11:11:03": "Port 6",
				"11:11:11:11:11:04": "Port 2", // trunk hacia sw2
				claimSw2Mac:         "Port 2",
			},
		},
		"sw2": {
			cfg:   RouterConfig{ID: "sw2", Type: "external", SnmpEnabled: true, Host: "192.168.1.3"},
			brMac: claimSw2Mac,
			fdb: map[string]string{
				claimGwMac:          "Port 1", // root port
				"11:11:11:11:11:04": "Port 3", // acceso
			},
		},
	}
}

// El caso de crowedavid: los Sonos cableados tras sw1/sw2 (externos SNMP)
// deben quedar anclados al nodo del switch que los aprendió en puerto de
// acceso, con el puerto físico del switch, y no colgar del gateway.
func TestClaim1417SonosDetrasDeSwitchSNMP(t *testing.T) {
	polled := claimFlotaPolled()
	devices := []Device{
		dev("11:11:11:11:11:01", "gateway", "cable"),
		dev("11:11:11:11:11:02", "gateway", "cable"),
		dev("11:11:11:11:11:03", "gateway", "cable"),
		dev("11:11:11:11:11:04", "gateway", "cable"),
		dev("11:11:11:11:11:09", "gateway", "cable"),
	}
	devices, dists := inferTopology(polled, devices)
	for i, d := range devices[:4] {
		if d.AttachTo != "" {
			t.Fatalf("prepara: sonos %d ya anclado a %q (fixture sin el bug)", i, d.AttachTo)
		}
	}
	if len(dists) != 0 {
		t.Fatalf("prepara: distnodes inesperados: %+v", dists)
	}
	gwID := "gateway"
	claimSwitchFdbClients(polled, devices, dists, gwID,
		fleetFdbEvidence(polled, gwID, nil, nil), fleetLldpEvidence(polled, gwID), nil)

	// sonos1/2: acceso de sw1 por "Port 5"; sonos3: "Port 6".
	for i, wantPort := range []string{"Port 5", "Port 5", "Port 6"} {
		if devices[i].AttachTo != "sw1" || devices[i].Port != wantPort {
			t.Fatalf("sonos%d: got (%q, %q), want (sw1, %q)", i+1, devices[i].AttachTo, devices[i].Port, wantPort)
		}
	}
	// sonos4 lo ven sw1 (trunk) y sw2 (acceso): gana el más profundo.
	if devices[3].AttachTo != "sw2" || devices[3].Port != "Port 3" {
		t.Fatalf("sonos4: got (%q, %q), want (sw2, Port 3)", devices[3].AttachTo, devices[3].Port)
	}
	// El cableado directo del gateway no se toca.
	if devices[4].AttachTo != "" || devices[4].Port != "lan4" {
		t.Fatalf("lan4: got (%q, %q), want (\"\", lan4)", devices[4].AttachTo, devices[4].Port)
	}
}

// El caso de casa (#1374): el uplink de switch16 y PC/Marantz (switch tonto)
// comparten el puerto lan1 del gateway. La FDB de switch16 ve a PC/Marantz en
// lan7, su ROOT port (tráfico upstream): no debe reclamarlos. Un equipo en un
// puerto de acceso (beelink, lan5) sí se reclama aunque hoy cuelgue del blob
// inferido del gateway.
func TestClaim1417RootPortNoReclama(t *testing.T) {
	polled := map[string]*routerPolled{
		"flint2": {
			cfg:   RouterConfig{ID: "flint2", Type: "glinet", IsGateway: true},
			brMac: claimGwMac,
			fdb: map[string]string{
				"11:11:11:11:11:01": "lan1", // PC (switch tonto)
				"11:11:11:11:11:02": "lan1", // marantz
				"11:11:11:11:11:05": "lan1", // beelink (detrás de switch16)
				claimSw16Mac:        "lan1",
			},
		},
		"switch16": {
			cfg:   RouterConfig{ID: "switch16", Type: "managed-switch", AgentOnly: true},
			brMac: claimSw16Mac,
			fdb: map[string]string{
				claimGwMac:          "lan7", // root port de switch16
				"11:11:11:11:11:01": "lan7", // PC: upstream, NO reclamar
				"11:11:11:11:11:02": "lan7", // marantz: upstream, NO reclamar
				"11:11:11:11:11:05": "lan5", // beelink: acceso, SÍ
			},
		},
	}
	devices := []Device{
		dev("11:11:11:11:11:01", "flint2", "cable"),
		dev("11:11:11:11:11:02", "flint2", "cable"),
		dev("11:11:11:11:11:05", "flint2", "cable"),
	}
	devices, dists := inferTopology(polled, devices)
	before := []string{devices[0].AttachTo, devices[1].AttachTo}
	gwID := "flint2"
	claimSwitchFdbClients(polled, devices, dists, gwID,
		fleetFdbEvidence(polled, gwID, nil, nil), fleetLldpEvidence(polled, gwID), nil)

	if devices[0].AttachTo != before[0] || devices[1].AttachTo != before[1] {
		t.Fatalf("PC/Marantz (root port upstream) no deben moverse: antes (%q, %q), después (%q, %q)",
			before[0], before[1], devices[0].AttachTo, devices[1].AttachTo)
	}
	if devices[2].AttachTo != "switch16" || devices[2].Port != "lan5" {
		t.Fatalf("beelink: got (%q, %q), want (switch16, lan5)", devices[2].AttachTo, devices[2].Port)
	}
}

// Guardas del reclamo: un cliente wifi (aunque su RouterID sea el gateway) no
// se reclama; un device ya colgado de un device-hub (attachTo que no es
// distnode) se conserva; un switch sin la MAC del gateway en su FDB (root
// port desconocido) no reclama nada.
func TestClaim1417Guardas(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {
			cfg:   RouterConfig{ID: "gw", Type: "openwrt", IsGateway: true},
			brMac: claimGwMac,
		},
		"sw1": {
			cfg:   RouterConfig{ID: "sw1", Type: "external", SnmpEnabled: true},
			brMac: claimSw1Mac,
			fdb: map[string]string{
				// SIN la MAC del gateway: root port desconocido → no reclama.
				"11:11:11:11:11:01": "Port 5",
				"11:11:11:11:11:02": "Port 5",
				"11:11:11:11:11:03": "Port 5",
			},
		},
	}
	devices := []Device{
		dev("11:11:11:11:11:01", "gw", "cable"),
		dev("11:11:11:11:11:02", "gw", "5GHz"),
		{ID: "d-hub", MAC: "11:11:11:11:11:03", RouterID: "gw", Band: "cable", Online: true, AttachTo: "d-citadel-01"},
	}
	dists := []DistributionNode{}
	gwID := "gw"
	claimSwitchFdbClients(polled, devices, dists, gwID,
		fleetFdbEvidence(polled, gwID, nil, nil), fleetLldpEvidence(polled, gwID), nil)

	if devices[0].AttachTo != "" || devices[1].AttachTo != "" {
		t.Fatalf("sin root port no hay reclamo: got (%q, %q)", devices[0].AttachTo, devices[1].AttachTo)
	}
	if devices[2].AttachTo != "d-citadel-01" {
		t.Fatalf("el attach al device-hub debe conservarse, got %q", devices[2].AttachTo)
	}
}

// Un puerto de sw1 con hoja inferida #1325 ya creada (switch tonto con >=2
// clientes bajo un switch Type "managed-switch"): el claim llena la hoja
// (attach al distnode) en vez de rodearla colgando los clientes del switch.
func TestClaim1417LlenaHoja1325(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {
			cfg:   RouterConfig{ID: "gw", Type: "openwrt", IsGateway: true},
			brMac: claimGwMac,
			fdb: map[string]string{
				"11:11:11:11:11:01": "lan1", // tras el switch tonto de sw1
				"11:11:11:11:11:02": "lan1",
				claimSw1Mac:         "lan1",
			},
		},
		"sw1": {
			cfg:   RouterConfig{ID: "sw1", Type: "managed-switch"},
			brMac: claimSw1Mac,
			fdb: map[string]string{
				claimGwMac:          "Port 1",
				"11:11:11:11:11:01": "Port 5", // switch tonto (hoja #1325)
				"11:11:11:11:11:02": "Port 5",
			},
		},
	}
	devices := []Device{
		dev("11:11:11:11:11:01", "gw", "cable"),
		dev("11:11:11:11:11:02", "gw", "cable"),
	}
	hoja := DistributionNode{ID: "dist-sw1-Port 5", Kind: "inferred", RouterID: "sw1", Port: "Port 5", MacCount: 2}
	dists := []DistributionNode{hoja}
	gwID := "gw"
	claimSwitchFdbClients(polled, devices, dists, gwID,
		fleetFdbEvidence(polled, gwID, nil, nil), fleetLldpEvidence(polled, gwID), nil)

	for i, d := range devices {
		if d.AttachTo != "dist-sw1-Port 5" {
			t.Fatalf("device %d: got %q, want la hoja dist-sw1-Port 5", i, d.AttachTo)
		}
	}
}
