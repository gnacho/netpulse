package db_test

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestOpenMigratesRouterOSRoutersToExternal(t *testing.T) {
	dataDir := t.TempDir()
	first, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open first: %v", err)
	}
	if _, err := first.Exec(`INSERT INTO routers (id, name, host, type, created_at) VALUES ('mk1', 'MikroTik', '192.168.88.1', 'routeros', 1)`); err != nil {
		t.Fatalf("insert routeros row: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open second: %v", err)
	}
	defer second.Close()

	var typ string
	var agentOnly int
	if err := second.QueryRow(`SELECT type, agent_only FROM routers WHERE id = 'mk1'`).Scan(&typ, &agentOnly); err != nil {
		t.Fatalf("read migrated router: %v", err)
	}
	if typ != "external" || agentOnly != 1 {
		t.Fatalf("migrated router = type %q agent_only %d, want external/1", typ, agentOnly)
	}
}
