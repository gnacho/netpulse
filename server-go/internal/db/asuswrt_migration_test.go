package db_test

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/rearmer"
)

func TestOpenMigratesAsuswrtRoutersToExternalAndPreservesToken(t *testing.T) {
	dataDir := t.TempDir()
	first, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open first: %v", err)
	}
	if _, err := first.Exec(`INSERT INTO routers (id, name, host, type, created_at) VALUES ('asus1', 'GT-BE98 Pro', '192.168.8.30', 'asuswrt', 1)`); err != nil {
		t.Fatalf("insert asuswrt row: %v", err)
	}
	if _, err := first.Exec(`INSERT INTO kv (key, value) VALUES (?, ?)`, rearmer.TokenPrefix+"asus1", "existing-token-hash"); err != nil {
		t.Fatalf("insert agent token: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open second: %v", err)
	}
	defer second.Close()

	var typ, tokenHash string
	var agentOnly int
	if err := second.QueryRow(`SELECT type, agent_only FROM routers WHERE id = 'asus1'`).Scan(&typ, &agentOnly); err != nil {
		t.Fatalf("read migrated router: %v", err)
	}
	if typ != "external" || agentOnly != 1 {
		t.Fatalf("migrated router = type %q agent_only %d, want external/1", typ, agentOnly)
	}
	if err := second.QueryRow(`SELECT value FROM kv WHERE key = ?`, rearmer.TokenPrefix+"asus1").Scan(&tokenHash); err != nil {
		t.Fatalf("read preserved token: %v", err)
	}
	if tokenHash != "existing-token-hash" {
		t.Fatalf("token hash changed to %q", tokenHash)
	}
}
