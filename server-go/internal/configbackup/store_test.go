package configbackup

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	st, err := NewStore(d)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return st
}

func countSnapshots(t *testing.T, st *Store, routerID string) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM config_snapshots WHERE router_id = ?", routerID).Scan(&n); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	return n
}

func TestSaveKeepsNewestSnapshots(t *testing.T) {
	st := newTestStore(t)
	for i := 0; i < maxPerRouter+5; i++ {
		if err := st.Save("rt1", fmt.Sprintf("snap-%02d", i), "network", []byte("x")); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	if got := countSnapshots(t, st, "rt1"); got != maxPerRouter {
		t.Fatalf("snapshots=%d want %d", got, maxPerRouter)
	}
	list, err := st.List("rt1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != maxPerRouter {
		t.Fatalf("listed=%d want %d", len(list), maxPerRouter)
	}
	if list[0].SnapshotID != "snap-34" {
		t.Fatalf("newest=%q want snap-34", list[0].SnapshotID)
	}
	if list[len(list)-1].SnapshotID != "snap-05" {
		t.Fatalf("oldest retained=%q want snap-05", list[len(list)-1].SnapshotID)
	}
}

func TestSaveRollsBackWhenRetentionDeleteFails(t *testing.T) {
	st := newTestStore(t)
	for i := 0; i < maxPerRouter; i++ {
		if err := st.Save("rt1", fmt.Sprintf("snap-%02d", i), "", []byte("x")); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	if _, err := st.db.Exec(`CREATE TRIGGER block_snapshot_delete BEFORE DELETE ON config_snapshots
		BEGIN SELECT RAISE(ABORT, 'delete blocked'); END;`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	err := st.Save("rt1", "snap-30", "", []byte("x"))
	if err == nil {
		t.Fatal("save with blocked retention delete succeeded")
	}
	if !strings.Contains(err.Error(), "prune snapshots") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := countSnapshots(t, st, "rt1"); got != maxPerRouter {
		t.Fatalf("snapshots after rollback=%d want %d", got, maxPerRouter)
	}
	var n int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM config_snapshots WHERE snapshot_id = ?", "snap-30").Scan(&n); err != nil {
		t.Fatalf("count rolled back snapshot: %v", err)
	}
	if n != 0 {
		t.Fatalf("rolled back snapshot persisted")
	}

	if _, err := st.db.Exec("DROP TRIGGER block_snapshot_delete"); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if err := st.Save("rt1", "snap-31", "", []byte("x")); err != nil {
		t.Fatalf("save after trigger drop: %v", err)
	}
	if got := countSnapshots(t, st, "rt1"); got != maxPerRouter {
		t.Fatalf("snapshots after recovery=%d want %d", got, maxPerRouter)
	}
	list, err := st.List("rt1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list[0].SnapshotID != "snap-31" {
		t.Fatalf("newest=%q want snap-31", list[0].SnapshotID)
	}
	if list[len(list)-1].SnapshotID != "snap-01" {
		t.Fatalf("oldest retained=%q want snap-01", list[len(list)-1].SnapshotID)
	}
}
