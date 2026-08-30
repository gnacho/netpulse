// upgrade_finish_test.go - issue #401: el primer push con la versión objetivo
// cierra el ciclo del upgrade (paso terminal done, TTL corto) en vez de
// dejar el estado en "restarting" hasta el TTL general.
package httpapi

import (
	"testing"
	"time"
)

func TestUpgradeTrackerFinishIfTarget(t *testing.T) {
	u := newUpgradeTracker()
	now := time.Now()
	u.now = func() time.Time { return now }

	u.begin("gateway", "2.21.0")
	if st, _ := u.snapshot("gateway"); st.Step != upgradeStepRequested || st.Target != "2.21.0" {
		t.Fatalf("begin: %+v", st)
	}
	u.set("gateway", upgradeStepRestarting, 0, "")

	// Versión aún vieja: el ciclo sigue abierto.
	if u.finishIfTarget("gateway", "2.20.0") {
		t.Fatal("una versión menor no debe cerrar el ciclo")
	}
	if st, _ := u.snapshot("gateway"); st.Step != upgradeStepRestarting {
		t.Fatalf("debe seguir en restarting: %+v", st)
	}

	// El push con la versión objetivo cierra el ciclo.
	if !u.finishIfTarget("gateway", "2.21.0") {
		t.Fatal("la versión objetivo debe cerrar el ciclo")
	}
	st, ok := u.snapshot("gateway")
	if !ok || st.Step != upgradeStepDone {
		t.Fatalf("paso terminal esperado: %+v ok=%v", st, ok)
	}
	// Segundo cierre: ya está cerrado.
	if u.finishIfTarget("gateway", "2.21.0") {
		t.Fatal("no debe cerrar dos veces")
	}

	// TTL corto del done: caduca en <1 min.
	now = now.Add(upgradeDoneTTL + time.Second)
	if _, ok := u.snapshot("gateway"); ok {
		t.Fatal("done debe caducar pronto")
	}
}

func TestUpgradeTrackerFinishPrefixesAndStates(t *testing.T) {
	u := newUpgradeTracker()
	now := time.Now()
	u.now = func() time.Time { return now }

	// Prefijo v/ y -rN del lado del agente (netgrip reporta "v0.26.1").
	u.begin("redmi-ax6-2", "0.26.1")
	u.set("redmi-ax6-2", upgradeStepRequested, 0, "")
	if !u.finishIfTarget("redmi-ax6-2", "v0.26.1") {
		t.Fatal("v0.26.1 debe cerrar contra target 0.26.1")
	}

	// Sin target (estados escritos a mano, upgrades legados): nunca cierra.
	u.set("solo", upgradeStepRestarting, 0, "")
	if u.finishIfTarget("solo", "9.9.9") {
		t.Fatal("sin target no debe cerrar")
	}

	// queued y failed no se cierran por push.
	u2 := newUpgradeTracker()
	u2.now = func() time.Time { return now }
	u2.begin("q", "1.0.0")
	u2.queue("q")
	if u2.finishIfTarget("q", "1.0.0") {
		t.Fatal("queued no debe cerrar (el comando aún no salió)")
	}
	u2.begin("f", "1.0.0")
	u2.set("f", upgradeStepFailed, 0, "boom")
	if u2.finishIfTarget("f", "1.0.0") {
		t.Fatal("failed es terminal, no se cierra")
	}

	// Versión vacía (agente sin versión): nunca cierra.
	u3 := newUpgradeTracker()
	u3.now = func() time.Time { return now }
	u3.begin("e", "1.0.0")
	if u3.finishIfTarget("e", "") {
		t.Fatal("versión vacía no debe cerrar")
	}
}
