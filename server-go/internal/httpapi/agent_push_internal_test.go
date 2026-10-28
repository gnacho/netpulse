package httpapi

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestAcceptAgentPushPersistsAcrossServers(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	newServer := func() *server {
		return &server{db: d, agents: adapters.NewAgentRegistry(0)}
	}
	s1, s2 := newServer(), newServer()
	now := time.Now().Unix()

	if !s1.acceptAgentPush(&probe.Payload{Router: "rt1", Ts: now}) {
		t.Fatal("first push rejected")
	}
	if s2.acceptAgentPush(&probe.Payload{Router: "rt1", Ts: now}) {
		t.Fatal("equal timestamp accepted")
	}
	if s2.acceptAgentPush(&probe.Payload{Router: "rt1", Ts: now - 1}) {
		t.Fatal("stale timestamp accepted")
	}
	if !s2.acceptAgentPush(&probe.Payload{Router: "rt1", Ts: now + 1}) {
		t.Fatal("newer timestamp rejected")
	}
}
