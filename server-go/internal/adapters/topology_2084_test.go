// topology_2084_test.go - un switch sondeado no se dibuja dos veces y un
// puerto con hipervisor emite un solo nodo.
//
// El caso medido por el reportero: dos switches Netgear sondeados por SNMP,
// con un access point entre dos puertos del mismo numero (g12 en ambos). El
// switch B se aprende por el LAG (Fiber Backplane), pero sus tramas LLDP
// llegan a A por g12 y crean alli un segundo nodo del mismo equipo. Ademas,
// la MAC que justifica ese nodo (la secundaria que emite LLDP) oscila entre
// los dos puertos con cada trama, asi que el nodo vive sobre una entrada FDB
// que no representa trafico de hosts. B's g12 estaba en RSTP Discarding.
package adapters

import (
	"strings"
	"testing"
)

// El enlace real ya esta aprendido en otro puerto: el LLDP de este puerto no
// materializa un segundo nodo del mismo switch.
func TestInferTopologyNoDuplicaSwitchAprendidoPorOtroPuerto(t *testing.T) {
	const (
		macB    = "A0:40:A0:5E:FE:7A" // bridge MAC de B (routers.mac)
		macBLl  = "A0:40:A0:5E:FE:7C" // MAC secundaria que emite LLDP
		macAP   = "A0:04:60:2D:31:94"
		macCli1 = "CE:27:92:AE:AA:9F"
	)
	polled := map[string]*routerPolled{
		"nsw-off-a3": {
			cfg:   RouterConfig{ID: "nsw-off-a3", Name: "NSW-OFF-A3", Host: "192.168.2.11", Type: "managed-switch"},
			brMac: "A0:40:A0:5E:FE:7B",
			fdb: map[string]string{
				macB:    "Fiber Backplane", // el enlace real
				macAP:   "01-NightHawk X10",
				macCli1: "01-NightHawk X10",
			},
			lldp: []LldpNeighbor{{
				Port: "01-NightHawk X10", Chassis: "NSW-OFF-7A", ChassisMac: macBLl,
				Mgmt: "192.168.2.12", Caps: []string{"Bridge"}, PortDesc: "01-NightHawk X10",
			}},
		},
		"nsw-off-7a": {
			cfg:   RouterConfig{ID: "nsw-off-7a", Name: "NSW-OFF-7A", Host: "192.168.2.12", Type: "managed-switch"},
			brMac: macB,
			fdb: map[string]string{
				"94:83:C4:00:00:01": "Fiber Backplane",
				"10:20:30:40:50:60": "Fiber Backplane",
			},
		},
	}
	devices := []Device{
		dev(macAP, "nsw-off-a3", "cable"),
		dev(macCli1, "nsw-off-a3", "cable"),
	}
	_, dists := inferTopology(polled, devices)
	for _, d := range dists {
		if d.Port == "01-NightHawk X10" && d.RouterID == "nsw-off-a3" {
			t.Fatalf("el switch ya aprendido por el LAG no debe colgar de este puerto: %+v", d)
		}
		if strings.EqualFold(d.Name, "NSW-OFF-7A") {
			t.Fatalf("NSW-OFF-7A ya es una unidad sondeada, no un nodo de distribucion: %+v", d)
		}
	}
}

// La cadena switch->switch legitima (#300) sigue emitiendo el nodo: aqui el
// padre aprende al hijo por el MISMO puerto que trae el LLDP.
func TestInferTopologyMantieneCadenaCuandoElPuertoEsElEnlace(t *testing.T) {
	const macChild = "28:C6:8E:1D:90:44"
	polled := map[string]*routerPolled{
		"flint2": {
			cfg:   RouterConfig{ID: "flint2", IsGateway: true},
			brMac: "94:83:C4:00:00:01",
			fdb: map[string]string{
				macChild:            "lan3",
				"04:D4:C4:8B:30:A7": "lan3",
				"DC:A6:32:4F:77:02": "lan3",
			},
			lldp: []LldpNeighbor{{
				Port: "lan3", Chassis: "GS308E", ChassisMac: macChild,
				Mgmt: "192.168.8.13", Caps: []string{"Bridge"}, PortDesc: "ge5",
			}},
		},
		"swA": {
			cfg:   RouterConfig{ID: "swA", Name: "GS308E", Type: "managed-switch", AgentOnly: true},
			brMac: macChild,
			fdb:   map[string]string{"AA:BB:CC:DD:EE:0A": "ge5"},
		},
	}
	devices := []Device{
		dev("04:D4:C4:8B:30:A7", "flint2", "cable"),
		dev("DC:A6:32:4F:77:02", "flint2", "cable"),
	}
	_, dists := inferTopology(polled, devices)
	found := false
	for _, d := range dists {
		if d.Kind == "managed" && d.Port == "lan3" && d.RouterID == "flint2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("la cadena switch->switch legitima debe conservarse: %+v", dists)
	}
}

// Un puerto con hipervisor no recibe ademas un nodo inferred con el mismo id.
func TestInferTopologyPuertoHipervisorEmiteUnSoloNodo(t *testing.T) {
	const (
		gwMac  = "94:83:C4:00:00:01"
		hostMu = "A0:11:22:33:44:55"
		vmMac  = "00:50:56:AA:BB:CC" // OUI VMware
	)
	polled := map[string]*routerPolled{
		"gw": {
			cfg:   RouterConfig{ID: "gw", IsGateway: true},
			brMac: gwMac,
			fdb:   map[string]string{"10:20:30:40:50:99": "lan1"},
		},
		"swB": {
			cfg:   RouterConfig{ID: "swB", Name: "NSW-OFF-7A", Type: "external", SnmpEnabled: true},
			brMac: "A0:40:A0:5E:FE:7A",
			fdb: map[string]string{
				gwMac:  "ge1", // root port (uplink)
				hostMu: "ge2",
				vmMac:  "ge2",
			},
		},
	}
	devices := []Device{
		dev(hostMu, "swB", "cable"),
		dev(vmMac, "swB", "cable"),
	}
	_, dists := inferTopology(polled, devices)

	seen := map[string]int{}
	for _, d := range dists {
		seen[d.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("id repetido %q (%d veces): %+v", id, n, dists)
		}
	}
	var node *DistributionNode
	for i := range dists {
		if dists[i].ID == "dist-swB-ge2" {
			node = &dists[i]
		}
	}
	if node == nil {
		t.Fatalf("falta el nodo del puerto con hipervisor: %+v", dists)
	}
	if node.Kind != "hypervisor" {
		t.Fatalf("manda la evidencia mas fuerte, no %q: %+v", node.Kind, node)
	}
}

// El dedup final deja una entrada por id y gana la evidencia mas fuerte.
func TestDedupeDistributionNodesKeepsStrongest(t *testing.T) {
	in := []DistributionNode{
		{ID: "dist-a", Kind: "inferred"},
		{ID: "dist-b", Kind: "managed"},
		{ID: "dist-a", Kind: "hypervisor"},
		{ID: "dist-b", Kind: "inferred"},
		{ID: "dist-c", Kind: "inferred"},
	}
	out := dedupeDistributionNodes(in)
	if len(out) != 3 {
		t.Fatalf("quedan %d entradas: %+v", len(out), out)
	}
	got := map[string]string{}
	for _, d := range out {
		got[d.ID] = d.Kind
	}
	if got["dist-a"] != "hypervisor" || got["dist-b"] != "managed" || got["dist-c"] != "inferred" {
		t.Fatalf("precedencia: %+v", got)
	}
}
