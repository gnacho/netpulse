package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestAppendVirtualPortsListsWhatNetIfCarries(t *testing.T) {
	ports := []EthPort{{ID: "eth0", Iface: "eth0", Label: "WAN", Up: true}}
	ifaces := map[string]probe.IfCounters{
		"eth0":    {Rx: 1000, Tx: 2000},
		"br-lan":  {Rx: 500, Tx: 700},
		"vlan10":  {Rx: 0, Tx: 0},
		"wg0":     {Rx: 10, Tx: 20},
		"lo":      {Rx: 999999, Tx: 999999},
		"br-wan2": {Rx: 1, Tx: 0},
	}
	rxBps, txBps := 800.0, 200.0
	rates := map[string]probe.IfRate{
		"br-lan": {IfCounters: ifaces["br-lan"], RxBps: &rxBps, TxBps: &txBps},
	}

	out := appendVirtualPorts(ports, ifaces, rates)

	if len(out) != 5 {
		t.Fatalf("expected 4 physical plus 4 virtual ports, got %d: %+v", len(out), out)
	}
	// The physical port is left as it was and not duplicated.
	if out[0].ID != "eth0" || out[0].Label != "WAN" || !out[0].Up {
		t.Fatalf("physical port changed: %+v", out[0])
	}
	byName := map[string]EthPort{}
	for _, p := range out[1:] {
		byName[p.Label] = p
	}
	if _, ok := byName["lo"]; ok {
		t.Fatal("loopback must not be listed")
	}
	if _, ok := byName["eth0"]; ok {
		t.Fatal("a physical port must not be added twice")
	}
	if p := byName["br-lan"]; p.Family != "bridge" || p.ID != "if-br-lan" || p.Iface != "br-lan" {
		t.Fatalf("bridge port wrong: %+v", p)
	}
	if p := byName["br-lan"]; !p.Up || p.RxBps != 800 || p.TxBps != 200 {
		t.Fatalf("bridge traffic wrong: %+v", p)
	}
	if p := byName["vlan10"]; p.Family != "vlan" || p.Up {
		t.Fatalf("idle vlan should be listed and not up: %+v", p)
	}
	if p := byName["wg0"]; p.Family != "tunnel" || !p.Up {
		t.Fatalf("tunnel wrong: %+v", p)
	}
	// Rates only reach the interface they belong to.
	if p := byName["wg0"]; p.RxBps != 0 || p.TxBps != 0 {
		t.Fatalf("rates leaked into another interface: %+v", p)
	}
}

func TestAppendVirtualPortsWithoutNetIfChangesNothing(t *testing.T) {
	ports := []EthPort{{ID: "eth0", Iface: "eth0"}}
	if out := appendVirtualPorts(ports, nil, nil); len(out) != 1 || out[0].ID != "eth0" {
		t.Fatalf("expected the same ports, got %+v", out)
	}
}

func TestVirtualIfaceFamilyNamesTheFamilies(t *testing.T) {
	cases := map[string]string{
		"br-lan":    "bridge",
		"bridge":    "bridge",
		"vlan10":    "vlan",
		"eth0.10":   "vlan",
		"bond0":     "lag",
		"lagg0":     "lag",
		"wg0":       "tunnel",
		"tun0":      "tunnel",
		"pppoe-wan": "tunnel",
		"gre1":      "tunnel",
		"eth1":      "virtual",
		"foo0":      "virtual",
	}
	for name, want := range cases {
		if got := virtualIfaceFamily(name); got != want {
			t.Errorf("virtualIfaceFamily(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestLinkPortsKeepsOnlyPhysicalLinks(t *testing.T) {
	ports := []EthPort{
		{ID: "eth0", Iface: "eth0"},
		{ID: "if-br-lan", Iface: "br-lan", Family: "bridge"},
		{ID: "snmp-2", Iface: "eth1", Family: "ethernet"},
		{ID: "if-vlan10", Iface: "vlan10", Family: "vlan"},
	}
	out := linkPorts(ports)
	if len(out) != 2 {
		t.Fatalf("expected the two physical links, got %+v", out)
	}
	if out[0].ID != "eth0" || out[1].ID != "snmp-2" {
		t.Fatalf("wrong ports kept: %+v", out)
	}
	if same := linkPorts(out); len(same) != 2 || &same[0] != &out[0] {
		t.Fatal("with nothing to drop the same slice should come back")
	}
}
