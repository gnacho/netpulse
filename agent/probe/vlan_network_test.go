package probe

import "testing"

// The shape `uci show network` prints on a router that segments with 802.1Q
// subinterfaces (issue #1757): one bridge per network, each fed a tagged
// subinterface of the physical port, and one interface per bridge carrying
// the name and the address of the network. No bridge-vlan sections at all,
// which is why `bridge vlan show` reports every port on VLAN 1.
const uciSubifNetworks = `network.Lan=interface
network.Lan.device='br-lan'
network.Lan.proto='static'
network.Lan.ipaddr='192.168.1.1'
network.Lan.netmask='255.255.255.0'
network.IOT=interface
network.IOT.device='br-iot'
network.IOT.proto='static'
network.IOT.ipaddr='192.168.111.1'
network.IOT.netmask='255.255.255.0'
network.Services=interface
network.Services.device='br-services'
network.Services.proto='static'
network.Services.ipaddr='192.168.222.1'
network.Services.netmask='255.255.255.0'
network.@device[0]=device
network.@device[0].name='br-lan'
network.@device[0].type='bridge'
network.@device[0].ports='eth1' 'eth0'
network.@device[1]=device
network.@device[1].name='br-iot'
network.@device[1].type='bridge'
network.@device[1].ports='eth0.999'
network.@device[2]=device
network.@device[2].name='br-services'
network.@device[2].type='bridge'
network.@device[2].ports='eth0.555'
`

func TestParseUciNetworkVlansReadsSubinterfaces(t *testing.T) {
	ports := ParseUciNetworkVlans(uciSubifNetworks)

	iot := vlansOf(ports, "br-iot")
	if len(iot) != 1 {
		t.Fatalf("br-iot = %+v, want one VLAN", iot)
	}
	if iot[0].ID != 999 || !iot[0].PVID || iot[0].Tagged {
		t.Fatalf("br-iot vlan = %+v, want 999 as the native VLAN of the bridge", iot[0])
	}
	if iot[0].Name != "IOT" || iot[0].Subnet != "192.168.111.1/24" {
		t.Fatalf("br-iot name/subnet = %q %q", iot[0].Name, iot[0].Subnet)
	}

	// The physical port carries it tagged. This is what tells a client behind
	// eth0 which VLAN it is on.
	eth0 := vlansOf(ports, "eth0")
	if len(eth0) != 2 {
		t.Fatalf("eth0 = %+v, want the tagged VLANs 999 and 555", eth0)
	}
	if eth0[0].ID != 999 || !eth0[0].Tagged {
		t.Fatalf("eth0 vlan = %+v, want 999 tagged", eth0[0])
	}

	// A bridge with no subinterface is a plain untagged network, and still
	// has a name and a subnet worth showing.
	lan := vlansOf(ports, "br-lan")
	if len(lan) != 1 || lan[0].ID != 1 || lan[0].Name != "Lan" || lan[0].Subnet != "192.168.1.1/24" {
		t.Fatalf("br-lan = %+v", lan)
	}

	// The forwarding database names the bridge port, which here is the
	// subinterface: it has to resolve to the same VLAN as the bridge or a
	// device behind eth0.999 shows no VLAN at all (#1757).
	sub := vlansOf(ports, "eth0.999")
	if len(sub) != 1 || sub[0].ID != 999 || sub[0].Name != "IOT" || !sub[0].PVID {
		t.Fatalf("eth0.999 = %+v, want VLAN 999 (the port the FDB reports)", sub)
	}
}

func TestParseUciNetworkVlansIgnoresNonBridges(t *testing.T) {
	noise := `network.wan=interface
network.wan.device='eth1.836'
network.wan.proto='dhcp'
network.@device[0]=device
network.@device[0].name='br-lan'
network.@device[0].type='bridge'
network.@device[0].ports='eth0' 'eth1'
`
	ports := ParseUciNetworkVlans(noise)
	// The bridge is untagged, so it is LAN 1. The WAN is bound straight to a
	// subinterface, with no bridge in between, and it is still VLAN 836.
	if got := vlansOf(ports, "eth1.836"); len(got) != 1 || got[0].ID != 836 || !got[0].PVID {
		t.Fatalf("eth1.836 = %+v, want the WAN VLAN", got)
	}
	if got := vlansOf(ports, "eth1"); len(got) != 0 {
		t.Fatalf("eth1 = %+v, want nothing: it is a plain member, not a VLAN", got)
	}
}

func TestParseUciNetworkVlansNeedsNoMasquerade(t *testing.T) {
	// `uci show` order is stable but nothing guarantees the section header
	// comes before its options, and a missing netmask must not invent a /24.
	out := `network.iot.proto='static'
network.iot.device='br-iot'
network.iot=interface
network.iot.ipaddr='10.0.0.1'
network.dev.ports='eth0.42'
network.dev.type='bridge'
network.dev.name='br-iot'
network.dev=device
`
	ports := ParseUciNetworkVlans(out)
	iot := vlansOf(ports, "br-iot")
	if len(iot) != 1 || iot[0].ID != 42 || iot[0].Subnet != "10.0.0.1" {
		t.Fatalf("br-iot = %+v", iot)
	}
}

// A router without vlan_filtering answers "VLAN 1" for every port. That is a
// valid answer to a question nobody asked, and it must not shadow the real
// segmentation.
const ruttoBridgeVlan = `port    vlan-id
eth0    1 PVID Egress Untagged
eth0.999    1 PVID Egress Untagged
br-iot    1 PVID Egress Untagged
`

func TestMergeVlanPortsDropsTheDefaultVlan(t *testing.T) {
	kernel := ParseBridgeVlan(ruttoBridgeVlan)
	declared := ParseUciNetworkVlans(uciSubifNetworks)
	merged := MergeVlanPorts(kernel, declared)

	iot := vlansOf(merged, "br-iot")
	if len(iot) != 1 || iot[0].ID != 999 {
		t.Fatalf("br-iot = %+v, want only the real VLAN", iot)
	}
	// Ports the kernel knows and the config does not are kept untouched.
	if got := vlansOf(merged, "eth0"); len(got) != 2 || got[0].ID != 999 || got[1].ID != 555 {
		t.Fatalf("eth0 = %+v, want the declared VLANs behind the kernel ones", got)
	}
}

func TestMergeVlanPortsKeepsKernelFlags(t *testing.T) {
	kernel := []VlanPort{{Port: "lan2", Vlans: []VlanEntry{{ID: 10, Tagged: true}}}}
	declared := []VlanPort{{Port: "lan2", Vlans: []VlanEntry{{ID: 10, Name: "IoT", Subnet: "10.0.0.1/24"}}}}
	merged := MergeVlanPorts(kernel, declared)
	if len(merged) != 1 || len(merged[0].Vlans) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	e := merged[0].Vlans[0]
	if !e.Tagged || e.Name != "IoT" || e.Subnet != "10.0.0.1/24" {
		t.Fatalf("entry = %+v, want the kernel flags and the declared name", e)
	}
}
