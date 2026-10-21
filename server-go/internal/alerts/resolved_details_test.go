// resolved_details_test.go — #1466: el evento de cierre lleva en el payload
// los datos del evento que resolvió (título, ts, type y vars), para que la
// UI no dependa de tenerlo en la página actual del feed (paginado a 20).
package alerts

import (
	"testing"
	"time"
)

func TestListAttachesResolvedDetails(t *testing.T) {
	e := New(nil, nil)
	clock := time.Unix(1_700_000_000, 0)
	e.SetClock(func() time.Time { return clock })

	orig := AlertEvent{
		ID: "alert-weak-aa:bb", Category: CatSignal, Severity: "warn",
		Title: "Señal WiFi débil en Pixel", Type: "wifi-weak",
		Vars: map[string]string{"mac": "aa:bb"}, RouterID: "rt4",
	}
	if !e.Emit(orig) {
		t.Fatal("no emitió el evento original")
	}

	// Unos minutos después llega el evento de cierre.
	clock = clock.Add(7 * time.Minute)
	closeEv := AlertEvent{
		ID: "wifi-weak-recovered-aa:bb", Category: CatSignal, Severity: "ok",
		Title: "Señal WiFi recuperada", Type: "wifi-weak-recovered", RouterID: "rt4",
		Resolves: []string{"alert-weak-aa:bb"},
	}
	if !e.Emit(closeEv) {
		t.Fatal("no emitió el evento de cierre")
	}

	var got, stored *AlertEvent
	for _, ev := range e.List() {
		switch ev.ID {
		case closeEv.ID:
			c := ev
			got = &c
		case orig.ID:
			c := ev
			stored = &c
		}
	}
	if got == nil || stored == nil {
		t.Fatalf("faltan eventos en el feed (cierre=%v original=%v)", got != nil, stored != nil)
	}
	if len(got.Resolved) != 1 {
		t.Fatalf("resolved: %d entradas, esperaba 1", len(got.Resolved))
	}
	r := got.Resolved[0]
	if r.ID != orig.ID || r.Title != stored.Title || r.Ts != stored.Ts || r.Type != stored.Type {
		t.Fatalf("resolved: %+v, esperaba id/título/ts/type del original %+v", r, stored)
	}
	if r.Ts == 0 {
		t.Fatal("el ts del evento original no se propagó")
	}
	if r.Vars["mac"] != "aa:bb" {
		t.Fatalf("vars no propagadas: %+v", r.Vars)
	}
}

// Un ID desconocido (evento podado o nunca visto) no rompe: queda la
// referencia desnuda con el ID.
func TestListResolvedUnknownID(t *testing.T) {
	e := New(nil, nil)
	if !e.Emit(AlertEvent{
		ID: "close-x", Category: CatSignal, Severity: "ok", Title: "cierre",
		RouterID: "rt4", Resolves: []string{"alert-que-ya-no-esta"},
	}) {
		t.Fatal("no emitió")
	}
	for _, ev := range e.List() {
		if ev.ID != "close-x" {
			continue
		}
		if len(ev.Resolved) != 1 || ev.Resolved[0].ID != "alert-que-ya-no-esta" || ev.Resolved[0].Title != "" {
			t.Fatalf("resolved inesperado: %+v", ev.Resolved)
		}
		return
	}
	t.Fatal("el evento de cierre no está en el feed")
}
