package routerstore

import "testing"

// gatewayIPFromRoutes elige la gateway LAN a partir de `ip -4 route show`.
// Los escenarios reproducen el reporte de la issue #1701: en on-box la via
// por defecto es la del ISP (publica o CGNAT) y la gateway LAN real es el
// propio router; en un servidor dedicado la via privada ES la gateway.
func TestGatewayIPFromRoutes(t *testing.T) {
	cases := []struct {
		name  string
		table string
		want  string
	}{
		{
			name: "dedicated server private via wins",
			table: `default via 192.168.1.1 dev eth0 proto static src 192.168.1.226
192.168.1.0/24 dev eth0 proto kernel scope link src 192.168.1.226
`,
			want: "192.168.1.1",
		},
		{
			name: "on-box CGNAT via falls back to LAN self IP",
			table: `default via 100.66.120.1 dev eth1
100.66.120.1 dev eth1 scope link src 100.66.120.7
192.168.1.0/24 dev br-lan proto kernel scope link src 192.168.1.1
`,
			want: "192.168.1.1",
		},
		{
			name: "on-box public via falls back to LAN self IP",
			table: `default via 81.34.12.7 dev eth1
81.34.12.7 dev eth1 scope link src 81.34.12.7
192.168.8.0/24 dev br-lan proto kernel scope link src 192.168.8.1
`,
			want: "192.168.8.1",
		},
		{
			name: "on-box pppoe default without via uses LAN self IP",
			table: `default dev pppoe-wan scope link
192.168.1.0/24 dev br-lan proto kernel scope link src 192.168.1.1
`,
			want: "192.168.1.1",
		},
		{
			name: "docker bridge keeps the RFC1918 gateway",
			// Limitacion conocida (#1701): desde un contenedor en red bridge
			// la unica gateway visible es la del puente; no se puede saber la
			// gateway real de la LAN del host.
			table: `default via 172.16.6.1 dev eth0
172.16.6.0/24 dev eth0 proto kernel scope link src 172.16.6.4
`,
			want: "172.16.6.1",
		},
		{
			name: "multi-WAN private via preferred over public",
			table: `default via 1.2.3.4 dev eth1 metric 1
default via 192.168.14.1 dev eth2 metric 20
192.168.14.0/24 dev eth2 proto kernel scope link src 192.168.14.10
`,
			want: "192.168.14.1",
		},
		{
			name:  "empty table",
			table: "",
			want:  "",
		},
		{
			name: "no private route anywhere keeps the default via",
			table: `default via 203.0.113.1 dev eth1
203.0.113.0/24 dev eth1 proto kernel scope link src 203.0.113.9
`,
			want: "203.0.113.1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gatewayIPFromRoutes([]byte(tc.table))
			if got != tc.want {
				t.Fatalf("gatewayIPFromRoutes() = %q, want %q", got, tc.want)
			}
		})
	}
}
