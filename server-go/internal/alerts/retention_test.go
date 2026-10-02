// retention_test.go - issue #1034: mark-all-read NO destructivo, "Vaciar
// registro" destructivo explícito y retención temporal configurable del log.
package alerts

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func countLogRows(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM alert_log").Scan(&n); err != nil {
		t.Fatalf("count alert_log: %v", err)
	}
	return n
}

func setRetentionDays(t *testing.T, d *db.DB, days int) {
	t.Helper()
	if _, err := d.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		KeyRetentionDays, itoa(days)); err != nil {
		t.Fatalf("kv retention: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [12]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// (a) MarkAllRead persiste el leído y CONSERVA las filas de alert_log: el
// feed sigue mostrando las alertas (leídas) tras un reload.
func TestMarkAllReadPersistsAndKeepsLog(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	e := New(d, nil)
	e.Emit(ev("a1", CatSystem, false))
	e.Emit(ev("a2", CatRouter, true))
	e.MarkAllRead()

	if got := e.UnreadCount(); got != 0 {
		t.Fatalf("unread tras MarkAllRead: %d, esperaba 0", got)
	}
	if got := countLogRows(t, d); got != 2 {
		t.Fatalf("filas de alert_log tras MarkAllRead: %d, esperaba 2 (no destructivo)", got)
	}

	// Motor nuevo sobre la misma DB: las alertas vuelven del log y siguen
	// leídas (read_flag espejado + read-set kv).
	e2 := New(d, nil)
	list := e2.List()
	if got := len(list); got != 2 {
		t.Fatalf("lista tras reload: %d, esperaba 2", got)
	}
	for _, x := range list {
		if !x.Read {
			t.Fatalf("alerta %s no leída tras reload", x.ID)
		}
	}
	if got := e2.UnreadCount(); got != 0 {
		t.Fatalf("unread tras reload: %d, esperaba 0", got)
	}
}

// (b) DismissAll ("Vaciar registro") sigue siendo destructivo: borra las
// filas de alert_log, además de vaciar el feed.
func TestDismissAllDeletesLogRows(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	e := New(d, nil)
	e.Emit(ev("a1", CatSystem, false))
	e.Emit(ev("a2", CatRouter, true))
	if got := countLogRows(t, d); got != 2 {
		t.Fatalf("filas antes de DismissAll: %d, esperaba 2", got)
	}
	e.DismissAll()
	if got := countLogRows(t, d); got != 0 {
		t.Fatalf("filas de alert_log tras DismissAll: %d, esperaba 0", got)
	}
}

// (c) La poda por retención borra las filas más viejas que N días y conserva
// las recientes; la volátil vieja NO sale (condición aún viva, #966).
func TestPruneLogByRetention(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	now := time.Now()
	e := New(d, nil)
	e.SetClock(func() time.Time { return now })
	old := ev("old", CatSystem, false)
	old.Ts = now.Add(-40 * 24 * time.Hour).Unix()
	e.Emit(old)
	e.Emit(ev("new", CatSystem, false))
	staleVolatile := AlertEvent{
		ID: "alert-agent-down-r1", Category: CatSystem, Severity: "warn",
		Title: "Agent down", Type: HintAgentDown, RouterID: "r1",
		Ts: now.Add(-40 * 24 * time.Hour).Unix(),
	}
	e.EmitVolatile(staleVolatile)
	if got := countLogRows(t, d); got != 2 {
		t.Fatalf("filas antes de podar: %d, esperaba 2 (la volátil no se persiste)", got)
	}

	setRetentionDays(t, d, 30)
	if n := e.PruneLogByRetention(); n != 1 {
		t.Fatalf("filas podadas: %d, esperaba 1", n)
	}
	if got := countLogRows(t, d); got != 1 {
		t.Fatalf("filas tras podar: %d, esperaba 1", got)
	}
	// En memoria: fuera la persistente vieja, se quedan la reciente y la
	// volátil (su condición sigue viva aunque sea antigua).
	list := e.List()
	if got := len(list); got != 2 {
		t.Fatalf("lista tras podar: %d, esperaba 2", got)
	}
	for _, x := range list {
		if x.ID == "old" {
			t.Fatal("la alerta vieja sigue en la lista tras podar")
		}
	}
	// Un reload tampoco la resucita.
	e2 := New(d, nil)
	for _, x := range e2.List() {
		if x.ID == "old" {
			t.Fatal("la alerta vieja reapareció tras reload")
		}
	}
}

// (d) retentionDays = 0 desactiva la poda temporal (la cota de 500 filas es
// aparte y no se ejerce aquí).
func TestPruneLogByRetentionDisabled(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	now := time.Now()
	setRetentionDays(t, d, 0)
	e := New(d, nil)
	e.SetClock(func() time.Time { return now })
	old := ev("old", CatSystem, false)
	old.Ts = now.Add(-400 * 24 * time.Hour).Unix()
	e.Emit(old)

	if n := e.PruneLogByRetention(); n != 0 {
		t.Fatalf("filas podadas con retention=0: %d, esperaba 0", n)
	}
	if got := countLogRows(t, d); got != 1 {
		t.Fatalf("filas con retention=0: %d, esperaba 1", got)
	}
	if got := len(e.List()); got != 1 {
		t.Fatalf("lista con retention=0: %d, esperaba 1", got)
	}
}
