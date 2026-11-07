package roamevents

import "testing"

// TestRouterHostEligible (#1944): el collector abre SSH en cada ciclo, así que
// la lista tiene que excluir lo que no corre logread. Antes solo se miraba
// AgentOnly y las unidades SNMP-only del despliegue recibían un dial SSH por
// minuto que el equipo registraba como handshake fallido.
func TestRouterHostEligible(t *testing.T) {
	cases := []struct {
		name string
		host RouterHost
		want bool
	}{
		{"openwrt sin tipo", RouterHost{Host: "rt1.lan"}, true},
		{"openwrt", RouterHost{Host: "rt1.lan", Type: "openwrt"}, true},
		{"glinet", RouterHost{Host: "flint2.lan", Type: "glinet"}, true},
		{"managed-switch SNMP-only", RouterHost{Host: "sw1.lan", Type: "managed-switch"}, false},
		{"external", RouterHost{Host: "ext.example", Type: "external"}, false},
		{"agent-only", RouterHost{Host: "rt2.lan", AgentOnly: true}, false},
		{"pausada", RouterHost{Host: "rt3.lan", Disabled: true}, false},
		{"sin host", RouterHost{Type: "openwrt"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.host.Eligible(); got != tc.want {
				t.Fatalf("Eligible() = %v, want %v", got, tc.want)
			}
		})
	}
}
