package rintegration

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return NewStore(d.DB)
}

func TestStoreCRUD(t *testing.T) {
	st := newTestStore(t)

	r := Row{
		Slug: "mik", Vendor: "routeros", Mode: "local", Host: "10.0.0.1",
		Username: "admin", IntervalS: 30, Enabled: true, State: "pending",
	}
	if err := st.Upsert(r); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, ok := st.Get("mik")
	if !ok {
		t.Fatal("Get returned not found")
	}
	if got.Vendor != "routeros" || got.Mode != "local" || got.Host != "10.0.0.1" {
		t.Fatalf("unexpected row: %+v", got)
	}
	if !got.Enabled || got.State != "pending" || got.IntervalS != 30 {
		t.Fatalf("unexpected flags: %+v", got)
	}

	if err := st.SetState("mik", "running", ""); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	got, _ = st.Get("mik")
	if got.State != "running" {
		t.Fatalf("state = %q", got.State)
	}

	if err := st.SetVersion("mik", "2.0.0", "routeros_2.0.0_linux_amd64.tar.gz", "amd64"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	got, _ = st.Get("mik")
	if got.Version != "2.0.0" || got.Asset != "routeros_2.0.0_linux_amd64.tar.gz" || got.Arch != "amd64" {
		t.Fatalf("version fields: %+v", got)
	}

	if err := st.SetEnabled("mik", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	got, _ = st.Get("mik")
	if got.Enabled {
		t.Fatal("enabled should be false")
	}

	if err := st.SetLastStart("mik", 1234567890); err != nil {
		t.Fatalf("SetLastStart: %v", err)
	}
	got, _ = st.Get("mik")
	if got.LastStartMs != 1234567890 {
		t.Fatalf("last_start_ms = %d", got.LastStartMs)
	}

	list, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Slug != "mik" {
		t.Fatalf("list = %+v", list)
	}

	if err := st.Delete("mik"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := st.Get("mik"); ok {
		t.Fatal("row still exists after delete")
	}
}

func TestStoreUpsertPreservesCreatedAt(t *testing.T) {
	st := newTestStore(t)
	r := Row{Slug: "a", Vendor: "routeros", Mode: "local", Host: "h", Username: "u", IntervalS: 30}
	if err := st.Upsert(r); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	first, _ := st.Get("a")
	if first.CreatedAt == 0 {
		t.Fatal("created_at not set")
	}
	r2 := Row{Slug: "a", Vendor: "routeros", Mode: "local", Host: "h2", Username: "u", IntervalS: 60, Enabled: true}
	if err := st.Upsert(r2); err != nil {
		t.Fatalf("Upsert #2: %v", err)
	}
	second, _ := st.Get("a")
	if second.CreatedAt != first.CreatedAt {
		t.Fatalf("created_at changed: %d -> %d", first.CreatedAt, second.CreatedAt)
	}
	if second.Host != "h2" || !second.Enabled {
		t.Fatalf("upsert did not update fields: %+v", second)
	}
}
