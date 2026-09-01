package adapters

import (
	"testing"
)

// TestDawnDeprecatedFromPolled verifica que la detección de DAWN se active
// cuando al menos un router reporta el dato en su payload (#426).
func TestDawnDeprecatedFromPolled(t *testing.T) {
	t.Run("sin DAWN", func(t *testing.T) {
		got := dawnDeprecatedFromPolled(map[string]*routerPolled{
			"r1": {dawnDetected: false},
			"r2": {dawnDetected: false},
		})
		if got {
			t.Errorf("dawnDeprecatedFromPolled = true, want false")
		}
	})

	t.Run("con DAWN en al menos un router", func(t *testing.T) {
		got := dawnDeprecatedFromPolled(map[string]*routerPolled{
			"r1": {dawnDetected: false},
			"r2": {dawnDetected: true},
		})
		if !got {
			t.Errorf("dawnDeprecatedFromPolled = false, want true")
		}
	})

	t.Run("mapa vacío", func(t *testing.T) {
		got := dawnDeprecatedFromPolled(map[string]*routerPolled{})
		if got {
			t.Errorf("dawnDeprecatedFromPolled = true, want false")
		}
	})
}
