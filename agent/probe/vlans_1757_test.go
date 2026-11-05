package probe

import "testing"

// #1757: los datos reales del router x86 y del AP de un usuario que segmenta
// con subinterfaces 802.1Q (router) y con bridge vlan filtering (AP). Los tres
// fallos que destaparon: el FDB descartaba el puerto de la subinterfaz, solo se
// leía un bridge y la VLAN de cada cliente (`vlan <id>`) se tiraba.

// Router x86: cinco bridges, cada uno colgado de una subinterfaz del mismo
// eth0. El FDB reporta el puerto como `eth0.999`.
func TestFdbRuterSubinterfaces(t *testing.T) {
	out := "==PORTS==br-iot\n" +
		"1 eth0.999\n" +
		"==MACS==br-iot\n" +
		"eth0.999 aa:bb:cc:00:00:99\n" +
		"==PORTS==br-guest\n" +
		"1 eth0.777\n" +
		"==MACS==br-guest\n" +
		"eth0.777 aa:bb:cc:00:00:77\n"
	wired, wirelessSide, _ := ParseBridgeFdbVlans(out)
	if wired["AA:BB:CC:00:00:99"] != "eth0.999" {
		t.Fatalf("cliente de br-iot sin puerto: %+v", wired)
	}
	if wired["AA:BB:CC:00:00:77"] != "eth0.777" {
		t.Fatalf("cliente de br-guest sin puerto: %+v", wired)
	}
	if len(wirelessSide) != 0 {
		t.Fatalf("una subinterfaz cableada no es lado wireless: %+v", wirelessSide)
	}
}

// AP con bridge vlan filtering: el FDB trae la VLAN real de cada cliente y las
// tablas de puertos de dos bridges no se pisan (cada uno numera desde 1).
func TestFdbVlanPorClienteYDosBridges(t *testing.T) {
	out := "==PORTS==br_switch\n" +
		"1 eth1\n2 lan1\n3 phy1-ap2\n" +
		"==MACS==br_switch\n" +
		"lan1 aa:bb:cc:00:00:01 777\n" +
		"phy1-ap2 aa:bb:cc:00:00:02 999\n" +
		"==PORTS==br-guest\n" +
		"1 eth0.777\n2 lan1\n" +
		"==MACS==br-guest\n" +
		"eth0.777 aa:bb:cc:00:00:03 777\n" +
		"lan1 aa:bb:cc:00:00:04 0\n"
	wired, wirelessSide, vlans := ParseBridgeFdbVlans(out)
	if wired["AA:BB:CC:00:00:01"] != "lan1" {
		t.Fatalf("cableado: %+v", wired)
	}
	if wirelessSide["AA:BB:CC:00:00:02"] != "phy1-ap2" {
		t.Fatalf("wifi: %+v", wirelessSide)
	}
	if vlans["AA:BB:CC:00:00:01"] != 777 || vlans["AA:BB:CC:00:00:02"] != 999 {
		t.Fatalf("VLAN por cliente: %+v", vlans)
	}
	// El segundo bridge reusa port_no 1 y 2 para otros puertos: la tabla del
	// primero no debe filtrarse a la del segundo.
	if wired["AA:BB:CC:00:00:03"] != "eth0.777" {
		t.Fatalf("segundo bridge mal resuelto: %+v", wired)
	}
	if got, ok := vlans["AA:BB:CC:00:00:04"]; ok || got != 0 {
		t.Fatalf("sin campo vlan no hay VLAN que inventar: %+v", vlans)
	}
}

// La salida de un agente viejo (una sola tabla, sin sufijo ni vlan) sigue
// valiendo: no se puede romper la flota al actualizar el servidor.
func TestFdbFormatoAntiguo(t *testing.T) {
	out := "==PORTS==\n1 lan1\n2 phy0-ap0\n" +
		"==MACS==\n1 aa:bb:cc:00:00:01\nphy0-ap0 aa:bb:cc:00:00:02\n"
	wired, wirelessSide, vlans := ParseBridgeFdbVlans(out)
	if wired["AA:BB:CC:00:00:01"] != "lan1" {
		t.Fatalf("cableado: %+v", wired)
	}
	if wirelessSide["AA:BB:CC:00:00:02"] != "phy0-ap0" {
		t.Fatalf("wifi: %+v", wirelessSide)
	}
	if len(vlans) != 0 {
		t.Fatalf("formato antiguo no trae VLANs: %+v", vlans)
	}
}

