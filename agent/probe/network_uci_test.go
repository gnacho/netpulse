package probe

import (
	"reflect"
	"testing"
)

// Config real del router x86 del reporter de #1757 (una bridge por red,
// alimentada por una subinterfaz 802.1Q).
const uciNetworksSubif = `
network.globals=globals
network.globals.packet_steering='1'
network.Lan=interface
network.Lan.device='br-lan'
network.Lan.proto='static'
network.Lan.ipaddr='192.168.1.1'
network.Lan.netmask='255.255.255.0'
network.@device[0]=device
network.@device[0].type='bridge'
network.@device[0].name='br-lan'
network.@device[0].ports='eth0'
network.Hack=interface
network.Hack.device='br-hack'
network.Hack.proto='static'
network.Hack.ipaddr='10.0.99.1'
network.Hack.netmask='255.255.255.0'
network.@device[1]=device
network.@device[1].type='bridge'
network.@device[1].name='br-hack'
network.@device[1].ports='eth0.888'
network.IOT=interface
network.IOT.device='br-iot'
network.IOT.proto='static'
network.IOT.ipaddr='192.168.111.1'
network.IOT.netmask='255.255.255.0'
network.@device[2]=device
network.@device[2].type='bridge'
network.@device[2].name='br-iot'
network.@device[2].ports='eth0.999'
`

// Config real del AP Flint 2 del mismo reporter: una sola bridge con VLAN
// filtering y una subinterfaz por red.
const uciNetworksFiltering = `
network.lan=interface
network.lan.device='br_switch.1'
network.lan.proto='static'
network.lan.ipaddr='192.168.1.2'
network.lan.netmask='255.255.255.0'
network.Guest=interface
network.Guest.proto='none'
network.Guest.device='br_switch.777'
network.Iot=interface
network.Iot.proto='none'
network.Iot.device='br_switch.999'
network.@device[6]=device
network.@device[6].type='bridge'
network.@device[6].name='br_switch'
network.@device[6].ports='eth1 lan1 lan2 lan3 lan4 lan5'
network.@bridge-vlan[0]=bridge-vlan
network.@bridge-vlan[0].device='br_switch'
network.@bridge-vlan[0].vlan='1'
network.@bridge-vlan[0].ports='eth1 lan1 lan2 lan3 lan4 lan5'
network.@bridge-vlan[1]=bridge-vlan
network.@bridge-vlan[1].device='br_switch'
network.@bridge-vlan[1].vlan='777'
network.@bridge-vlan[1].ports='eth1:t lan1:t lan2:t'
network.@bridge-vlan[2]=bridge-vlan
network.@bridge-vlan[2].device='br_switch'
network.@bridge-vlan[2].vlan='999'
network.@bridge-vlan[2].ports='eth1:t lan1:t lan2:t lan3:t'
`

func TestParseUciNetworksSubinterfaces(t *testing.T) {
	nets := ParseUciNetworks(uciNetworksSubif)
	if len(nets) != 3 {
		t.Fatalf("redes = %d, quiero 3: %+v", len(nets), nets)
	}
	byName := map[string]UciNetwork{}
	for _, n := range nets {
		byName[n.Name] = n
	}
	lan := byName["Lan"]
	if lan.Device != "br-lan" || lan.Subnet != "192.168.1.1/24" || lan.Vlan != 0 {
		t.Fatalf("Lan = %+v", lan)
	}
	if !reflect.DeepEqual(lan.Ports, []string{"eth0"}) {
		t.Fatalf("miembros de Lan = %v", lan.Ports)
	}
	hack := byName["Hack"]
	if hack.Vlan != 888 || hack.Subnet != "10.0.99.1/24" {
		t.Fatalf("Hack = %+v", hack)
	}
	if !reflect.DeepEqual(hack.Ports, []string{"eth0.888"}) {
		t.Fatalf("miembros de Hack = %v", hack.Ports)
	}
	if iot := byName["IOT"]; iot.Vlan != 999 || iot.Subnet != "192.168.111.1/24" {
		t.Fatalf("IOT = %+v", iot)
	}
	// El orden es el de la configuracion, no el alfabetico.
	if nets[0].Name != "Lan" || nets[1].Name != "Hack" || nets[2].Name != "IOT" {
		t.Fatalf("orden = %+v", nets)
	}
}

func TestParseUciNetworksBridgeFiltering(t *testing.T) {
	nets := ParseUciNetworks(uciNetworksFiltering)
	if len(nets) != 3 {
		t.Fatalf("redes = %d, quiero 3: %+v", len(nets), nets)
	}
	byName := map[string]UciNetwork{}
	for _, n := range nets {
		byName[n.Name] = n
	}
	lan := byName["lan"]
	if lan.Vlan != 1 || lan.Subnet != "192.168.1.2/24" {
		t.Fatalf("lan = %+v", lan)
	}
	guest := byName["Guest"]
	if guest.Vlan != 777 || guest.Proto != "none" {
		t.Fatalf("Guest = %+v", guest)
	}
	// Los miembros son los de SU bridge-vlan, sin el sufijo :t, no todos los
	// puertos de la bridge.
	if !reflect.DeepEqual(guest.Ports, []string{"eth1", "lan1", "lan2"}) {
		t.Fatalf("miembros de Guest = %v", guest.Ports)
	}
	iot := byName["Iot"]
	if iot.Vlan != 999 || !reflect.DeepEqual(iot.Ports, []string{"eth1", "lan1", "lan2", "lan3"}) {
		t.Fatalf("Iot = %+v", iot)
	}
}

func TestParseUciNetworksSinUci(t *testing.T) {
	if nets := ParseUciNetworks(""); nets != nil {
		t.Fatalf("vacio = %+v", nets)
	}
	// Una interfaz sin device no es una red que se pueda dibujar.
	if nets := ParseUciNetworks("network.x=interface\nnetwork.x.proto='static'\n"); len(nets) != 0 {
		t.Fatalf("sin device = %+v", nets)
	}
}
