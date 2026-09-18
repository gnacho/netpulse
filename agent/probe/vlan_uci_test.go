package probe

import "testing"

// The shape `uci show network` prints on a router with 802.1Q filtering:
// one bridge-vlan section per VLAN, each listing the ports that carry it,
// and a list option printing all its values on one line.
const uciBridgeVlans = `network.@bridge-vlan[0]=bridge-vlan
network.@bridge-vlan[0].device='br-home'
network.@bridge-vlan[0].vlan='10'
network.@bridge-vlan[0].ports='lan2:u*' 'eth0:t'
network.@bridge-vlan[1]=bridge-vlan
network.@bridge-vlan[1].device='br-home'
network.@bridge-vlan[1].vlan='20'
network.@bridge-vlan[1].ports='lan2:t'
network.@bridge-vlan[2]=bridge-vlan
network.@bridge-vlan[2].device='br-home'
network.@bridge-vlan[2].vlan='30'
network.@bridge-vlan[2].ports='lan3'
`

func vlansOf(ports []VlanPort, name string) []VlanEntry {
	for _, p := range ports {
		if p.Port == name {
			return p.Vlans
		}
	}
	return nil
}

func TestParseUciBridgeVlansGroupsByPort(t *testing.T) {
	ports := ParseUciBridgeVlans(uciBridgeVlans)
	if len(ports) != 3 {
		t.Fatalf("got %d ports, want lan2, eth0 and lan3: %+v", len(ports), ports)
	}
	// The panel lists ports, each with the VLANs it carries — the same
	// shape `bridge vlan show` reports.
	lan2 := vlansOf(ports, "lan2")
	if len(lan2) != 2 {
		t.Fatalf("lan2 = %+v, want both VLANs", lan2)
	}
	if lan2[0].ID != 10 || lan2[0].Tagged || !lan2[0].PVID {
		t.Fatalf("lan2 vlan 10 = %+v, want untagged and the port's own", lan2[0])
	}
	if lan2[1].ID != 20 || !lan2[1].Tagged || lan2[1].PVID {
		t.Fatalf("lan2 vlan 20 = %+v, want tagged", lan2[1])
	}
	if eth0 := vlansOf(ports, "eth0"); len(eth0) != 1 || !eth0[0].Tagged {
		t.Fatalf("eth0 = %+v, want one tagged VLAN", eth0)
	}
	// A bare port name is untagged and not the ingress VLAN.
	if lan3 := vlansOf(ports, "lan3"); len(lan3) != 1 || lan3[0].Tagged || lan3[0].PVID {
		t.Fatalf("lan3 = %+v", lan3)
	}
}

func TestParseUciBridgeVlansIgnoresEverythingElse(t *testing.T) {
	// Other network sections, and VLAN ids outside the valid range, are
	// not VLANs of a bridge.
	noise := `network.lan=interface
network.lan.device='br-home.10'
network.@device[0]=device
network.@device[0].name='br-home'
network.@bridge-vlan[0]=bridge-vlan
network.@bridge-vlan[0].vlan='5000'
network.@bridge-vlan[0].ports='lan2:t'
`
	if got := ParseUciBridgeVlans(noise); len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
	if got := ParseUciBridgeVlans(""); len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
}