// #1757: `ubus call hostapd.<if> get_clients` es la fuente primaria en
// cualquier OpenWrt, y el objeto ubus es la interfaz AP. Sin rellenar Iface el
// cliente wifi no se puede casar con el puerto del bridge y sale sin VLAN.
func TestHostapdClientsRellenaIfaceYSSID(t *testing.T) {
	out := "==AP==hostapd.phy0-ap0\n" +
		`{"freq":2437,"clients":{"aa:bb:cc:00:00:01":{"auth":true,"assoc":true,"authorized":true,"signal":-55,"bytes":{"rx":10,"tx":20}}}}` + "\n" +
		"==AP==hostapd.phy1-ap2\n" +
		`{"freq":5180,"clients":{"aa:bb:cc:00:00:02":{"auth":true,"assoc":true,"authorized":true,"signal":-60,"bytes":{"rx":30,"tx":40}}}}` + "\n" +
		"==SSID==\nphy0-ap0 Iot\nphy1-ap2 Paperopoli\n"
	got := ParseHostapdClients(out)
	if c := got["AA:BB:CC:00:00:01"]; c.Iface != "phy0-ap0" || c.SSID != "Iot" {
		t.Fatalf("cliente 2.4G: %+v", c)
	}
	if c := got["AA:BB:CC:00:00:02"]; c.Iface != "phy1-ap2" || c.SSID != "Paperopoli" {
		t.Fatalf("cliente 5G: %+v", c)
	}
}

// El nombre de la red vive en el pseudo-puerto de la subinterfaz
// (br_switch.777); los puertos wifi solo traen el id. Un id que nombra una
// sola red en el equipo hereda su nombre.
func TestMergeVlanPortsNombraPuertosWifi(t *testing.T) {
	kernel := ParseBridgeVlan(`port              vlan-id
eth1              1 PVID Egress Untagged
                  777
                  999
br_switch         1
                  777
                  999
phy1-ap2          777 PVID Egress Untagged
phy1-ap1          999 PVID Egress Untagged
`)
	declared := ParseUciNetworkVlans(`network.lan=interface
network.lan.device='br_switch.1'
network.lan.ipaddr='192.168.1.2'
network.lan.netmask='255.255.255.0'
network.Iot=interface
network.Iot.proto='none'
network.Iot.device='br_switch.999'
network.Guest=interface
network.Guest.proto='none'
network.Guest.device='br_switch.777'
`)
	got := MergeVlanPorts(kernel, declared)
	byPort := map[string]VlanPort{}
	for _, p := range got {
		byPort[p.Port] = p
	}
	entry := func(port string, id int) VlanEntry {
		for _, e := range byPort[port].Vlans {
			if e.ID == id {
				return e
			}
		}
		t.Fatalf("%s no lleva la VLAN %d: %+v", port, id, byPort[port])
		return VlanEntry{}
	}
	if e := entry("phy1-ap2", 777); e.Name != "Guest" || !e.PVID {
		t.Fatalf("phy1-ap2 debería heredar el nombre de la VLAN 777: %+v", e)
	}
	if e := entry("phy1-ap1", 999); e.Name != "Iot" {
		t.Fatalf("phy1-ap1 debería heredar el nombre de la VLAN 999: %+v", e)
	}
	if e := entry("br_switch.1", 1); e.Name != "lan" || e.Subnet != "192.168.1.2/24" {
		t.Fatalf("br_switch.1: %+v", e)
	}
}

// Dos redes distintas con el mismo número en el equipo: mejor sin nombre que
// con el de la otra.
func TestMergeVlanPortsNoInventaNombreAmbiguo(t *testing.T) {
	kernel := ParseBridgeVlan(`port              vlan-id
phy1-ap2          100 PVID Egress Untagged
br-a              100
br-b              100
`)
	declared := []VlanPort{
		{Port: "br-a.100", Vlans: []VlanEntry{{ID: 100, PVID: true, Name: "Oficina"}}},
		{Port: "br-b.100", Vlans: []VlanEntry{{ID: 100, PVID: true, Name: "Invitados"}}},
	}
	for _, p := range MergeVlanPorts(kernel, declared) {
		if p.Port != "phy1-ap2" {
			continue
		}
		if len(p.Vlans) != 1 || p.Vlans[0].Name != "" {
			t.Fatalf("no debe heredar un nombre ambiguo: %+v", p.Vlans)
		}
	}
}
