package topooverride_test

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/topooverride"
)

func TestStoreCRUD(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if got := len(topooverride.List(d.DB)); got != 0 {
		t.Fatalf("BD vacía esperaba 0 overrides, hay %d", got)
	}

	o, err := topooverride.Add(d.DB, topooverride.AddInput{
		MAC: " C8:FF:BF:08:6F:BA ", Kind: "hypervisor",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if o.ID != "c8:ff:bf:08:6f:ba" {
		t.Errorf("id debe ser MAC normalizada, got %q", o.ID)
	}
	if !o.Enabled {
		t.Errorf("nuevo override debe estar enabled")
	}

	// Upsert: mismo id, no duplica
	if _, err := topooverride.Add(d.DB, topooverride.AddInput{
		MAC: "c8:ff:bf:08:6f:ba", Kind: "switch",
	}); err != nil {
		t.Fatalf("Add duplicado: %v", err)
	}
	if got := len(topooverride.List(d.DB)); got != 1 {
		t.Fatalf("upsert no debe duplicar: %d", got)
	}
	list := topooverride.List(d.DB)
	if list[0].Kind != "switch" {
		t.Errorf("upsert debe actualizar kind, got %q", list[0].Kind)
	}

	// Update parcial
	enabled := false
	o, ok := topooverride.Update(d.DB, "c8:ff:bf:08:6f:ba", topooverride.UpdateInput{Enabled: &enabled})
	if !ok {
		t.Fatal("Update existente devolvió false")
	}
	if o.Enabled {
		t.Errorf("Update enabled=false no aplicó")
	}

	// Update inexistente
	if _, ok := topooverride.Update(d.DB, "00:00:00:00:00:00", topooverride.UpdateInput{Enabled: &enabled}); ok {
		t.Error("Update inexistente debe devolver false")
	}

	// Remove
	if !topooverride.Remove(d.DB, "c8:ff:bf:08:6f:ba") {
		t.Error("Remove existente devolvió false")
	}
	if topooverride.Remove(d.DB, "c8:ff:bf:08:6f:ba") {
		t.Error("Remove inexistente debe devolver false")
	}
	if got := len(topooverride.List(d.DB)); got != 0 {
		t.Fatalf("tras remove debe quedar vacío, hay %d", got)
	}
}
