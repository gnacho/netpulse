// probe_1976_test.go — router en modo puente (WNDR3700v2 sin DSA): el
// internet le llega por la LAN y la WAN declarada en board.json no se usa. El
// puerto del bridge no debe pintarse como WAN (#1976).
package probe

import "testing"

// Caso del reportero: board.json declara la WAN en eth1 (sin cable) y eth0 es
// la interfaz del switch, miembro de br-lan, que es por donde sale la red.
func TestBuildEthPortsBridgedRouterKeepsDeclaredWan(t *testing.T) {
	layout, err := ParsePortLayout(`{"network":{"lan":{"device":"eth0"},"wan":{"device":"eth1"}}}`)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	// eth1 está arriba pero sin cable (not running); eth0 lleva el tráfico.
	states := ParsePortStates("eth0 up 1000 - 1 -\neth1 down 0 - 1 -\n")
	ifaces := map[string]IfRate{
		"eth0": {IfCounters: IfCounters{Rx: 9000, Tx: 8000}},
		"eth1": {IfCounters: IfCounters{}},
	}
	ports := BuildEthPorts(layout, states, map[string]bool{"eth0": true}, ifaces, "eth0")
	if len(ports) != 2 {
		t.Fatalf("bocas: %+v", ports)
	}
	if ports[0].ID != "wan" || ports[0].Iface != "eth1" || ports[0].Up {
		t.Fatalf("la WAN declarada debía quedarse en eth1 y abajo: %+v", ports[0])
	}
	if ports[1].ID != "eth0" || ports[1].Label != "ETH 0" || !ports[1].Up {
		t.Fatalf("el puerto del switch debía seguir siendo ETH 0: %+v", ports[1])
	}
	if ports[1].RxBytes != 9000 {
		t.Fatalf("las estadísticas del bridge son de eth0: %+v", ports[1])
	}
}

// La guarda es solo para miembros del bridge: si el uplink cae en una boca que
// no cuelga de la LAN, el runtime sigue mandando aunque la unidad tenga bridge
// (#1482).
func TestBuildEthPortsNonBridgeUplinkStillWins(t *testing.T) {
	layout, err := ParsePortLayout(`{"network":{"lan":{"ports":["eth1"]},"wan":{"device":"eth0"}}}`)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	states := ParsePortStates("eth0 up 0 - 1 -\neth1 up 1000 - 1 -\neth2 up 2500 - 1 -\n")
	ports := BuildEthPorts(layout, states, map[string]bool{"eth1": true}, nil, "eth2")
	if len(ports) != 3 {
		t.Fatalf("bocas: %+v", ports)
	}
	if ports[2].ID != "wan" {
		t.Fatalf("el uplink fuera del bridge debía salir como WAN: %+v", ports[2])
	}
	if ports[0].ID != "eth0" || ports[0].Label != "ETH 0" {
		t.Fatalf("la WAN del layout debía volver a su netdev: %+v", ports[0])
	}
}
