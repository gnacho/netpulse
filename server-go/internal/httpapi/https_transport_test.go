package httpapi

// FORK: tests for the record of how each agent reports (https_settings.go).

import (
	"testing"
	"time"
)

func TestAgentsOnHTTPSurviveARestartAndForgetTheDeleted(t *testing.T) {
	s := panelTestServer(t)
	for _, slug := range []string{"router-a", "router-b", "router-c"} {
		if _, err := s.db.Exec("INSERT INTO kv (key, value) VALUES (?, 'h')", agentTokenKey(slug)); err != nil {
			t.Fatal(err)
		}
	}
	s.noteAgentTransport("router-a", false)
	s.noteAgentTransport("router-b", true)
	s.noteAgentTransport("router-c", false)
	// A later push over the other transport is what counts.
	s.noteAgentTransport("router-c", true)

	// A restarted server - same database, empty memory - still knows.
	restarted := &server{db: s.db}
	got := restarted.agentsOnHTTP()
	if len(got) != 1 || got[0].Slug != "router-a" {
		t.Fatalf("after a restart, on HTTP: %+v", got)
	}
	if tls := restarted.agentsByTransport(true); len(tls) != 2 {
		t.Fatalf("after a restart, on HTTPS: %+v", tls)
	}

	// A deleted agent drops out.
	if _, err := s.db.Exec("DELETE FROM kv WHERE key = ?", agentTokenKey("router-a")); err != nil {
		t.Fatal(err)
	}
	if got := restarted.agentsOnHTTP(); len(got) != 0 {
		t.Fatalf("a deleted agent is still listed: %+v", got)
	}
}

func TestASilentAgentDropsOut(t *testing.T) {
	s := panelTestServer(t)
	if _, err := s.db.Exec("INSERT INTO kv (key, value) VALUES (?, 'h')", agentTokenKey("router-a")); err != nil {
		t.Fatal(err)
	}
	s.agentTransport.Store("router-a", agentTransport{TLS: false, At: time.Now().Add(-agentTransportWindow - time.Minute)})
	if got := s.agentsOnHTTP(); len(got) != 0 {
		t.Fatalf("an agent silent for over a day is listed: %+v", got)
	}
}
