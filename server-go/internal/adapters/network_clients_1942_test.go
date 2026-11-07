package adapters

import (
	"reflect"
	"testing"
)

// #1942: un cliente cae en exactamente una red o en ninguna. Los casos
// cubren el br-lan sin VLAN (antes la columna quedaba vacia), el split
// lan/guest, el trunk que no se puede adivinar y la via WiFi.
func TestAttributeNetworkClients_1942(t *testing.T) {
	lan := NetworkInfo{Name: "lan", Device: "br-lan", Ports: []string{"lan1", "lan2", "lan3"}}
	guest := NetworkInfo{Name: "guest", Device: "br-guest", Vlan: 20, Ports: []string{"lan3"}}
	iot := NetworkInfo{Name: "iot", Device: "br-iot", Vlan: 999, Ports: []string{"eth0.999"}}
	cases := []struct {
		name     string
		networks []NetworkInfo
		clients  []Device
		wifiNet  map[string]string
		want     map[string][]string // red -> MACs; las redes sin entrada quedan sin el campo
	}{
		{
			name:     "br-lan sin VLAN atribuye por puerto",
			networks: []NetworkInfo{lan},
			clients: []Device{
				{MAC: "aa", Port: "lan2"},
				{MAC: "bb"}, // sin puerto ni VLAN: no se adivina
			},
			want: map[string][]string{"lan": {"aa"}},
		},
		{
			name:     "puerto trunk con VLAN desempata",
			networks: []NetworkInfo{lan, guest},
			clients:  []Device{{MAC: "aa", Port: "lan3", Vlan: 20}},
			want:     map[string][]string{"guest": {"aa"}},
		},
		{
			name:     "puerto trunk sin VLAN no atribuye",
			networks: []NetworkInfo{lan, guest},
			clients:  []Device{{MAC: "aa", Port: "lan3"}},
			want:     map[string][]string{},
		},
		{
			name:     "puerto exclusivo sin VLAN si atribuye",
			networks: []NetworkInfo{lan, guest},
			clients:  []Device{{MAC: "aa", Port: "lan1"}},
			want:     map[string][]string{"lan": {"aa"}},
		},
		{
			name:     "subinterfaz: base del puerto mas VLAN",
			networks: []NetworkInfo{iot},
			clients:  []Device{{MAC: "aa", Port: "eth0", Vlan: 999}},
			want:     map[string][]string{"iot": {"aa"}},
		},
		{
			name:     "VLAN sola, sin puerto, como antes",
			networks: []NetworkInfo{lan, guest},
			clients:  []Device{{MAC: "aa", Vlan: 20}},
			want:     map[string][]string{"guest": {"aa"}},
		},
		{
			name:     "wifi por config wireless",
			networks: []NetworkInfo{lan, guest},
			clients: []Device{
				{MAC: "aa", WifiIface: "wlan1"},
				{MAC: "bb", WifiIface: "wlan9"}, // iface no declarada: ninguna
			},
			wifiNet: map[string]string{"wlan1": "guest"},
			want:    map[string][]string{"guest": {"aa"}},
		},
		{
			name:     "MAC vacia nunca se lista",
			networks: []NetworkInfo{lan},
			clients:  []Device{{MAC: "", Port: "lan1"}},
			want:     map[string][]string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := attributeNetworkClients(tc.networks, tc.clients, tc.wifiNet)
			if len(got) != len(tc.networks) {
				t.Fatalf("redes devueltas = %d, quiero %d", len(got), len(tc.networks))
			}
			for _, n := range got {
				want := tc.want[n.Name]
				if !reflect.DeepEqual(n.ClientMacs, want) && !(len(n.ClientMacs) == 0 && len(want) == 0) {
					t.Errorf("red %s: ClientMacs = %v, quiero %v", n.Name, n.ClientMacs, want)
				}
			}
		})
	}
}

// La entrada no se modifica: el cache anti-parpadeo comparte el slice.
func TestAttributeNetworkClientsNoMutaEntrada(t *testing.T) {
	in := []NetworkInfo{{Name: "lan", Ports: []string{"lan1"}}}
	attributeNetworkClients(in, []Device{{MAC: "aa", Port: "lan1"}}, nil)
	if len(in[0].ClientMacs) != 0 {
		t.Fatalf("la entrada quedo modificada: %v", in[0].ClientMacs)
	}
}

func TestWifiNetworkMap(t *testing.T) {
	ifaces := []Dot11rIface{
		{Ifname: "wlan0", Network: "lan"},
		{Ifname: "wlan1", Network: "guest"},
		{Ifname: "", Network: "lan"},   // sin ifname no mapea
		{Ifname: "wlan2", Network: ""}, // sin red no mapea
	}
	got := wifiNetworkMap(ifaces)
	want := map[string]string{"wlan0": "lan", "wlan1": "guest"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wifiNetworkMap = %v, quiero %v", got, want)
	}
	if wifiNetworkMap([]Dot11rIface{{Ifname: "wlan0"}}) != nil {
		t.Fatal("sin mapeos utiles debe devolver nil")
	}
}
