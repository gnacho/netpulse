package httpapi

import (
	"testing"
	"time"
)

// NetgripTestFetch devuelve el resolver actual de la última release de
// NetGrip (hooks de test para httpapi_test, patrón export_test).
func NetgripTestFetch() func() string { return netgripLatestFetch }

// NetgripTestSetFetch sustituye el resolver y limpia la cache.
func NetgripTestSetFetch(f func() string) {
	netgripLatestMu.Lock()
	defer netgripLatestMu.Unlock()
	netgripLatestFetch = f
	netgripLatestVer = ""
	netgripLatestAt = time.Time{}
}

func TestNetgripLatestCacheAndCmp(t *testing.T) {
	orig := netgripLatestFetch
	t.Cleanup(func() {
		netgripLatestMu.Lock()
		netgripLatestFetch = orig
		netgripLatestVer = ""
		netgripLatestAt = time.Time{}
		netgripLatestMu.Unlock()
	})

	// cmpSemver: orden numérico por campo, tolerante a v/ y sufijos -rN.
	if got := cmpSemver("v0.22.1", "0.23.0"); got != -1 {
		t.Fatalf("cmpSemver(v0.22.1, 0.23.0) = %d, want -1", got)
	}
	if got := cmpSemver("0.23.0-r6", "0.23.0"); got != 0 {
		t.Fatalf("cmpSemver(0.23.0-r6, 0.23.0) = %d, want 0", got)
	}
	if got := cmpSemver("0.23.0", "v0.22.9"); got != 1 {
		t.Fatalf("cmpSemver(0.23.0, v0.22.9) = %d, want 1", got)
	}
	if got := cmpSemver("nonsense", "0.1.0"); got != 0 {
		t.Fatalf("cmpSemver malparseado = %d, want 0", got)
	}

	// netgripLatest usa la cache dentro del TTL aunque el fetch cambie.
	netgripLatestMu.Lock()
	netgripLatestFetch = func() string { return "1.2.3" }
	netgripLatestMu.Unlock()
	if v := netgripLatest(); v != "1.2.3" {
		t.Fatalf("netgripLatest tras fetch = %q, want 1.2.3", v)
	}
	netgripLatestMu.Lock()
	netgripLatestFetch = func() string { return "9.9.9" }
	netgripLatestMu.Unlock()
	if v := netgripLatest(); v != "1.2.3" {
		t.Fatalf("netgripLatest dentro del TTL = %q, want 1.2.3 (cache)", v)
	}
}

func TestNetgripLatestFetchFailVacia(t *testing.T) {
	orig := netgripLatestFetch
	t.Cleanup(func() {
		netgripLatestMu.Lock()
		netgripLatestFetch = orig
		netgripLatestVer = ""
		netgripLatestMu.Unlock()
	})
	netgripLatestMu.Lock()
	netgripLatestFetch = func() string { return "" }
	netgripLatestVer = ""
	netgripLatestAt = time.Time{}
	netgripLatestMu.Unlock()
	if v := netgripLatest(); v != "" {
		t.Fatalf("fetch roto debe dar vacío, got %q", v)
	}
	// Sin latest conocida, cmpSemver da 0 → updateAvailable false (sin botón).
	if cmpSemver(netgripLatest(), "0.23.0") > 0 {
		t.Fatal("latest vacía no debe marcar updateAvailable")
	}
}
