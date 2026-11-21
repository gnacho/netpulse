package httpapi

import (
	"reflect"
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestNormalizePayloadMACs(t *testing.T) {
	const (
		lo1 = "aa:bb:cc:dd:ee:01"
		hy2 = "aa-bb-cc-dd-ee-02"
		bn3 = "aabbccddee03"
		c1  = "AA:BB:CC:DD:EE:01"
		c2  = "AA:BB:CC:DD:EE:02"
		c3  = "AA:BB:CC:DD:EE:03"
	)
	p := &probe.Payload{Data: probe.PayloadData{
		System: &probe.SystemData{BridgeMAC: lo1, IfaceMACs: []string{hy2, bn3}},
		Wireless: &probe.WirelessData{
			Clients:   map[string]probe.WirelessClient{lo1: {}},
			Radios:    []probe.Radio{{BSSID: hy2}},
			OwnBssids: []probe.OwnBSSID{{BSSID: bn3}},
			Scans:     []probe.ScanResult{{BSSID: lo1}},
		},
		DHCP: &probe.DHCPData{
			Leases:       []probe.DhcpLease{{MAC: lo1}},
			GlClients:    []probe.DhcpLease{{MAC: hy2}},
			Reservations: []probe.DhcpReservation{{MAC: bn3}},
		},
		FDB: &probe.FDBData{
			MACs:         map[string]string{lo1: "lan1", "garbage": "lan2"},
			WirelessSide: map[string]string{hy2: "5 GHz"},
			VLANs:        map[string]int{bn3: 20},
		},
		Arp:      map[string]string{lo1: "192.0.2.1"},
		ArpDevs:  map[string]string{hy2: "br-lan"},
		ArpStale: []string{bn3},
		Lldp: &probe.LldpData{Neighbors: []probe.LldpNeighbor{
			{ChassisMac: lo1}, {ChassisMac: "sw-core"},
		}},
		Dawn: &probe.DawnData{SSIDs: map[string]probe.DawnSSID{"casa": {
			APs:     []probe.DawnAP{{BSSID: lo1}},
			Clients: map[string]probe.DawnClient{hy2: {BSSID: bn3}},
		}}},
		Usteer: &probe.UsteerData{SSIDs: map[string]probe.UsteerSSID{"casa": {
			APs:     []probe.UsteerAP{{BSSID: hy2}},
			Clients: map[string]probe.UsteerClient{bn3: {}},
		}}},
		ClientBw:  &probe.ClientBwData{Available: true, Hosts: map[string]probe.NlbwCounter{lo1: {RxBytes: 1}}},
		Discovery: &probe.DiscoveryData{RandomMACs: []string{hy2}},
	}}

	normalizePayloadMACs(p)
	d := p.Data

	eq := func(field, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s: esperaba %q, obtuve %q", field, want, got)
		}
	}
	hasKey := func(field string, m any, want string) {
		t.Helper()
		if !reflect.ValueOf(m).MapIndex(reflect.ValueOf(want)).IsValid() {
			t.Errorf("%s: esperaba la clave canónica %q en %v", field, want, m)
		}
	}

	eq("System.BridgeMAC", d.System.BridgeMAC, c1)
	eq("System.IfaceMACs[0]", d.System.IfaceMACs[0], c2)
	eq("System.IfaceMACs[1]", d.System.IfaceMACs[1], c3)
	hasKey("Wireless.Clients", d.Wireless.Clients, c1)
	eq("Radios", d.Wireless.Radios[0].BSSID, c2)
	eq("OwnBssids", d.Wireless.OwnBssids[0].BSSID, c3)
	eq("Scans", d.Wireless.Scans[0].BSSID, c1)
	eq("Leases", d.DHCP.Leases[0].MAC, c1)
	eq("GlClients", d.DHCP.GlClients[0].MAC, c2)
	eq("Reservations", d.DHCP.Reservations[0].MAC, c3)
	hasKey("FDB.MACs", d.FDB.MACs, c1)
	hasKey("FDB.MACs (no MAC se conserva)", d.FDB.MACs, "garbage")
	if len(d.FDB.MACs) != 2 {
		t.Errorf("FDB.MACs: esperaba 2 entradas, obtuve %v", d.FDB.MACs)
	}
	hasKey("FDB.WirelessSide", d.FDB.WirelessSide, c2)
	hasKey("FDB.VLANs", d.FDB.VLANs, c3)
	if d.FDB.VLANs[c3] != 20 {
		t.Errorf("FDB.VLANs: esperaba VLAN 20, obtuve %v", d.FDB.VLANs)
	}
	hasKey("Arp", d.Arp, c1)
	hasKey("ArpDevs", d.ArpDevs, c2)
	eq("ArpStale", d.ArpStale[0], c3)
	eq("Lldp MAC", d.Lldp.Neighbors[0].ChassisMac, c1)
	eq("Lldp no MAC", d.Lldp.Neighbors[1].ChassisMac, "sw-core")
	dawn := d.Dawn.SSIDs["casa"]
	eq("Dawn AP", dawn.APs[0].BSSID, c1)
	hasKey("Dawn clientes", dawn.Clients, c2)
	eq("Dawn cliente BSSID", dawn.Clients[c2].BSSID, c3)
	us := d.Usteer.SSIDs["casa"]
	eq("Usteer AP", us.APs[0].BSSID, c2)
	hasKey("Usteer clientes", us.Clients, c3)
	hasKey("ClientBw", d.ClientBw.Hosts, c1)
	eq("RandomMACs", d.Discovery.RandomMACs[0], c2)

	// Secciones ausentes y payload nil no deben provocar pánico.
	normalizePayloadMACs(&probe.Payload{})
	normalizePayloadMACs(nil)
}

