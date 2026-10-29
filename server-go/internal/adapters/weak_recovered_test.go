// weak_recovered_test.go - el cierre de señal débil colapsa por dispositivo
// (#1518): el cierre lleva ID estable por MAC y va con EmitOrUpdate, así que
// un episodio nuevo REEMPLAZA la entrada anterior en el log en vez de apilar
// otra "resuelta" idéntica. La referencia Resolves pasa a apuntar al último
// episodio cerrado.
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

func TestEmitWeakRecoveredCollapsesPerDevice(t *testing.T) {
	eng := alerts.New(nil, nil)
	// #1659: signal arranca en "none" (la categoría no entra al feed), este
	// test trabaja con cierres de señal.
	if err := eng.SetConfig(map[string]string{alerts.CatSignal: alerts.LevelAll}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	l := &Live{engine: eng}

	sig := -58
	d := &Device{MAC: "AA:BB:CC:DD:EE:01", Name: "dev-a", SignalDbm: &sig, RouterID: "rt1"}
	l.emitWeakRecovered(d, "salon", "alert-weak-AA:BB:CC:DD:EE:01-1000", 1000, 61_000)
	l.emitWeakRecovered(d, "salon", "alert-weak-AA:BB:CC:DD:EE:01-2000", 2000, 3_060_000)

	events := eng.List()
	if len(events) != 1 {
		t.Fatalf("dos episodios del mismo dispositivo: esperaba 1 cierre en el log, hay %d: %+v", len(events), events)
	}
	ev := events[0]
	if ev.Type != alerts.TypeWifiWeakRecovered {
		t.Errorf("type = %q, esperaba %q", ev.Type, alerts.TypeWifiWeakRecovered)
	}
	if want := "alert-weak-AA:BB:CC:DD:EE:01-ok"; ev.ID != want {
		t.Errorf("id = %q, esperaba el ID estable %q", ev.ID, want)
	}
	if len(ev.Resolves) != 1 || ev.Resolves[0] != "alert-weak-AA:BB:CC:DD:EE:01-2000" {
		t.Errorf("resolves = %v, esperaba la referencia al último episodio cerrado", ev.Resolves)
	}
	if ev.Vars["device"] != "dev-a" || ev.Vars["signal"] != "-58" || ev.Vars["router"] != "salon" {
		t.Errorf("vars = %v, esperaba el último estado del episodio", ev.Vars)
	}

	// Un dispositivo distinto sigue teniendo su propia entrada.
	sig2 := -62
	d2 := &Device{MAC: "AA:BB:CC:DD:EE:02", Name: "dev-b", SignalDbm: &sig2, RouterID: "rt1"}
	l.emitWeakRecovered(d2, "salon", "alert-weak-AA:BB:CC:DD:EE:02-3000", 3000, 6_060_000)
	if got := len(eng.List()); got != 2 {
		t.Fatalf("cerrar un segundo dispositivo: esperaba 2 cierres (uno por MAC), hay %d", got)
	}
}
