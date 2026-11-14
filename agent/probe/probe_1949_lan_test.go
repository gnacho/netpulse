// probe_1949_lan_test.go — AP con una sola boca física: su uplink es la LAN y
// no debe salir como WAN (#1949). El AP multi-boca conserva la atribución del
// miembro que mira al core (#1500).
package probe

import "testing"

// WAX220: board.json solo declara la LAN (device eth0), así que no hay layout
// de puertos y manda el fallback. Una boca física es eth0 dentro de br-lan.
func TestBuildEthPortsSinglePortApKeepsItsName(t *testing.T) {
	layout, err := ParsePortLayout(`{"network":{"lan":{"device":"eth0","protocol":"static"}}}`)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if len(layout) != 0 {
		t.Fatalf("este board.json no declara puertos, layout = %+v", layout)
	}
	states := ParsePortStates("br-lan up 1000\neth0 up 2500 - 1 -\n")
	ifaces := map[string]IfRate{"eth0": {IfCounters: IfCounters{Rx: 190045434383, Tx: 1866622728826}}}
	for _, uplink := range []string{"br-lan", "eth0"} {
		ports := BuildEthPorts(layout, states, map[string]bool{"eth0": true}, ifaces, uplink)
		if len(ports) != 1 {
			t.Fatalf("uplink %q: una sola boca física, got %+v", uplink, ports)
		}
		if ports[0].ID != "eth0" || ports[0].Label != "ETH 0" {
			t.Fatalf("uplink %q: la boca debía llamarse ETH 0, got %+v", uplink, ports[0])
		}
		if ports[0].RxBytes != 190045434383 {
			t.Fatalf("uplink %q: las estadísticas son de eth0, got %+v", uplink, ports[0])
		}
	}
}

// Control de #1500: con varias bocas el miembro del bridge que mira al core
// sigue saliendo como WAN, que es lo que distingue el uplink del resto.
func TestBuildEthPortsMultiPortApKeepsUplinkAsWan(t *testing.T) {
	states := ParsePortStates("br0 up 1000\neth0 up 2500 - 1 -\neth1 up 1000 - 1 -\neth3 up 1000 - 1 -\n")
	members := map[string]bool{"eth0": true, "eth1": true, "eth3": true}
	ifaces := map[string]IfRate{
		"eth0": {IfCounters: IfCounters{Rx: 188583580}},
		"eth3": {IfCounters: IfCounters{Rx: 619101463}},
	}
	ports := BuildEthPorts(nil, states, members, ifaces, "eth0")
	if len(ports) != 3 {
		t.Fatalf("bocas: %+v", ports)
	}
	if ports[0].ID != "wan" || ports[0].Iface != "eth0" {
		t.Fatalf("el uplink del AP multi-boca sigue siendo WAN: %+v", ports[0])
	}
}
