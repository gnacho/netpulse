// upgrade_restart_1900_test.go - issue #1900: el ciclo del upgrade tiene que
// cerrarse. Si el agente intercambia el binario y no consigue reiniciarse, el
// paso "restarting" no puede quedarse ahí mientras la UI dice que la unidad
// está al día.
package httpapi

import (
	"strings"
	"testing"
	"time"
)

func TestUpgradeTrackerRestartingExpiresToFailure(t *testing.T) {
	u := newUpgradeTracker()
	now := time.Now()
	u.now = func() time.Time { return now }

	u.begin("asus", "3.0.36")
	u.set("asus", upgradeStepRestarting, 0, "")

	// Dentro de la ventana de gracia el paso sigue vivo: el binario nuevo
	// necesita un momento para conectar y empujar su versión.
	now = now.Add(upgradeRestartGrace - time.Second)
	if st, ok := u.snapshot("asus"); !ok || st.Step != upgradeStepRestarting {
		t.Fatalf("dentro del margen debe seguir en restarting: %+v ok=%v", st, ok)
	}

	now = now.Add(2 * time.Second)
	st, ok := u.snapshot("asus")
	if !ok {
		t.Fatal("el fallo debe seguir visible para el operador")
	}
	if st.Step != upgradeStepRestartFail {
		t.Fatalf("step = %q, want %q", st.Step, upgradeStepRestartFail)
	}
	if !strings.Contains(st.Error, "binario anterior") {
		t.Fatalf("error = %q, quiero el motivo del estado", st.Error)
	}
}

// Un reinicio a mano cierra el ciclo: el push de la versión objetivo vuelve a
// marcar "done" aunque el estado haya pasado por restart_failed.
func TestUpgradeTrackerRestartFailClosesOnTargetPush(t *testing.T) {
	u := newUpgradeTracker()
	now := time.Now()
	u.now = func() time.Time { return now }

	u.begin("asus", "3.0.36")
	u.set("asus", upgradeStepRestarting, 0, "")
	now = now.Add(upgradeRestartGrace + time.Second)
	if st, _ := u.snapshot("asus"); st.Step != upgradeStepRestartFail {
		t.Fatalf("paso esperado restart_failed: %+v", st)
	}

	if !u.finishIfTarget("asus", "3.0.36") {
		t.Fatal("el push de la versión objetivo debe cerrar el ciclo")
	}
	if st, _ := u.snapshot("asus"); st.Step != upgradeStepDone {
		t.Fatalf("paso final = %q, want done", st.Step)
	}
}

// El fallo vive más que el TTL general: un operador que mira la flota unos
// minutos después tiene que verlo, no un hueco vacío.
func TestUpgradeTrackerRestartFailOutlivesTheGeneralTTL(t *testing.T) {
	u := newUpgradeTracker()
	now := time.Now()
	u.now = func() time.Time { return now }

	u.begin("asus", "3.0.36")
	u.set("asus", upgradeStepRestarting, 0, "")
	now = now.Add(upgradeStateTTL + time.Minute)
	if st, ok := u.snapshot("asus"); !ok || st.Step != upgradeStepRestartFail {
		t.Fatalf("pasado el TTL general el fallo debe seguir: %+v ok=%v", st, ok)
	}

	now = now.Add(upgradeFailedTTL)
	if _, ok := u.snapshot("asus"); ok {
		t.Fatal("pasado su propio TTL el estado debe desaparecer")
	}
}
