// stale_arp6_wan_2200_test.go — #2200: el vecino IPv6 confirmado por la
// comprobación activa entraba en la tabla ARP sin su interfaz, así que el
// servidor no podía marcarlo como aprendido en la boca WAN y lo publicaba como
// cliente LAN por cable.
package probe

import (
	"context"
	"testing"
)

const v4LanConfirmada = `192.168.1.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE
`

// Un vecino fe80:: de eth1 confirmado con echo tiene que llevar eth1 en el mapa
// de interfaces, y no puede borrar las interfaces de las entradas IPv4.
func TestProbeArpKeepsIPv6NeighbourInterface(t *testing.T) {
	run := &staleArp6Runner{
		v4: []string{v4LanConfirmada},
		v6: []string{
			"fe80::a8bb:ccff:fe00:1 dev eth1 lladdr CC:CC:CC:00:00:01 STALE\n",
			"fe80::a8bb:ccff:fe00:1 dev eth1 lladdr CC:CC:CC:00:00:01 REACHABLE\n",
		},
	}
	p := NewProber(run, Options{})
	arp, _, devs := p.probeArp(context.Background())

	if arp["CC:CC:CC:00:00:01"] != "fe80::a8bb:ccff:fe00:1" {
		t.Fatalf("la pareja IPv6 no se confirmó: %+v", arp)
	}
	if got := devs["CC:CC:CC:00:00:01"]; got != "eth1" {
		t.Fatalf("interfaz del vecino IPv6: %q, esperaba eth1 (%+v)", got, devs)
	}
	if got := devs["AA:AA:AA:AA:AA:03"]; got != "br-lan" {
		t.Fatalf("la interfaz IPv4 se perdió: %q (%+v)", got, devs)
	}
}

// Un vecino que no contesta no entra ni en ARP ni en el mapa de interfaces: la
// interfaz sola no prueba presencia.
func TestProbeArpSilentIPv6LeavesNoInterface(t *testing.T) {
	run := &staleArp6Runner{
		v4:      []string{v4LanConfirmada},
		v6:      []string{"fe80::2 dev eth1 lladdr CC:CC:CC:00:00:02 STALE\n"},
		noReply: map[string]bool{"fe80::2": true},
	}
	p := NewProber(run, Options{})
	_, _, devs := p.probeArp(context.Background())
	if got, ok := devs["CC:CC:CC:00:00:02"]; ok {
		t.Fatalf("un vecino mudo no debe aportar interfaz: %q", got)
	}
}
