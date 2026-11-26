package httpapi

import (
	"strings"
	"testing"
)

// TestFindDhcpHostMultiMACList comprueba que findDhcpHost reconoce una sección
// host con `list mac` (varias MACs del mismo equipo). `uci show dhcp` la saca
// como elementos entre comillas simples separados por espacio, y uciValue solo
// quita las comillas de fuera.
func TestFindDhcpHostMultiMACList(t *testing.T) {
	// Salida real de `uci show dhcp` para una reserva con `list mac`
	// (dos o tres interfaces del mismo equipo compartiendo IP).
	raw := strings.Join([]string{
		"dhcp.cfg0abc12=host",
		"dhcp.cfg0abc12.name='portatil'",
		"dhcp.cfg0abc12.mac='aa:bb:cc:00:00:02' 'aa:bb:cc:00:00:03' 'aa:bb:cc:00:00:04'",
		"dhcp.cfg0abc12.ip='192.168.1.50'",
	}, "\n")

	hosts := parseDhcpHosts(strings.Split(raw, "\n"))
	if len(hosts) != 1 {
		t.Fatalf("esperaba 1 sección host, obtuve %d", len(hosts))
	}

	cases := []struct {
		name string
		mac  string
	}{
		{"primera MAC de la lista", "aa:bb:cc:00:00:02"},
		{"MAC intermedia de la lista", "aa:bb:cc:00:00:03"},
		{"última MAC de la lista", "aa:bb:cc:00:00:04"},
		{"primera MAC en mayúsculas", "AA:BB:CC:00:00:02"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findDhcpHost(hosts, tc.mac)
			if got == nil {
				t.Fatalf("esperaba encontrar la sección cfg0abc12 para %s (h.MAC=%q), obtuve nil", tc.mac, hosts[0].MAC)
			}
			if got.Section != "cfg0abc12" || got.IP != "192.168.1.50" {
				t.Errorf("esperaba cfg0abc12 / 192.168.1.50, obtuve %+v", got)
			}
		})
	}
}
