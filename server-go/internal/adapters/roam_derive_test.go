// roam_derive_test.go — roams derivados de la atribución wireless (#1740).
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func countRoamEvents(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM roam_events").Scan(&n); err != nil {
		t.Fatalf("count roam_events: %v", err)
	}
	return n
}

func polledWithWireless(routerID, mac, band string) map[string]*routerPolled {
	return map[string]*routerPolled{
		routerID: {
			cfg:      RouterConfig{ID: routerID, Name: routerID, Host: "192.168.1.2"},
			wireless: map[string]WirelessClient{mac: {SignalDbm: -55, Band: band}},
		},
	}
}

// TestBuildDevicesDerivesRoamBetweenRouters: la primera vez que se ve una MAC
// solo se aprende la atribución. Cuando el ciclo siguiente la ve en otro AP se
// emite un 'connected', y repetir el mismo AP no emite nada.
func TestBuildDevicesDerivesRoamBetweenRouters(t *testing.T) {
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)
	const mac = "AA:BB:CC:DD:EE:FF"

	l.buildDevices(polledWithWireless("patio", mac, "5 GHz"), "")
	if n := countRoamEvents(t, d); n != 0 {
		t.Fatalf("el primer ciclo no debe emitir roams, got %d", n)
	}

	l.buildDevices(polledWithWireless("salon", mac, "5 GHz"), "")
	if n := countRoamEvents(t, d); n != 1 {
		t.Fatalf("un salto de AP debe emitir 1 evento, got %d", n)
	}
	var routerID, typ, detail string
	if err := d.QueryRow("SELECT router_id, type, detail FROM roam_events").Scan(&routerID, &typ, &detail); err != nil {
		t.Fatalf("leer roam_events: %v", err)
	}
	if routerID != "salon" || typ != "connected" {
		t.Fatalf("evento inesperado: router=%q type=%q detail=%q", routerID, typ, detail)
	}
	if detail != "roamed from patio to salon" {
		t.Fatalf("detail inesperado: %q", detail)
	}

	l.buildDevices(polledWithWireless("salon", mac, "5 GHz"), "")
	if n := countRoamEvents(t, d); n != 1 {
		t.Fatalf("sin cambio de AP no debe emitir, got %d", n)
	}
}

// TestBuildDevicesRoamBandChangeAndEcho: cambiar de banda en el mismo AP cuenta
// como reasociación, y el rebote inmediato de vuelta no suma (anti-eco).
func TestBuildDevicesRoamBandChangeAndEcho(t *testing.T) {
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)
	const mac = "AA:BB:CC:DD:EE:11"

	l.buildDevices(polledWithWireless("patio", mac, "5 GHz"), "")
	l.buildDevices(polledWithWireless("patio", mac, "2.4 GHz"), "")
	if n := countRoamEvents(t, d); n != 1 {
		t.Fatalf("el cambio de banda debe emitir 1 evento, got %d", n)
	}

	l.buildDevices(polledWithWireless("patio", mac, "5 GHz"), "")
	if n := countRoamEvents(t, d); n != 1 {
		t.Fatalf("el eco de banda no debe emitir, got %d", n)
	}
}

// TestBuildDevicesRoamAmbiguousAcrossRouters: si dos APs reportan la misma MAC
// en el mismo ciclo, el orden de iteración del mapa decide la atribución, así
// que no se emite nada.
func TestBuildDevicesRoamAmbiguousAcrossRouters(t *testing.T) {
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)
	const mac = "AA:BB:CC:DD:EE:22"

	l.buildDevices(polledWithWireless("patio", mac, "5 GHz"), "")
	l.buildDevices(map[string]*routerPolled{
		"patio": {
			cfg:      RouterConfig{ID: "patio", Name: "patio"},
			wireless: map[string]WirelessClient{mac: {SignalDbm: -60, Band: "5 GHz"}},
		},
		"salon": {
			cfg:      RouterConfig{ID: "salon", Name: "salon"},
			wireless: map[string]WirelessClient{mac: {SignalDbm: -55, Band: "5 GHz"}},
		},
	}, "")
	if n := countRoamEvents(t, d); n != 0 {
		t.Fatalf("una MAC en dos APs en el mismo ciclo no debe emitir, got %d", n)
	}
}
