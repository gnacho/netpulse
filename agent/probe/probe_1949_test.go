// probe_1949_test.go — AP de un solo puerto: la "wan" del board.json es un
// miembro del bridge LAN y no debe salir como enlace a internet (#1949).
package probe

import "testing"

// board.json declara eth0 como LAN y también como WAN (caso WAX220). La boca
// es una sola y es LAN: ni id "wan" (la UI la pinta como Internet) ni boca
// duplicada.
func TestBuildEthPortsSinglePortApNoWan(t *testing.T) {
	layout, err := ParsePortLayout(`{"network":{"lan":{"ports":["eth0"]},"wan":{"device":"eth0"}}}`)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	ports := BuildEthPorts(layout, ParsePortStates("eth0 up 1000 - 1 -\n"), map[string]bool{"eth0": true}, nil, "br-lan")
	if len(ports) != 1 {
		t.Fatalf("una sola boca física, got %+v", ports)
	}
	if ports[0].ID == "wan" || ports[0].Label == "WAN" {
		t.Fatalf("el único puerto del AP no es una WAN: %+v", ports[0])
	}
	if ports[0].ID != "eth0" {
		t.Fatalf("la boca conserva su netdev, got %+v", ports[0])
	}
}

// Si el board.json solo declara la WAN y esa boca cuelga del bridge, sale
// como LAN con su netdev, no como WAN.
func TestBuildEthPortsBridgeMemberWanBecomesLan(t *testing.T) {
	layout, err := ParsePortLayout(`{"network":{"wan":{"device":"eth0"}}}`)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	ports := BuildEthPorts(layout, ParsePortStates("eth0 up 1000 - 1 -\n"), map[string]bool{"eth0": true}, nil, "")
	if len(ports) != 1 {
		t.Fatalf("bocas: %+v", ports)
	}
	if ports[0].ID != "eth0" || ports[0].Label != "LAN 1" {
		t.Fatalf("la boca del bridge debía salir como LAN 1 con su id: %+v", ports[0])
	}
}

// Guard: un router de verdad, cuya WAN no es miembro del bridge, sigue
// saliendo como WAN.
func TestBuildEthPortsRealWanNotBridgeMember(t *testing.T) {
	layout, err := ParsePortLayout(`{"network":{"lan":{"ports":["lan1"]},"wan":{"device":"eth0"}}}`)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	ports := BuildEthPorts(layout, ParsePortStates("eth0 up 1000 - 1 -\nlan1 up 1000 - 1 -\n"), map[string]bool{"lan1": true}, nil, "eth0")
	if len(ports) != 2 || ports[0].ID != "wan" {
		t.Fatalf("la WAN real no se toca: %+v", ports)
	}
}
