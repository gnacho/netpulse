package routerstore

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// #1917: el modo de inventario es un ajuste por unidad. "ssh" dice que la
// unidad se inventaria por SSH y que nunca se espera un agente, "agent" es el
// antiguo agent_only, y agent_only se deriva del modo para que el codigo que
// ya lo consultaba siga funcionando.
func TestInventoryModeRoundTrip(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	cfg, err := AddRouter(d.DB, AddInput{Name: "cpe", Host: "192.168.1.20", Type: "openwrt", InventoryMode: "ssh"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if cfg.InventoryMode != "ssh" {
		t.Fatalf("InventoryMode al crear = %q, esperaba ssh", cfg.InventoryMode)
	}
	if cfg.AgentOnly {
		t.Fatal("una unidad en modo ssh no es agent-only: tiene que poder sondearse por SSH")
	}

	listed := ListRouters(d.DB)
	if len(listed) != 1 || listed[0].InventoryMode != "ssh" || listed[0].AgentOnly {
		t.Fatalf("ListRouters devolvio %+v", listed)
	}

	// Cambiar a "agent" deriva agent_only=1, que es lo que leen el poller y la UI.
	agent := "agent"
	upd, ok := UpdateRouter(d.DB, cfg.ID, UpdateInput{InventoryMode: &agent})
	if !ok {
		t.Fatal("UpdateRouter devolvio ok=false")
	}
	if upd.InventoryMode != "agent" || !upd.AgentOnly {
		t.Fatalf("tras update a agent: mode=%q agentOnly=%v", upd.InventoryMode, upd.AgentOnly)
	}

	// Y volver a "auto" lo libera otra vez.
	auto := "auto"
	upd, _ = UpdateRouter(d.DB, cfg.ID, UpdateInput{InventoryMode: &auto})
	if upd.InventoryMode != "auto" || upd.AgentOnly {
		t.Fatalf("tras update a auto: mode=%q agentOnly=%v", upd.InventoryMode, upd.AgentOnly)
	}

	// Nil no toca el modo.
	name := "cpe-renombrado"
	upd, _ = UpdateRouter(d.DB, cfg.ID, UpdateInput{Name: &name})
	if upd.InventoryMode != "auto" {
		t.Fatalf("un update sin modo lo cambio a %q", upd.InventoryMode)
	}
}

// #1917: las filas anteriores a la columna nueva llegan con agent_only y sin
// modo. La lectura lo normaliza para que el frontend no vea un valor vacio.
func TestInventoryModeLegacyRowsNormalise(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	onlyAgent, err := AddRouter(d.DB, AddInput{Name: "nas", Host: "192.168.1.30", Type: "openwrt", AgentOnly: true})
	if err != nil {
		t.Fatalf("add agent-only: %v", err)
	}
	if onlyAgent.InventoryMode != "agent" || !onlyAgent.AgentOnly {
		t.Fatalf("agent_only sin modo: mode=%q agentOnly=%v", onlyAgent.InventoryMode, onlyAgent.AgentOnly)
	}

	plain, err := AddRouter(d.DB, AddInput{Name: "rt2", Host: "192.168.1.31", Type: "openwrt"})
	if err != nil {
		t.Fatalf("add plain: %v", err)
	}
	if plain.InventoryMode != "auto" || plain.AgentOnly {
		t.Fatalf("unidad normal sin modo: mode=%q agentOnly=%v", plain.InventoryMode, plain.AgentOnly)
	}
}
