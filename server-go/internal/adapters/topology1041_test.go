package adapters

import "testing"

// #1041: el vecino LLDP "sw1" debe identificar al fleet device enrolado como
// "sw1.lan" (primer label DNS). Antes el match por nombre exigía igualdad
// exacta y el uplink caía al gateway con el vecino duplicado como cliente.
func TestNeighborIsRouterFirstDnsLabel(t *testing.T) {
	routers := []routerIdentity{
		{ID: "gw", Name: "gateway", Host: "192.168.1.1", BrMac: "AA:BB:CC:00:00:01"},
		{ID: "sw1", Name: "sw1.lan", Host: "192.168.1.10", BrMac: "AA:BB:CC:00:00:02"},
	}
	cases := []struct {
		name   string
		nb     *LldpNeighbor
		selfID string
		want   string // ID esperado o ""
	}{
		{
			name:   "chassis corto contra nombre con dominio",
			nb:     &LldpNeighbor{Chassis: "sw1"},
			selfID: "ap1",
			want:   "sw1",
		},
		{
			name:   "chassis con dominio contra nombre corto",
			nb:     &LldpNeighbor{Chassis: "SW1.lan"},
			selfID: "ap1",
			want:   "sw1",
		},
		{
			name:   "sin match no fuerza nada",
			nb:     &LldpNeighbor{Chassis: "moca-bridge"},
			selfID: "ap1",
			want:   "",
		},
		{
			name:   "self excluido",
			nb:     &LldpNeighbor{Chassis: "sw1"},
			selfID: "sw1",
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := neighborIsRouter(c.nb, routers, c.selfID)
			id := ""
			if got != nil {
				id = got.ID
			}
			if id != c.want {
				t.Errorf("neighborIsRouter = %q, want %q", id, c.want)
			}
		})
	}
}
