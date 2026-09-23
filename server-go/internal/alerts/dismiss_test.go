// dismiss_test.go — issue #833: limpiar alerta (Dismiss) y auto-resolución
// (Resolve) en el motor de alertas.
package alerts

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestDismissRemovesFromFeedAndUnread(t *testing.T) {
	e := New(nil, nil)
	e.Emit(ev("a1", CatSystem, false))
	e.Emit(ev("a2", CatSystem, false))
	e.Dismiss("a1")
	if got := len(e.List()); got != 1 {
		t.Fatalf("lista tras dismiss: %d, esperaba 1", got)
	}
	for _, x := range e.List() {
		if x.ID != "a2" {
			t.Fatalf("alerta restante: %s", x.ID)
		}
	}
	if got := e.UnreadCount(); got != 1 {
		t.Fatalf("unread tras dismiss: %d, esperaba 1", got)
	}
	// Reaparece si la condición se re-dispara (fuera de la ventana de dedup).
	e.SetClock(func() time.Time { return time.Now().Add(6 * time.Minute) })
	e.Emit(ev("a1", CatSystem, false))
	if got := len(e.List()); got != 2 {
		t.Fatalf("lista tras re-emitir: %d, esperaba 2", got)
	}
}

func TestDismissPersistsAcrossRestart(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	e := New(d, nil)
	e.Emit(ev("a1", CatSystem, false))
	e.Emit(ev("a2", CatSystem, false))
	e.Dismiss("a1")

	// Motor nuevo sobre la misma DB: la alerta limpiada NO vuelve.
	e2 := New(d, nil)
	for _, x := range e2.List() {
		if x.ID == "a1" {
			t.Fatal("a1 dismiss reapareció tras reinicio")
		}
	}
	if got := len(e2.List()); got != 1 {
		t.Fatalf("lista tras reinicio: %d, esperaba 1", got)
	}
}

func TestResolveRemovesAndAllowsImmediateReEmit(t *testing.T) {
	e := New(nil, nil)
	e.Emit(ev("a1", CatSystem, false))
	e.Resolve("a1")
	if got := len(e.List()); got != 0 {
		t.Fatalf("lista tras resolve: %d, esperaba 0", got)
	}
	// A diferencia del dedup, Resolve permite re-alertar de inmediato
	// (la reincidencia es un incidente nuevo).
	e.Emit(ev("a1", CatSystem, false))
	if got := len(e.List()); got != 1 {
		t.Fatalf("lista tras re-emitir: %d, esperaba 1", got)
	}
}

func TestResolvePersistsAcrossRestart(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	e := New(d, nil)
	e.Emit(ev("a1", CatSystem, false))
	e.Resolve("a1")

	e2 := New(d, nil)
	if got := len(e2.List()); got != 0 {
		t.Fatalf("lista tras reinicio: %d, esperaba 0", got)
	}
}

func TestHas(t *testing.T) {
	e := New(nil, nil)
	if e.Has("a1") {
		t.Fatal("Has con lista vacía")
	}
	e.Emit(ev("a1", CatSystem, false))
	if !e.Has("a1") {
		t.Fatal("Has tras emit")
	}
	e.Resolve("a1")
	if e.Has("a1") {
		t.Fatal("Has tras resolve")
	}
}
