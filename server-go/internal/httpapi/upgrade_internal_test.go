// upgrade_internal_test.go — #284: tests internos del upgradeTracker (TTL y
// concurrencia básica set/snapshot). Paquete interno para acceder a la API
// no exportada.
package httpapi

import (
	"sync"
	"testing"
	"time"
)

func TestUpgradeTrackerTTL(t *testing.T) {
	tr := newUpgradeTracker()
	base := time.Now()
	tr.now = func() time.Time { return base }

	// Paso caducado (TTL 3 min) → no se expone.
	tr.states["patio"] = upgradeState{Step: upgradeStepDownloading, Pct: 40, Ts: base.Add(-upgradeStateTTL - time.Second)}
	if _, ok := tr.snapshot("patio"); ok {
		t.Fatal("paso caducado no debe exponerse")
	}

	// Paso reciente → se expone tal cual.
	tr.states["patio"] = upgradeState{Step: upgradeStepDownloading, Pct: 40, Ts: base.Add(-time.Second)}
	st, ok := tr.snapshot("patio")
	if !ok {
		t.Fatal("paso reciente debe exponerse")
	}
	if st.Step != upgradeStepDownloading || st.Pct != 40 {
		t.Fatalf("snapshot: %+v, want downloading/40", st)
	}

	// Slug desconocido → no hay snapshot.
	if _, ok := tr.snapshot("nadie"); ok {
		t.Fatal("slug sin estado no debe exponerse")
	}
}

// TestUpgradeTrackerConcurrent: set y snapshot en paralelo no corren (guardado
// por mutex; -race lo pillaría).
func TestUpgradeTrackerConcurrent(t *testing.T) {
	tr := newUpgradeTracker()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tr.set("patio", upgradeStepDownloading, j%100, "")
				tr.snapshot("patio")
			}
		}(i)
	}
	wg.Wait()
}