func TestCanonMACKeys(t *testing.T) {
	const canon = "AA:BB:CC:DD:EE:01"
	tests := []struct {
		name string
		in   map[string]string
		want map[string]string
	}{
		{"nil sigue nil", nil, nil},
		{"vacío no nil sigue vacío", map[string]string{}, map[string]string{}},
		{"ya canónico", map[string]string{canon: "a"}, map[string]string{canon: "a"}},
		{"minúsculas y guiones", map[string]string{"aa:bb:cc:dd:ee:01": "a", "aa-bb-cc-dd-ee-02": "b"},
			map[string]string{canon: "a", "AA:BB:CC:DD:EE:02": "b"}},
		{"colisión: gana la canónica (valor x)", map[string]string{canon: "x", "aa:bb:cc:dd:ee:01": "y"},
			map[string]string{canon: "x"}},
		{"colisión: gana la canónica (valor y)", map[string]string{canon: "y", "aa:bb:cc:dd:ee:01": "x", "aa-bb-cc-dd-ee-01": "z"},
			map[string]string{canon: "y"}},
		{"colisión sin canónica: gana la clave original menor", map[string]string{"aa:bb:cc:dd:ee:01": "minus", "aa-bb-cc-dd-ee-01": "guion", "aabbccddee01": "junto"},
			map[string]string{canon: "guion"}},
		{"clave que no es MAC se conserva", map[string]string{"garbage": "g", "aa:bb:cc:dd:ee:01": "a"},
			map[string]string{"garbage": "g", canon: "a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Repetido para cubrir el orden aleatorio de iteración del mapa.
			for i := 0; i < 50; i++ {
				got := canonMACKeys(tc.in)
				if (got == nil) != (tc.want == nil) {
					t.Fatalf("nil: esperaba nil=%v, obtuve %v", tc.want == nil, got)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("esperaba %v, obtuve %v", tc.want, got)
				}
			}
		})
	}

	t.Run("ya canónico devuelve el mismo mapa", func(t *testing.T) {
		in := map[string]string{canon: "a"}
		got := canonMACKeys(in)
		if reflect.ValueOf(got).Pointer() != reflect.ValueOf(in).Pointer() {
			t.Fatal("esperaba el mismo mapa, sin copiar")
		}
	})
}
