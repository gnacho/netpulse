// stale_arp6_wan_2200_test.go — #2200 en el camino SSH: un vecino IPv6
// confirmado en la boca WAN llega al servidor sin su interfaz, así que el
// filtro del lado WAN (#2183) no lo ve y lo publica como cliente LAN por cable.
package adapters

import (
	"testing"
)

func TestGetArpKeepsIPv6NeighbourInterface(t *testing.T) {
	pool := &staleArp6Pool{
		v4: []string{"192.168.2.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE\n"},
		v6: []string{
			"fe80::a8bb:ccff:fe00:1 dev eth1 lladdr CC:CC:CC:00:00:01 STALE\n",
			"fe80::a8bb:ccff:fe00:1 dev eth1 lladdr CC:CC:CC:00:00:01 REACHABLE\n",
		},
	}
	c := &OpenWrtClient{Host: "192.168.2.11", pool: pool}

	arp, _, devs := c.GetArp()
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

// El mapa de interfaces es lo que marca el lado WAN: con la interfaz del vecino
// IPv6, el gateway deja de publicarlo como cliente LAN.
func TestBuildDevicesMarksIPv6WanNeighbour(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	l.gatewayCfg = &RouterConfig{ID: "gw", Name: "Gateway", IsGateway: true}
	polled := map[string]*routerPolled{
		"gw": {
			cfg: RouterConfig{ID: "gw", Name: "Gateway", IsGateway: true},
			arp: map[string]string{
				"CC:CC:CC:00:00:01": "fe80::a8bb:ccff:fe00:1",
			},
			arpDevs: map[string]string{
				"CC:CC:CC:00:00:01": "eth1",
			},
		},
	}
	devs := l.buildDevices(polled, "eth1")
	for _, d := range devs {
		if d.MAC != "CC:CC:CC:00:00:01" {
			continue
		}
		if !d.WanSide {
			t.Fatalf("vecino IPv6 de la boca WAN sin marcar: %+v", d)
		}
		return
	}
	t.Fatalf("el vecino no aparece en devices: %+v", devs)
}
