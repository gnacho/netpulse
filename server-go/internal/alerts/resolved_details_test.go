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
	// #1659: signal arranca en "none" (oculta la categoría), este test la usa.
	if err := e.SetConfig(map[string]string{CatSignal: LevelAll}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
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
	// #1659: signal arranca en "none" (oculta la categoría), este test la usa.
	if err := e.SetConfig(map[string]string{CatSignal: LevelAll}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
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

// #1466: `resolves` sobrevive al reinicio (se persiste en alert_log). Sin
// esto, tras cada reinicio del server los cierres perdían sus referencias y
// el bloque de resueltos desaparecía del log.
func TestResolvesSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistTestEngine(t, dir, false)
	if err := e1.SetConfig(map[string]string{CatSignal: LevelAll}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	orig := AlertEvent{
		ID: "alert-weak-x", Category: CatSignal, Severity: "warn",
		Title: "Señal débil", Type: "wifi-weak", RouterID: "rt4",
	}
	if !e1.Emit(orig) {
		t.Fatal("no emitió el original")
	}
	if !e1.Emit(AlertEvent{
		ID: "alert-weak-x-ok", Category: CatSignal, Severity: "ok",
		Title: "Recuperada", RouterID: "rt4", Resolves: []string{"alert-weak-x"},
	}) {
		t.Fatal("no emitió el cierre")
	}

	e2 := newPersistTestEngine(t, dir, false)
	for _, ev := range e2.List() {
		if ev.ID != "alert-weak-x-ok" {
			continue
		}
		if len(ev.Resolved) != 1 || ev.Resolved[0].ID != "alert-weak-x" || ev.Resolved[0].Title != "Señal débil" {
			t.Fatalf("tras reiniciar, resolved: %+v (resolves=%v)", ev.Resolved, ev.Resolves)
		}
		return
	}
	t.Fatal("el evento de cierre no volvió tras el reinicio")
}

// #1466 (reabierto por David en v2.50): el flujo real resuelve el aviso
// original ANTES de que la UI pida el feed, y Resolve() lo retira de la
// lista viva. El test anterior nunca llamaba a Resolve, por eso el detalle
// salía en las pruebas y en producción solo quedaba el id.
func TestListResolvedDetailsAfterResolve(t *testing.T) {
	e := newPersistTestEngine(t, t.TempDir(), false)
	if err := e.SetConfig(map[string]string{CatSignal: LevelAll}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	orig := AlertEvent{
		ID: "alert-weak-64:9D:38:2E:E1:E7-1792538956947", Category: CatSignal,
		Severity: "warn", Title: "Señal WiFi débil en Pixel", Type: "wifi-weak",
		Vars: map[string]string{"device": "Pixel"}, RouterID: "rt4", Ts: 1_792_538_956,
	}
	if !e.Emit(orig) {
		t.Fatal("no emitió el original")
	}
	// La condición se recupera: el motor retira el aviso de la lista viva y
	// deja la fila en alert_log marcada como resuelta.
	e.Resolve(orig.ID)
	if !e.Emit(AlertEvent{
		ID: "alert-weak-64:9D:38:2E:E1:E7-ok", Category: CatSignal, Severity: "ok",
		Title: "Señal WiFi recuperada", Type: "wifi-weak-recovered", RouterID: "rt4",
		Resolves: []string{orig.ID},
	}) {
		t.Fatal("no emitió el cierre")
	}

	for _, ev := range e.List() {
		if ev.ID != "alert-weak-64:9D:38:2E:E1:E7-ok" {
			continue
		}
		if len(ev.Resolved) != 1 {
			t.Fatalf("resolved: %d entradas, esperaba 1", len(ev.Resolved))
		}
		r := ev.Resolved[0]
		if r.Title != orig.Title {
			t.Fatalf("título del resuelto = %q, esperaba %q (solo sobrevivía el id)", r.Title, orig.Title)
		}
		if r.Ts != orig.Ts || r.Type != orig.Type {
			t.Fatalf("resuelto incompleto: %+v", r)
		}
		if r.Vars["device"] != "Pixel" {
			t.Fatalf("vars no recuperadas: %+v", r.Vars)
		}
		return
	}
	t.Fatal("el cierre no está en el feed")
}
