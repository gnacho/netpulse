package adapters

import "testing"

func TestPickBackhaulUplink(t *testing.T) {
	// A managed switch with a 10G parent uplink (xg27) plus 1G AP downlinks and
	// a 100M access port that happens to be first: the core uplink wins.
	sw := []EthPort{
		{ID: "snmp-1", Label: "1/g1", Up: true, Speed: "100 Mbps", PeerKind: PeerKindMAC},
		{ID: "snmp-9", Label: "1/g9", Up: true, Speed: "1 Gbps", PeerKind: PeerKindRouterLinkLLDP},
		{ID: "snmp-27", Label: "1/xg27", Up: true, Speed: "10 Gbps", PeerKind: PeerKindRouterLinkLLDP},
	}
	if got := pickBackhaulUplink(sw); got == nil || got.ID != "snmp-27" {
		t.Fatalf("fastest router-link: %+v", got)
	}

	// Single router-link (an AP whose only wired peer is its parent): picked
	// even though a plain access port is up and listed first.
	ap := []EthPort{
		{ID: "eth1", Label: "ETH 1", Up: true, Speed: "1 Gbps", PeerKind: PeerKindMAC},
		{ID: "wan", Label: "WAN", Up: true, Speed: "2.5 Gbps", PeerKind: PeerKindRouterLink},
	}
	if got := pickBackhaulUplink(ap); got == nil || got.ID != "wan" {
		t.Fatalf("router-link over access port: %+v", got)
	}

	// No router-link peer (single-cable AP without LLDP): fall back to first up.
	noll := []EthPort{
		{ID: "eth1", Label: "ETH 1", Up: false},
		{ID: "eth2", Label: "ETH 2", Up: true, Speed: "1 Gbps", PeerKind: PeerKindMAC},
	}
	if got := pickBackhaulUplink(noll); got == nil || got.ID != "eth2" {
		t.Fatalf("first-up fallback: %+v", got)
	}

	// Nothing up: nil.
	if got := pickBackhaulUplink([]EthPort{{ID: "eth1", Up: false}}); got != nil {
		t.Fatalf("no up port should be nil, got %+v", got)
	}
}
