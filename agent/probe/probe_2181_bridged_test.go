// probe_2181_bridged_test.go — AP multi-boca con todas las bocas en br-lan
// (#2181): la boca WAN del board.json está puenteada, así que no es un enlace
// a internet, aunque su netdev se llame literalmente "wan".
package probe

import "testing"

// AVM FRITZ!Box 4040 como AP: board.json declara lan1..lan4 y la WAN en el
// netdev "wan"; /etc/config/network mete las cinco bocas en br-lan. La WAN no
// existe como enlace propio y el panel no puede pintarla como tal: con
// id "wan" la UI la saca como puerto WAN y anuncia "Internet link".
func TestBuildEthPortsBridgedWanIsNotTheWanPort(t *testing.T) {
	layout, err := ParsePortLayout(`{"network":{"lan":{"ports":["lan1","lan2","lan3","lan4"]},"wan":{"device":"wan"}}}`)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if len(layout) != 5 {
		t.Fatalf("layout: %+v", layout)
	}
	states := ParsePortStates("br-lan up 1000\nlan1 up 1000\nlan2 up 1000\nlan3 down -1\nlan4 up 100\nwan up 1000\n")
	members := map[string]bool{"lan1": true, "lan2": true, "lan3": true, "lan4": true, "wan": true}
	ifaces := map[string]IfRate{"wan": {IfCounters: IfCounters{Rx: 4242}}}
	ports := BuildEthPorts(layout, states, members, ifaces, "br-lan.10")

	if len(ports) != 5 {
		t.Fatalf("bocas: %+v", ports)
	}
	for _, p := range ports {
		if p.ID == "wan" {
			t.Fatalf("una boca puenteada no puede llevar el id wan: %+v", p)
		}
	}
	last := ports[len(ports)-1]
	if last.Label != "LAN 5" || last.Iface != "wan" {
		t.Fatalf("la ex-WAN va como última boca LAN: %+v", ports)
	}
	if !last.Up || last.Speed != "1 Gbps" || last.RxBytes != 4242 {
		t.Fatalf("estado y estadísticas de la boca: %+v", last)
	}
}

// Control: la WAN fuera del bridge sigue siendo la boca WAN y va primero.
func TestBuildEthPortsWanOutsideBridgeStaysWan(t *testing.T) {
	layout, err := ParsePortLayout(`{"network":{"lan":{"ports":["lan1","lan2","lan3","lan4"]},"wan":{"device":"wan"}}}`)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	states := ParsePortStates("lan1 up 1000\nlan2 down -1\nlan3 down -1\nlan4 down -1\nwan up 1000\n")
	members := map[string]bool{"lan1": true, "lan2": true, "lan3": true, "lan4": true}
	ports := BuildEthPorts(layout, states, members, nil, "wan")
	if len(ports) != 5 || ports[0].ID != "wan" || ports[0].Label != "WAN" {
		t.Fatalf("WAN fuera del bridge: %+v", ports)
	}
}
