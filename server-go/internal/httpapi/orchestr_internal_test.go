package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/orchestr"
)

// TestInvertDesiredAdGuard: el inverso de enabled=true es enabled=false (y
// viceversa); el resto de campos se conservan.
func TestInvertDesiredAdGuard(t *testing.T) {
	cases := []struct {
		name string
		in   bool
		want bool
	}{
		{"enabled_to_disabled", true, false},
		{"disabled_to_enabled", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, _ := json.Marshal(orchestr.AdGuardDesired{
				Enabled: c.in, Port: "3000", UpstreamDNS: "1.1.1.1",
			})
			out, err := invertDesired("adguard", in)
			if err != nil {
				t.Fatalf("invertDesired err: %v", err)
			}
			var got orchestr.AdGuardDesired
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("unmarshal err: %v", err)
			}
			if got.Enabled != c.want {
				t.Errorf("Enabled: in=%v want=%v got=%v", c.in, c.want, got.Enabled)
			}
			// El resto de campos se conservan (no se pierden al invertir).
			if got.Port != "3000" || got.UpstreamDNS != "1.1.1.1" {
				t.Errorf("campos no conservados: port=%q dns=%q", got.Port, got.UpstreamDNS)
			}
		})
	}
}

// TestInvertDesiredUnsupported: un módulo que no sabe invertirse devuelve error.
func TestInvertDesiredUnsupported(t *testing.T) {
	_, err := invertDesired("wireguard", json.RawMessage(`{}`))
	if err == nil {
		t.Error("esperaba error para módulo no soportado")
	}
}

// TestInvertDesiredInvalidJSON: JSON malformado devuelve error (no panic).
func TestInvertDesiredInvalidJSON(t *testing.T) {
	_, err := invertDesired("adguard", json.RawMessage(`{bad json`))
	if err == nil {
		t.Error("esperaba error para JSON inválido")
	}
}
