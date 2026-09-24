// alerts_persist_test.go — persistencia del log de alertas en SQLite (#798).
// El log sobrevive a reinicios (el self-update del updater reinicia el
// proceso), reconstruye el dedup y restaura el read-state espejado.
package alerts

import (
	"fmt"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func newPersistTestEngine(t *testing.T, dataDir string, volatile bool) *Engine {
	t.Helper()
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if volatile {
		return NewVolatile(d, nil)
	}
	return New(d, nil)
}

func TestPersistReload(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistTestEngine(t, dir, false)
	ev := AlertEvent{
		ID: "a1", Category: CatSystem, Urgent: true, Severity: "info",
		Title: "Auto-actualización programada", Description: "desc",
		Type: "autoupdate", Vars: map[string]string{"result": "applied"},
		Ts: time.Now().Unix(),
	}
	if !e1.Emit(ev) {
		t.Fatal("Emit no guardó el evento")
	}

	e2 := newPersistTestEngine(t, dir, false)
	list := e2.List()
	if len(list) != 1 {
		t.Fatalf("se esperaba 1 alerta tras recargar, hay %d", len(list))
	}
	got := list[0]
	if got.ID != "a1" || got.Title != ev.Title || got.Description != "desc" ||
		got.Type != "autoupdate" || !got.Urgent || got.Severity != "info" {
		t.Errorf("alerta recargada incompleta: %+v", got)
	}
	if got.Vars["result"] != "applied" {
		t.Errorf("vars no restaurados: %+v", got.Vars)
	}
	// Dedup reconstruido: reemitir la misma clave dentro de la ventana se
	// rechaza aunque sea un motor recién arrancado.
	if e2.Emit(ev) {
		t.Error("el dedup no sobrevivió al reinicio (reemisión aceptada)")
	}
}

func TestPersistReadMirrored(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistTestEngine(t, dir, false)
	e1.Emit(AlertEvent{ID: "r1", Category: CatSystem, Title: "t", Ts: time.Now().Unix()})
	e1.MarkRead("r1")

	e2 := newPersistTestEngine(t, dir, false)
	list := e2.List()
	if len(list) != 1 || !list[0].Read {
		t.Errorf("el read no se restauró: %+v", list)
	}
}

// Remove (#846): el evento desaparece de la lista, del read-set y de su
// espejo en alert_log (un reinicio no lo resucita). Los demás eventos y
// reads no se tocan.
func TestRemoveDeletesEverywhere(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistTestEngine(t, dir, false)
	now := time.Now().Unix()
	e1.Emit(AlertEvent{ID: "keep", Category: CatSystem, Title: "t", Ts: now})
	e1.Emit(AlertEvent{ID: "drop", Category: CatSystem, Title: "t2", Ts: now})
	e1.MarkRead("drop")
	e1.Remove("drop", "")

	if list := e1.List(); len(list) != 1 || list[0].ID != "keep" {
		t.Fatalf("lista tras Remove: %+v", list)
	}
	if e1.UnreadCount() != 1 {
		t.Fatalf("unread tras Remove: %d", e1.UnreadCount())
	}
	if e1.readSet["drop"] {
		t.Fatal("el read-set debía olvidar el ID eliminado")
	}

	e2 := newPersistTestEngine(t, dir, false)
	list := e2.List()
	if len(list) != 1 || list[0].ID != "keep" {
		t.Fatalf("alert_log no se limpió: %+v", list)
	}
}

func TestEmitOrUpdatePersistsMoveToFront(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistTestEngine(t, dir, false)
	now := time.Now()
	e1.SetClock(func() time.Time { return now })
	e1.EmitOrUpdate(AlertEvent{ID: "u1", Category: CatRouter, Urgent: true, Title: "t", Ts: now.Unix()})
	later := now.Add(10 * time.Minute)
	e1.SetClock(func() time.Time { return later })
	e1.EmitOrUpdate(AlertEvent{ID: "u1", Category: CatRouter, Urgent: true, Title: "t2", Ts: later.Unix()})

	e2 := newPersistTestEngine(t, dir, false)
	list := e2.List()
	if len(list) != 1 || list[0].Title != "t2" || list[0].Ts != later.Unix() {
		t.Errorf("la actualización no persistió: %+v", list)
	}
}

func TestPersistPrune(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistTestEngine(t, dir, false)
	base := time.Now().Add(-time.Hour)
	for i := 0; i < PersistMaxEvents+50; i++ {
		e1.SetClock(func() time.Time { return base.Add(time.Duration(i) * time.Second) })
		e1.Emit(AlertEvent{
			ID:       fmt.Sprintf("prune-%04d", i),
			Category: CatSystem, Title: fmt.Sprintf("t-%04d", i), Ts: base.Add(time.Duration(i) * time.Second).Unix(),
		})
	}
	var n int
	if err := e1.db.QueryRow("SELECT COUNT(*) FROM alert_log").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != PersistMaxEvents {
		t.Errorf("alert_log tiene %d filas, se esperaban %d", n, PersistMaxEvents)
	}
	// La lista en memoria sigue acotada a MaxEvents.
	if got := len(e1.List()); got != MaxEvents {
		t.Errorf("lista en memoria tiene %d eventos, se esperaban %d", got, MaxEvents)
	}
}

func TestVolatileDoesNotPersist(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistTestEngine(t, dir, true)
	e1.Emit(AlertEvent{ID: "v1", Category: CatSystem, Title: "t", Ts: time.Now().Unix()})

	e2 := newPersistTestEngine(t, dir, true)
	if got := len(e2.List()); got != 0 {
		t.Errorf("motor volatile restauró %d alertas, se esperaban 0", got)
	}
	var n int
	if err := e2.db.QueryRow("SELECT COUNT(*) FROM alert_log").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("alert_log tiene %d filas con motor volatile", n)
	}
}
