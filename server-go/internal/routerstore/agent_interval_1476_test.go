package routerstore

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// #1476: el intervalo del agente es un ajuste por router. Se guarda tal cual
// al crear, sobrevive a la lectura y se puede cambiar después.
func TestAgentIntervalRoundTrip(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	cfg, err := AddRouter(d.DB, AddInput{Name: "rt1", Host: "192.168.1.10", Type: "openwrt", AgentInterval: 90})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if cfg.AgentInterval != 90 {
		t.Fatalf("AgentInterval al crear = %d, esperaba 90", cfg.AgentInterval)
	}

	listed := ListRouters(d.DB)
	if len(listed) != 1 || listed[0].AgentInterval != 90 {
		t.Fatalf("ListRouters devolvió %+v", listed)
	}

	v := 15
	upd, ok := UpdateRouter(d.DB, cfg.ID, UpdateInput{AgentInterval: &v})
	if !ok {
		t.Fatal("UpdateRouter devolvió ok=false")
	}
	if upd.AgentInterval != 15 {
		t.Fatalf("AgentInterval tras update = %d, esperaba 15", upd.AgentInterval)
	}

	// 0 vuelve al default del agente y se guarda tal cual, sin normalizar.
	zero := 0
	upd, _ = UpdateRouter(d.DB, cfg.ID, UpdateInput{AgentInterval: &zero})
	if upd.AgentInterval != 0 {
		t.Fatalf("AgentInterval tras reset = %d, esperaba 0", upd.AgentInterval)
	}
}
