package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

// #1757: la lista de flota lleva el bridge VLAN filtering por puerto, con el
// nombre declarado en LuCI, para que la topología pueda etiquetar los
// clientes cableados de cada VLAN.
func TestBuildRouterExposesVlans(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	p := &routerPolled{
		cfg: RouterConfig{ID: "gw", Host: "192.168.1.1"},
		vlans: []probe.VlanPort{
			{Port: "lan1", Vlans: []probe.VlanEntry{{ID: 20, Tagged: true}, {ID: 1, PVID: true}}},
		},
		luci: &probe.LuCILabels{VlanLabels: map[string]string{"20": "IoT"}},
	}
	r := l.buildRouter(p, nil)
	if len(r.Vlans) != 1 || r.Vlans[0].Port != "lan1" {
		t.Fatalf("vlans = %+v, want el puerto lan1", r.Vlans)
	}
	got := r.Vlans[0].Vlans
	if len(got) != 2 || got[0].ID != 20 || got[0].Name != "IoT" || !got[0].Tagged || got[1].ID != 1 || !got[1].PVID {
		t.Fatalf("entradas = %+v", got)
	}
}

// Sin bridge vlan filtering el campo queda nil: la UI no debe pintar VLANs
// que la unidad no ha declarado.
func TestBuildRouterWithoutVlans(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	r := l.buildRouter(&routerPolled{cfg: RouterConfig{ID: "ap", Host: "192.168.1.2"}}, nil)
	if r.Vlans != nil {
		t.Fatalf("vlans = %+v, want nil", r.Vlans)
	}
}
