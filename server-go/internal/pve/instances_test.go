// instances_test.go - tests de la config multi-instancia (#764): migración
// legacy, upsert parcial (secret conservado) y delete.
package pve

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func openInstDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func kvPut(t *testing.T, d *db.DB, k, v string) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
		t.Fatal(err)
	}
}

func TestInstancesMigrationLegacy(t *testing.T) {
	d := openInstDB(t)
	kvPut(t, d, "proxmox_url", "https://192.168.1.100:8006")
	kvPut(t, d, "proxmox_token_id", "root@pam!netpulse")
	kvPut(t, d, "proxmox_token_secret", "uuid-1")

	list := LoadInstances(d.DB)
	if len(list) != 1 || list[0].ID != "default" || list[0].URL != "https://192.168.1.100:8006" || list[0].Secret != "uuid-1" {
		t.Fatalf("migración: %+v", list)
	}
	// Las claves legacy se limpian tras migrar.
	var n int
	_ = d.DB.QueryRow("SELECT count(*) FROM kv WHERE key LIKE 'proxmox_%' AND key != 'proxmox_instances'").Scan(&n)
	if n != 0 {
		t.Fatalf("claves legacy sin limpiar: %d", n)
	}
	// Segunda carga: ya viene de la lista.
	if list2 := LoadInstances(d.DB); len(list2) != 1 || list2[0].ID != "default" {
		t.Fatalf("segunda carga: %+v", list2)
	}
}

func TestInstancesUpsertSecretConservado(t *testing.T) {
	d := openInstDB(t)
	in := Instance{ID: "ofi", Name: "Oficina", Config: Config{URL: "https://10.0.0.200:8006", TokenID: "root@pam!np", Secret: "s1"}}
	if err := UpsertInstance(d.DB, in); err != nil {
		t.Fatalf("upsert nuevo: %v", err)
	}
	// Edición sin secret: conserva s1.
	in.Secret = ""
	in.Name = "Ofi editada"
	if err := UpsertInstance(d.DB, in); err != nil {
		t.Fatalf("upsert edicion: %v", err)
	}
	list := LoadInstances(d.DB)
	if len(list) != 1 || list[0].Secret != "s1" || list[0].Name != "Ofi editada" {
		t.Fatalf("tras edicion: %+v", list)
	}
	// Nueva sin secret: error.
	if err := UpsertInstance(d.DB, Instance{ID: "x", Config: Config{URL: "https://x:8006", TokenID: "a!b"}}); err == nil {
		t.Fatalf("nueva sin secret debe fallar")
	}
	// Delete.
	if err := DeleteInstance(d.DB, "ofi"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if list := LoadInstances(d.DB); len(list) != 0 {
		t.Fatalf("tras delete: %+v", list)
	}
}
