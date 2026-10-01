// alerts_volatile_test.go - alertas volátiles (#966): la caída de agente no
// se persiste, la purga de arranque limpia el histórico agent-down, y la
// poda del read-set no reactiva alertas visibles.
package alerts

import (
	"fmt"
	"testing"
	"time"
)

// alertLogCount: filas de alert_log que casan con el patrón LIKE dado.
func alertLogCount(t *testing.T, e *Engine, like string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow("SELECT COUNT(*) FROM alert_log WHERE id LIKE ?", like).Scan(&n); err != nil {
		t.Fatalf("count alert_log: %v", err)
	}
	return n
}

// 1. La alerta volátil NO se persiste en alert_log (pero sí está en el feed).
func TestVolatileAlertNotPersisted(t *testing.T) {
	dir := t.TempDir()
	e := newPersistTestEngine(t, dir, false)
	ok := e.EmitVolatile(AlertEvent{
		ID: "alert-agent-down-rt2", Category: CatSystem, Severity: "warn",
		Title: "Agente caído en rt2", Type: HintAgentDownSSH, RouterID: "rt2",
	})
	if !ok {
		t.Fatal("EmitVolatile no guardó el evento")
	}
	if list := e.List(); len(list) != 1 || list[0].ID != "alert-agent-down-rt2" {
		t.Fatalf("feed: %+v", list)
	}
	if n := alertLogCount(t, e, "%"); n != 0 {
		t.Fatalf("alert_log: %d filas, esperado 0 (volátil no persiste)", n)
	}
}

// 2. Purga one-shot al arrancar: el histórico agent-down* acumulado por
// versiones previas desaparece; el resto del log se conserva.
func TestVolatileStartupPurge(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistTestEngine(t, dir, false)
	now := time.Now().Unix()
	// Histórico al estilo viejo (IDs con timestamp) + una alerta normal.
	e1.Emit(AlertEvent{ID: "alert-agent-down-rt2-1717000000000", Category: CatSystem, Severity: "warn", Title: "Agente caído", Type: HintAgentDownSSH, RouterID: "rt2", Ts: now})
	e1.Emit(AlertEvent{ID: "alert-agent-down-rt3-1717000001000", Category: CatRouter, Urgent: true, Severity: "critical", Title: "Agente caído", Type: HintAgentDown, RouterID: "rt3", Ts: now})
	e1.Emit(AlertEvent{ID: "keep-me", Category: CatSystem, Severity: "info", Title: "Otra alerta", Ts: now})
	if n := alertLogCount(t, e1, "alert-agent-down-%"); n != 2 {
		t.Fatalf("siembra: %d filas agent-down, esperado 2", n)
	}

	e2 := newPersistTestEngine(t, dir, false)
	if n := alertLogCount(t, e2, "alert-agent-down-%"); n != 0 {
		t.Fatalf("purga: %d filas agent-down tras arrancar, esperado 0", n)
	}
	list := e2.List()
	if len(list) != 1 || list[0].ID != "keep-me" {
		t.Fatalf("feed tras purga: %+v (solo debía quedar keep-me)", list)
	}
}

// 3. MarkRead no reactiva: la poda FIFO del read-set (cap 200) no puede
// expulsar una alerta visible leída (el badge no vuelve a subir).
func TestMarkReadDoesNotReactivateVisible(t *testing.T) {
	dir := t.TempDir()
	e := newPersistTestEngine(t, dir, false)
	e.Emit(AlertEvent{ID: "live1", Category: CatSystem, Severity: "warn", Title: "visible", Ts: time.Now().Unix()})
	e.MarkRead("live1")
	if e.UnreadCount() != 0 {
		t.Fatal("live1 debía quedar leída")
	}
	// 250 IDs ajenos fuerzan la rotación del read-set más allá del cap.
	junk := make([]string, 0, 250)
	for i := 0; i < 250; i++ {
		junk = append(junk, fmt.Sprintf("junk-%d", i))
	}
	e.MarkRead(junk...)
	if e.UnreadCount() != 0 {
		t.Fatalf("live1 reapareció como no leída tras la poda (unread=%d)", e.UnreadCount())
	}
	list := e.List()
	if len(list) != 1 || !list[0].Read {
		t.Fatalf("feed: %+v (live1 debía seguir leída)", list)
	}
	if !e.readSet["live1"] {
		t.Fatal("live1 expulsada del read-set estando visible")
	}
}

// 4. MarkAllRead sobrevive a un reinicio (read-set persistido en kv).
func TestMarkAllReadSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistTestEngine(t, dir, false)
	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		e1.Emit(AlertEvent{ID: fmt.Sprintf("a%d", i), Category: CatSystem, Severity: "warn", Title: fmt.Sprintf("t%d", i), Ts: now + int64(i)})
	}
	if e1.UnreadCount() != 3 {
		t.Fatalf("siembra: unread=%d, esperado 3", e1.UnreadCount())
	}
	e1.MarkAllRead()
	if e1.UnreadCount() != 0 {
		t.Fatalf("tras MarkAllRead: unread=%d", e1.UnreadCount())
	}

	e2 := newPersistTestEngine(t, dir, false)
	if n := e2.UnreadCount(); n != 0 {
		t.Fatalf("tras reinicio: unread=%d, esperado 0", n)
	}
}

// 5. La alerta live es volátil: un reemit con el mismo ID no duplica, el
// Remove al recuperar la saca del feed y un reinicio no la resucita.
func TestVolatileAlertIsLiveOnly(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistTestEngine(t, dir, false)
	ev := AlertEvent{
		ID: "alert-agent-down-rt2", Category: CatSystem, Severity: "warn",
		Title: "Agente caído en rt2", Type: HintAgentDownSSH, RouterID: "rt2",
	}
	e1.EmitVolatile(ev)
	e1.EmitVolatile(ev) // reemisión con el mismo ID estable
	if list := e1.List(); len(list) != 1 {
		t.Fatalf("reemisión duplicó la alerta: %+v", list)
	}

	// Recuperación: desaparece del feed.
	e1.Remove("alert-agent-down-rt2")
	if list := e1.List(); len(list) != 0 {
		t.Fatalf("tras Remove: %+v (debía quedar vacío)", list)
	}
	if e1.UnreadCount() != 0 {
		t.Fatalf("unread tras Remove: %d", e1.UnreadCount())
	}

	// Reinicio: no resucita (nunca se persistió).
	e2 := newPersistTestEngine(t, dir, false)
	if list := e2.List(); len(list) != 0 {
		t.Fatalf("tras reinicio: %+v (volátil resucitada)", list)
	}
}
