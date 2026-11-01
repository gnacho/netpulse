package adapters

// FORK: #1757 - the VLAN numbers from `bridge vlan show` get the names LuCI
// keeps for them.

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestVlanPortsCarryTheLuCINames(t *testing.T) {
	ports := []VlanPort{
		{Port: "lan1", Vlans: []VlanEntry{{ID: 1, PVID: true}, {ID: 20, Tagged: true}}},
		{Port: "lan2", Vlans: []VlanEntry{{ID: 30}}},
	}
	luci := &probe.LuCILabels{VlanLabels: map[string]string{"1": "LAN", "20": "IoT"}}

	got := vlanPortsWithNames(ports, luci)
	if len(got) != 2 || len(got[0].Vlans) != 2 {
		t.Fatalf("shape: %+v", got)
	}
	if got[0].Vlans[0].Name != "LAN" || got[0].Vlans[1].Name != "IoT" {
		t.Errorf("names: %+v", got[0].Vlans)
	}
	// A VLAN without a label keeps no name: an invented one would be worse
	// than the number alone.
	if got[1].Vlans[0].Name != "" {
		t.Errorf("unlabelled VLAN got %q", got[1].Vlans[0].Name)
	}
	// The flags and the id survive the copy.
	if !got[0].Vlans[0].PVID || !got[0].Vlans[1].Tagged || got[0].Vlans[1].ID != 20 {
		t.Errorf("flags: %+v", got[0].Vlans)
	}
	// The payload the registry holds is not written to.
	if ports[0].Vlans[0].Name != "" {
		t.Fatalf("the payload was mutated: %+v", ports[0].Vlans)
	}

	// Without labels there is nothing to join, so the same slice comes back.
	if same := vlanPortsWithNames(ports, nil); &same[0] != &ports[0] {
		t.Error("no labels: a copy was made for nothing")
	}
	if same := vlanPortsWithNames(ports, &probe.LuCILabels{}); &same[0] != &ports[0] {
		t.Error("empty labels: a copy was made for nothing")
	}
}
