package adapters

import (
	"reflect"
	"testing"
)

// Tests de la fase LLDP-first (issue #300): los vecinos LLDP son ground truth
// de los enlaces de infraestructura (router↔switch y switch↔switch), con el
// FDB como fallback cuando no hay lldpd.

const (
	macGS308E = "28:C6:8E:1D:90:44"
	macLGS352 = "AA:BB:CC:DD:EE:0A"
)

// distSummary es la forma canónica de comparación de un DistributionNode en
// los tests: solo los campos que la inferencia debe fijar.
type distSummary struct {
	id, kind, routerID, port, parent, mac, name, ip string
	macCount                                        int
}

func summarizeDists(dists []DistributionNode) []distSummary {
	if len(dists) == 0 {
		return nil
	}
	out := make([]distSummary, 0, len(dists))
	for _, d := range dists {
		out = append(out, distSummary{
			id: d.ID, kind: d.Kind, routerID: d.RouterID, port: d.Port,
			parent: d.Parent, mac: d.Mac, name: d.Name, ip: d.Ip,
			macCount: d.MacCount,
		})
	}
	return out
}

func TestInferTopologyLldpGroundTruth(t *testing.T) {
	tests := []struct {
		name    string
		polled  map[string]*routerPolled
		devices []Device
		want    []distSummary
	}{
		{
			// Gateway → SwitchA (GS308E) → SwitchB (LGS352C): cadena LLDP.
			// SwitchA es un router managed-switch sondeado (con lldp); su
			// distnode cuelga del gateway y el de SwitchB cuelga de SwitchA.
			// La MAC del propio SwitchA está excluida del FDB del gateway
			// (es su bridge MAC), así que el matching es por identidad.
			name: "cadena switch→switch",
			polled: map[string]*routerPolled{
				"flint2": {cfg: RouterConfig{ID: "flint2", IsGateway: true}, brMac: "94:83:C4:00:00:01",
					fdb: map[string]string{
						macGS308E: "lan3", "04:D4:C4:8B:30:A7": "lan3", "DC:A6:32:4F:77:02": "lan3",
					},
					lldp: []LldpNeighbor{{Port: "lan3", Chassis: "GS308E", ChassisMac: macGS308E, Mgmt: "192.168.8.13", Caps: []string{"Bridge"}, PortDesc: "ge5"}},
				},
				"swA": {cfg: RouterConfig{ID: "swA", Name: "GS308E", Type: "managed-switch", AgentOnly: true}, brMac: macGS308E,
					fdb: map[string]string{
						macLGS352: "ge5", "10:20:30:40:50:60": "ge5",
					},
					lldp: []LldpNeighbor{{Port: "ge5", Chassis: "LGS352C", ChassisMac: macLGS352, Mgmt: "192.168.8.20", Caps: []string{"Bridge"}, PortDesc: "ge1"}},
				},
			},
			devices: []Device{
				dev("04:D4:C4:8B:30:A7", "flint2", "cable"),
				dev("DC:A6:32:4F:77:02", "flint2", "cable"),
				dev(macLGS352, "swA", "cable"),
				dev("10:20:30:40:50:60", "swA", "cable"),
			},
			want: []distSummary{
				{id: "dist-flint2-lan3", kind: "managed", routerID: "flint2", port: "lan3", parent: "", mac: macGS308E, name: "GS308E", ip: "192.168.8.13", macCount: 2},
				{id: "dist-swA-ge5", kind: "managed", routerID: "swA", port: "ge5", parent: "dist-flint2-lan3", mac: macLGS352, name: "LGS352C", ip: "192.168.8.20", macCount: 2},
			},
		},
		{
			// Switch identificado SOLO por LLDP (chassis-MAC en el FDB, sin
			// router managed-switch en config): nodo managed sin Parent.
			name: "switch identificado por LLDP",
			polled: map[string]*routerPolled{
				"flint2": {cfg: RouterConfig{ID: "flint2", IsGateway: true}, brMac: "94:83:C4:00:00:01",
					fdb: map[string]string{
						macGS308E: "lan3", "04:D4:C4:8B:30:A7": "lan3",
					},
					lldp: []LldpNeighbor{{Port: "lan3", Chassis: "GS308E", ChassisMac: macGS308E, Mgmt: "192.168.8.13", Caps: []string{"Bridge"}, PortDesc: "ge5"}},
				},
			},
			devices: []Device{
				dev(macGS308E, "flint2", "cable"),
				dev("04:D4:C4:8B:30:A7", "flint2", "cable"),
			},
			want: []distSummary{
				{id: "dist-flint2-lan3", kind: "managed", routerID: "flint2", port: "lan3", parent: "", mac: macGS308E, name: "GS308E", ip: "192.168.8.13", macCount: 2},
			},
		},
		{
			// Un vecino LLDP que es un ROUTER regular (openwrt) no genera nodo
			// managed: es un enlace router↔router (uplink), no un switch.
			name: "router regular no genera nodo managed",
			polled: map[string]*routerPolled{
				"flint2": {cfg: RouterConfig{ID: "flint2", IsGateway: true}, brMac: "94:83:C4:00:00:01",
					fdb: map[string]string{
						"04:D4:C4:8B:30:A7": "lan1", "DC:A6:32:4F:77:02": "lan1",
					},
					lldp: []LldpNeighbor{{Port: "lan1", Chassis: "Living", ChassisMac: "94:83:C4:00:00:02", Mgmt: "192.168.8.2", Caps: []string{"Bridge", "Router"}, PortDesc: "lan3"}},
				},
				"living": {cfg: RouterConfig{ID: "living", Type: "openwrt"}, brMac: "94:83:C4:00:00:02", fdb: map[string]string{}},
			},
			devices: []Device{
				dev("04:D4:C4:8B:30:A7", "flint2", "cable"),
				dev("DC:A6:32:4F:77:02", "flint2", "cable"),
			},
			want: nil,
		},
		{
			// Sin LLDP (lldpd ausente): el FDB sigue siendo el fallback → nodo
			// "inferred" exactamente como antes.
			name: "FDB fallback sin LLDP",
			polled: map[string]*routerPolled{
				"flint2": {cfg: RouterConfig{ID: "flint2", IsGateway: true}, brMac: "94:83:C4:00:00:01",
					fdb: map[string]string{
						"78:2B:CB:AA:01:01": "lan3", "8C:EA:48:AA:02:02": "lan3",
					}},
			},
			devices: []Device{
				dev("78:2B:CB:AA:01:01", "flint2", "cable"),
				dev("8C:EA:48:AA:02:02", "flint2", "cable"),
			},
			want: []distSummary{
				{id: "dist-flint2-lan3", kind: "inferred", routerID: "flint2", port: "lan3", macCount: 2},
			},
		},
		{
			// Hipervisor con LLDP: la evidencia de hipervisor manda sobre la de
			// LLDP (no se convierte en nodo managed).
			name: "hipervisor prevalece sobre LLDP",
			polled: map[string]*routerPolled{
				"flint2": {cfg: RouterConfig{ID: "flint2", IsGateway: true}, brMac: "94:83:C4:00:00:01",
					fdb: map[string]string{
						"3C:52:82:10:20:30": "lan2", "BC:24:11:00:20:10": "lan2", "BC:24:11:00:20:11": "lan2",
					},
					lldp: []LldpNeighbor{{Port: "lan2", Chassis: "pve", ChassisMac: "3C:52:82:10:20:30", Caps: []string{"Station"}}},
				},
			},
			devices: []Device{
				dev("3C:52:82:10:20:30", "flint2", "cable"),
				dev("BC:24:11:00:20:10", "flint2", "cable"),
				dev("BC:24:11:00:20:11", "flint2", "cable"),
			},
			want: []distSummary{
				{id: "dist-flint2-lan2", kind: "hypervisor", routerID: "flint2", port: "lan2", macCount: 3},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, dists := inferTopology(tt.polled, tt.devices)
			got := summarizeDists(dists)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("dists:\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

// El distnode de un switch LLDP encadena a sus clientes FDB y sella al propio
// switch como managed-switch (D1/D2), también en una cadena switch→switch.
func TestInferTopologyLldpCadenaClientes(t *testing.T) {
	polled := map[string]*routerPolled{
		"flint2": {cfg: RouterConfig{ID: "flint2", IsGateway: true}, brMac: "94:83:C4:00:00:01",
			fdb: map[string]string{
				macGS308E: "lan3", "04:D4:C4:8B:30:A7": "lan3", "DC:A6:32:4F:77:02": "lan3",
			},
			lldp: []LldpNeighbor{{Port: "lan3", Chassis: "GS308E", ChassisMac: macGS308E, Mgmt: "192.168.8.13", Caps: []string{"Bridge"}, PortDesc: "ge5"}},
		},
		"swA": {cfg: RouterConfig{ID: "swA", Name: "GS308E", Type: "managed-switch", AgentOnly: true}, brMac: macGS308E,
			fdb: map[string]string{
				macLGS352: "ge5", "10:20:30:40:50:60": "ge5",
			},
			lldp: []LldpNeighbor{{Port: "ge5", Chassis: "LGS352C", ChassisMac: macLGS352, Mgmt: "192.168.8.20", Caps: []string{"Bridge"}, PortDesc: "ge1"}},
		},
	}
	devices := []Device{
		dev("04:D4:C4:8B:30:A7", "flint2", "cable"),
		dev("DC:A6:32:4F:77:02", "flint2", "cable"),
		dev(macLGS352, "swA", "cable"),
		dev("10:20:30:40:50:60", "swA", "cable"),
	}
	devices, dists := inferTopology(polled, devices)
	if len(dists) != 2 {
		t.Fatalf("distnodes: %+v", dists)
	}
	// Los PC tras el GS308E cuelgan del nodo del GS308E.
	if devices[0].AttachTo != "dist-flint2-lan3" || devices[1].AttachTo != "dist-flint2-lan3" {
		t.Fatalf("PCs tras GS308E: %+v %+v", devices[0], devices[1])
	}
	// El PC tras el LGS352C cuelga del nodo del LGS352C (segundo eslabón).
	if devices[3].AttachTo != "dist-swA-ge5" {
		t.Fatalf("PC tras LGS352C: %+v", devices[3])
	}
	// El LGS352C (switch B) queda sellado managed-switch; los clientes no.
	if devices[2].Infra != "managed-switch" {
		t.Fatalf("sellado del LGS352C: %+v", devices[2])
	}
	if devices[0].Infra != "" || devices[1].Infra != "" || devices[3].Infra != "" {
		t.Fatalf("clientes sin infra: %+v %+v %+v", devices[0], devices[1], devices[3])
	}
}

// BuildTopoSemantics: la cadena switch→switch emite un enlace dist distnode→
// distnode (From = Parent), además del router→raíz.
func TestTopoSemanticsCadenaLldp(t *testing.T) {
	routers := []Router{
		{ID: "flint2", Name: "Gateway", RoleBadge: "Principal", Status: "online"},
		{ID: "swA", Name: "GS308E", RoleBadge: "SW", Status: "online"},
	}
	dists := []DistributionNode{
		{ID: "dist-flint2-lan3", Kind: "managed", RouterID: "flint2", Port: "lan3", Mac: macGS308E},
		{ID: "dist-swA-ge5", Kind: "managed", RouterID: "swA", Port: "ge5", Parent: "dist-flint2-lan3", Mac: macLGS352},
	}
	devices := []Device{
		{RouterID: "flint2", Band: "cable", Port: "lan3", ID: "pc1", MAC: "04:D4:C4:8B:30:A7", AttachTo: "dist-flint2-lan3", Online: true},
		{RouterID: "swA", Band: "cable", Port: "ge5", ID: "pc2", MAC: "DC:A6:32:4F:77:02", AttachTo: "dist-swA-ge5", Online: true},
	}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, dists)

	var gotDist []TopoLink
	for _, l := range sem.Links {
		if l.Kind == "dist" {
			gotDist = append(gotDist, l)
		}
	}
	wantDist := []TopoLink{
		{From: "flint2", To: "dist-flint2-lan3", Kind: "dist", Port: "lan3"},
		{From: "dist-flint2-lan3", To: "dist-swA-ge5", Kind: "dist", Port: "ge5"},
	}
	if !reflect.DeepEqual(gotDist, wantDist) {
		t.Fatalf("dist links:\n got: %+v\nwant: %+v", gotDist, wantDist)
	}
}
