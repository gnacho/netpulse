// updater_pending_test.go — confirmación post-update (issue #161): marcador
// from→to persistido antes de aplicar, comparado en el siguiente arranque.
package updater

import (
	"database/sql"
	"encoding/json"
	"testing"
)

// seedPending inserta el marcador pendingApply en kv.
func seedPending(t *testing.T, dbh *sql.DB, from, to string) {
	t.Helper()
	data, _ := json.Marshal(PendingApply{From: from, To: to})
	if _, err := dbh.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		pendingApplyKey, string(data)); err != nil {
		t.Fatal(err)
	}
}

func TestPendingApplyConfirmedOnNewCommit(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeDeployScript(t, root)
	old := writeGitHead(t, root)
	newShort := gitRootCreaSegundoCommit(t, root)
	if newShort == old {
		t.Fatal("los SHAs deberían diferir tras un commit nuevo")
	}
	dbh := openDB(t)
	seedPending(t, dbh, old, newShort)

	u := New(root, "owner/netpulse", "", "2.0.0", dbh) // "arranque"
	st := u.Status()
	if st.PendingApply == nil {
		t.Fatalf("pendingApply debería confirmar el update: %+v", st)
	}
	if st.PendingApply.From != old || st.PendingApply.To != newShort {
		t.Errorf("pendingApply: %+v, want from=%s to=%s", st.PendingApply, old, newShort)
	}
	// Ack → descartada de una sola vez.
	u.AckPending()
	if u.Status().PendingApply != nil {
		t.Fatal("tras ack, pendingApply debería ser nil")
	}
	if _, ok := readPendingApply(dbh); ok {
		t.Fatal("el marcador debería estar borrado tras confirmar")
	}
}

func TestPendingApplyClearedSilentlyWhenUnchanged(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeDeployScript(t, root)
	short := writeGitHead(t, root)
	dbh := openDB(t)
	// Marcador con from == HEAD actual: el update no cambió nada → silencio.
	seedPending(t, dbh, short, "targetsha")

	u := New(root, "owner/netpulse", "", "2.0.0", dbh)
	st := u.Status()
	if st.PendingApply != nil {
		t.Fatalf("sin cambio no debería confirmar: %+v", st.PendingApply)
	}
	if _, ok := readPendingApply(dbh); ok {
		t.Fatal("el marcador debería borrarse en silencio")
	}
}

func TestPendingApplyIgnoredOnStableLayout(t *testing.T) {
	// Layout estable (sin deploy/update.sh): el marcador se limpia sin
	// confirmación (no hay auto-apply en este layout).
	root := t.TempDir()
	dbh := openDB(t)
	seedPending(t, dbh, "oldsha", "newsha")
	u := New(root, "owner/netpulse", "", "2.0.0", dbh)
	if u.Status().PendingApply != nil {
		t.Fatal("en layout estable no debería confirmar")
	}
	if _, ok := readPendingApply(dbh); ok {
		t.Fatal("el marcador debería borrarse")
	}
}
