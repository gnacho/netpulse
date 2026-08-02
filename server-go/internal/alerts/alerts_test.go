// alerts_test.go — tests del motor (SPEC-ALERTAS §2-4): defaults, validación,
// filtrado none/urgent/all en creación, dedup 5 min, cap 100, read-state
// (FIFO 200, persistencia kv) y hook Notifier.
package alerts

import (
	"fmt"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func ev(id, cat string, urgent bool) AlertEvent {
	return AlertEvent{ID: id, Category: cat, Urgent: urgent, Severity: "warn",
		Title: "t-" + id, RouterID: "r1"}
}

func TestDefaultConfig(t *testing.T) {
	e := New(nil, nil)
	cfg := e.Config()
	want := DefaultConfig()
	if len(cfg) != 6 {
		t.Fatalf("config: %d claves, esperaba 6", len(cfg))
	}
	for _, cat := range Categories {
		if cfg[cat] != want[cat] {
			t.Fatalf("default %s: %q, esperaba %q", cat, cfg[cat], want[cat])
		}
	}
	// Defaults exactos del SPEC §2
	if cfg[CatRouter] != "urgent" || cfg[CatInternet] != "urgent" ||
		cfg[CatClients] != "urgent" || cfg[CatSignal] != "none" ||
		cfg[CatVPN] != "none" || cfg[CatSystem] != "all" {
		t.Fatalf("defaults SPEC: %v", cfg)
	}
}

func TestSetConfigPartialAndValidation(t *testing.T) {
	e := New(nil, nil)
	if err := e.SetConfig(map[string]string{"vpn": "all"}); err != nil {
		t.Fatalf("SetConfig parcial: %v", err)
	}
	cfg := e.Config()
	if cfg["vpn"] != "all" {
		t.Fatalf("vpn: %q", cfg["vpn"])
	}
	// El resto sigue en defaults
	if cfg["router"] != "urgent" || cfg["signal"] != "none" {
		t.Fatalf("merge parcial rompió defaults: %v", cfg)
	}
	// Categoría desconocida → error
	if err := e.SetConfig(map[string]string{"wifi": "all"}); err == nil {
		t.Fatal("categoría desconocida debía fallar")
	}
	// Nivel inválido → error
	if err := e.SetConfig(map[string]string{"vpn": "todo"}); err == nil {
		t.Fatal("nivel inválido debía fallar")
	}
	// Y no deja la config a medias
	if e.Config()["vpn"] != "all" {
		t.Fatalf("config tras error: %v", e.Config())
	}
}

func TestEmitFiltering(t *testing.T) {
	e := New(nil, nil)
	// none → descarta (signal por defecto)
	if e.Emit(ev("s1", CatSignal, true)) {
		t.Fatal("signal:none debía descartar")
	}
	// urgent → solo urgentes (clients por defecto)
	if e.Emit(ev("c1", CatClients, false)) {
		t.Fatal("clients:urgent debía descartar no-urgente")
	}
	if !e.Emit(ev("c2", CatClients, true)) {
		t.Fatal("clients:urgent debía aceptar urgente")
	}
	// all → pasa todo (system por defecto)
	if !e.Emit(ev("y1", CatSystem, false)) {
		t.Fatal("system:all debía aceptar no-urgente")
	}
	if got := len(e.List()); got != 2 {
		t.Fatalf("lista: %d, esperaba 2", got)
	}
	// Cambio de nivel reconfigura el filtrado
	if err := e.SetConfig(map[string]string{"signal": "all"}); err != nil {
		t.Fatal(err)
	}
	if !e.Emit(ev("s2", CatSignal, false)) {
		t.Fatal("signal:all debía aceptar")
	}
}

func TestEmitDefaultsFillTsAndTime(t *testing.T) {
	e := New(nil, nil)
	e.Emit(AlertEvent{ID: "a1", Category: CatSystem, Severity: "ok", Title: "x", RouterID: "r"})
	got := e.List()[0]
	if got.Ts == 0 || got.Time == "" {
		t.Fatalf("Ts/Time no rellenados: %+v", got)
	}
	if got.Read {
		t.Fatal("Read debe nacer false")
	}
}

func TestDedupWindow(t *testing.T) {
	e := New(nil, nil)
	now := time.Unix(1_700_000_000, 0)
	e.SetClock(func() time.Time { return now })
	base := AlertEvent{ID: "d1", Category: CatSystem, Severity: "ok", Title: "mismo", RouterID: "r1"}
	if !e.Emit(base) {
		t.Fatal("primer emit debía pasar")
	}
	// Mismo (category,title,routerId) dentro de 5 min → ignorado (aunque cambie el ID)
	dup := base
	dup.ID = "d2"
	if e.Emit(dup) {
		t.Fatal("dedup 5 min debía ignorar")
	}
	// Distinto routerId → otra clave, pasa
	other := base
	other.ID, other.RouterID = "d3", "r2"
	if !e.Emit(other) {
		t.Fatal("routerId distinto debía pasar")
	}
	// Tras la ventana → vuelve a pasar
	now = now.Add(DedupWindow + time.Second)
	dup.ID = "d4"
	if !e.Emit(dup) {
		t.Fatal("tras 5 min debía pasar de nuevo")
	}
}

func TestCap100(t *testing.T) {
	e := New(nil, nil)
	for i := 0; i < 105; i++ {
		e.Emit(AlertEvent{ID: fmt.Sprintf("e%d", i), Category: CatSystem,
			Severity: "ok", Title: fmt.Sprintf("t%d", i), RouterID: "r"})
	}
	list := e.List()
	if len(list) != MaxEvents {
		t.Fatalf("cap: %d, esperaba %d", len(list), MaxEvents)
	}
	if list[0].ID != "e104" {
		t.Fatalf("más reciente primero: %s", list[0].ID)
	}
}

func TestReadStateAndUnread(t *testing.T) {
	e := New(nil, nil)
	e.Emit(ev("a", CatSystem, false))
	e.Emit(ev("b", CatSystem, false))
	e.Emit(ev("c", CatSystem, false))
	if got := e.UnreadCount(); got != 3 {
		t.Fatalf("unread: %d, esperaba 3", got)
	}
	e.MarkRead("a", "b")
	if got := e.UnreadCount(); got != 1 {
		t.Fatalf("unread tras read: %d, esperaba 1", got)
	}
	for _, x := range e.List() {
		want := x.ID == "a" || x.ID == "b"
		if x.Read != want {
			t.Fatalf("Read de %s: %v, esperaba %v", x.ID, x.Read, want)
		}
	}
	e.MarkAllRead()
	if got := e.UnreadCount(); got != 0 {
		t.Fatalf("unread tras read-all: %d", got)
	}
}

func TestReadSetFIFOCap(t *testing.T) {
	e := New(nil, nil)
	ids := make([]string, 0, MaxReadIDs+10)
	for i := 0; i < MaxReadIDs+10; i++ {
		ids = append(ids, fmt.Sprintf("id%d", i))
	}
	e.MarkRead(ids...)
	if len(e.readOrd) != MaxReadIDs {
		t.Fatalf("readOrd: %d, esperaba %d", len(e.readOrd), MaxReadIDs)
	}
	if e.readSet["id0"] {
		t.Fatal("FIFO debía expulsar el más antiguo")
	}
	if !e.readSet["id209"] {
		t.Fatal("el más reciente debe seguir en el set")
	}
}

func TestPersistenceRoundtrip(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	e := New(d, nil)
	if err := e.SetConfig(map[string]string{"vpn": "all", "signal": "urgent"}); err != nil {
		t.Fatal(err)
	}
	e.Emit(ev("p1", CatSystem, false))
	e.Emit(ev("p2", CatSystem, false))
	e.MarkRead("p1")

	// Motor nuevo sobre la misma DB: config y read-state sobreviven
	e2 := New(d, nil)
	cfg := e2.Config()
	if cfg["vpn"] != "all" || cfg["signal"] != "urgent" || cfg["router"] != "urgent" {
		t.Fatalf("config persistida: %v", cfg)
	}
	e2.Seed(ev("p1", CatSystem, false))
	e2.Seed(ev("p2", CatSystem, false))
	for _, x := range e2.List() {
		if (x.ID == "p1") != x.Read {
			t.Fatalf("read persistido de %s: %v", x.ID, x.Read)
		}
	}
	if got := e2.UnreadCount(); got != 1 {
		t.Fatalf("unread persistido: %d, esperaba 1", got)
	}
}

type spyNotifier struct{ got []AlertEvent }

func (s *spyNotifier) Notify(ev AlertEvent) { s.got = append(s.got, ev) }

func TestNotifierOnlyUrgentThatPass(t *testing.T) {
	spy := &spyNotifier{}
	e := New(nil, spy)
	e.Emit(ev("n1", CatSystem, false)) // pasa, no urgente → sin notify
	e.Emit(ev("n2", CatSystem, true))  // pasa, urgente → notify
	e.Emit(ev("n3", CatSignal, true))  // signal:none → descartado, sin notify
	if len(spy.got) != 1 || spy.got[0].ID != "n2" {
		t.Fatalf("notifier: %+v", spy.got)
	}
}

func TestSeedSkipsConfigButKeepsDedup(t *testing.T) {
	e := New(nil, nil)
	// Seed ignora el filtro (vpn:none) — historia del demo (SPEC §5)
	if !e.Seed(ev("h1", CatVPN, false)) {
		t.Fatal("Seed debía guardar pese a vpn:none")
	}
	if e.Seed(AlertEvent{ID: "h2", Category: CatVPN, Title: "t-h1", RouterID: "r1"}) {
		t.Fatal("Seed debía respetar dedup")
	}
	if got := len(e.List()); got != 1 {
		t.Fatalf("lista: %d", got)
	}
}
