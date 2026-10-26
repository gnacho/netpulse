// hide_recovery_test.go - ajuste de ocultar eventos de recuperación (#1518).
// Con el ajuste activo, los cierres informativos (severity ok) no se sirven
// en List() ni cuentan en UnreadCount(); los avisos resueltos (severity
// warn/critical con resolvedTs) siguen visibles. El ajuste vive en kv: el
// efecto es inmediato y sobrevive a reinicios sin migración.
package alerts

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func setHideRecovery(t *testing.T, d *db.DB, on bool) {
	t.Helper()
	val := "0"
	if on {
		val = "1"
	}
	_, err := d.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		KeyHideRecovery, val)
	if err != nil {
		t.Fatalf("kv set: %v", err)
	}
}

func TestHideRecoveryFiltersFeedAndUnread(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	e := New(d, nil)

	now := time.Now().Unix()
	e.Emit(AlertEvent{ID: "w1", Category: CatSignal, Severity: "warn", Title: "Weak signal on dev-a", Ts: now})
	e.Emit(AlertEvent{ID: "ok1", Category: CatSignal, Severity: "ok", Title: "Weak signal resolved on dev-a", Type: TypeWifiWeakRecovered, Ts: now})
	// Aviso original ya resuelto (#1349): no es un cierre ok, se queda.
	e.Emit(AlertEvent{ID: "rw1", Category: CatSignal, Severity: "warn", Title: "Weak signal on dev-b", ResolvedTs: now, Ts: now})

	if got := len(e.List()); got != 3 {
		t.Fatalf("sin ajuste: esperaba 3 eventos visibles, hay %d", got)
	}
	if got := e.UnreadCount(); got != 3 {
		t.Fatalf("sin ajuste: esperaba 3 no leídas, hay %d", got)
	}

	setHideRecovery(t, d, true)
	list := e.List()
	if got := len(list); got != 2 {
		t.Fatalf("con ajuste: esperaba 2 eventos visibles (warn activo + warn resuelto), hay %d: %+v", got, list)
	}
	for _, ev := range list {
		if ev.Severity == "ok" {
			t.Errorf("un evento severity ok sigue visible con el ajuste activo: %+v", ev)
		}
	}
	if got := e.UnreadCount(); got != 2 {
		t.Fatalf("con ajuste: esperaba 2 no leídas, hay %d", got)
	}

	// Desactivar restaura el feed completo sin reinicio.
	setHideRecovery(t, d, false)
	if got := len(e.List()); got != 3 {
		t.Fatalf("ajuste desactivado: esperaba 3 eventos visibles, hay %d", got)
	}
}

// El ajuste vive en kv: un motor nuevo sobre la misma BD lo respeta (tras el
// reinicio del self-update el feed sigue limpio).
func TestHideRecoverySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	e1 := New(d, nil)
	e1.Emit(AlertEvent{ID: "ok1", Category: CatSystem, Severity: "ok", Title: "Agent recovered", Ts: time.Now().Unix()})
	setHideRecovery(t, d, true)

	e2 := New(d, nil)
	if got := len(e2.List()); got != 0 {
		t.Fatalf("tras reinicio con ajuste activo: esperaba 0 eventos visibles, hay %d", got)
	}
	if got := e2.UnreadCount(); got != 0 {
		t.Fatalf("tras reinicio con ajuste activo: esperaba 0 no leídas, hay %d", got)
	}
}

// NotifyChange dispara el callback de mutación sin tocar el estado (lo usa el
// PUT de umbrales para re-emitir el feed por SSE al activar el ajuste).
func TestNotifyChangeFires(t *testing.T) {
	e := New(nil, nil)
	fired := 0
	e.SetOnChange(func() { fired++ })
	e.NotifyChange()
	e.NotifyChange()
	if fired != 2 {
		t.Fatalf("NotifyChange: esperaba 2 disparos, hubo %d", fired)
	}
}

// Purga one-shot (#1518): los cierres de señal débil del formato ANTIGUO
// (alert-weak-<mac>-ok-<ts>) se borran de alert_log al arrancar; el ID
// estable nuevo (alert-weak-<mac>-ok) y los avisos originales (con o sin
// resolved_ts) se conservan.
func TestPurgeSupersededWeakClosures(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	now := time.Now().Unix()
	seed := []AlertEvent{
		{ID: "alert-weak-aa:bb-ok-1790000000", Category: CatSignal, Severity: "ok", Title: "viejo 1", Ts: now - 60},
		{ID: "alert-weak-aa:bb-ok-1790000300", Category: CatSignal, Severity: "ok", Title: "viejo 2", Ts: now - 30},
		{ID: "alert-weak-aa:bb-ok", Category: CatSignal, Severity: "ok", Title: "ID estable", Ts: now},
		{ID: "alert-weak-aa:bb-1790000100", Category: CatSignal, Severity: "warn", Title: "aviso original", Ts: now - 20, ResolvedTs: now - 10},
	}
	for _, ev := range seed {
		if !e1emit(t, d, ev) {
			t.Fatalf("no se insertó %s", ev.ID)
		}
	}

	e2 := New(d, nil)
	got := map[string]bool{}
	for _, ev := range e2.List() {
		got[ev.ID] = true
	}
	if got["alert-weak-aa:bb-ok-1790000000"] || got["alert-weak-aa:bb-ok-1790000300"] {
		t.Error("los cierres del formato antiguo siguen en el log tras la purga")
	}
	if !got["alert-weak-aa:bb-ok"] || !got["alert-weak-aa:bb-1790000100"] {
		t.Errorf("la purga borró entradas válidas: %v", got)
	}
}

// e1emit inserta un evento directo en alert_log (formato de fila real, sin
// pasar por el engine que ya purga).
func e1emit(t *testing.T, d *db.DB, ev AlertEvent) bool {
	t.Helper()
	_, err := d.Exec(
		`INSERT INTO alert_log (id, ts, category, type, severity, urgent, title, description, router_id, resolved_ts)
		 VALUES (?, ?, ?, ?, ?, 0, ?, '', '', ?)`,
		ev.ID, ev.Ts, ev.Category, ev.Type, ev.Severity, ev.Title, ev.ResolvedTs)
	return err == nil
}
