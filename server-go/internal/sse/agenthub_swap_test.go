package sse

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSwapConnIsAtomic(t *testing.T) {
	h := NewAgentHub(func(string, string) bool { return true })
	const n = 32

	conns := make([]*agentConn, n)
	olds := make([]*agentConn, n)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		conns[i] = &agentConn{slug: "rt1", done: make(chan struct{})}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			olds[i] = h.swapConn(conns[i])
		}(i)
	}
	close(start)
	wg.Wait()

	seen := make(map[*agentConn]bool, n-1)
	for _, old := range olds {
		if old == nil {
			continue
		}
		if seen[old] {
			t.Fatalf("duplicate previous connection: %p", old)
		}
		seen[old] = true
	}
	if len(seen) != n-1 {
		t.Fatalf("previous connections: got %d want %d", len(seen), n-1)
	}

	h.mu.Lock()
	got := h.conns["rt1"]
	h.mu.Unlock()
	if got == nil {
		t.Fatal("no active connection")
	}
	found := false
	for _, c := range conns {
		if c == got {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unexpected active connection: %p", got)
	}
}

func TestConcurrentAgentStreamsKeepOne(t *testing.T) {
	h := NewAgentHub(func(string, string) bool { return true })

	type stream struct {
		rec    *httptest.ResponseRecorder
		cancel context.CancelFunc
		done   chan struct{}
	}
	streams := make([]*stream, 2)
	start := make(chan struct{})

	for i := range streams {
		req := httptest.NewRequest("GET", "/api/agents/rt1/stream", nil)
		req.SetPathValue("slug", "rt1")
		ctx, cancel := context.WithCancel(req.Context())
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		s := &stream{rec: rec, cancel: cancel, done: make(chan struct{})}
		streams[i] = s

		go func() {
			defer close(s.done)
			<-start
			h.HandleStream(rec, req)
		}()
	}
	close(start)

	var loser int
	select {
	case <-streams[0].done:
		loser = 0
	case <-streams[1].done:
		loser = 1
	case <-time.After(3 * time.Second):
		t.Fatal("no stream was replaced")
	}
	for _, s := range streams {
		s.cancel()
	}
	<-streams[1-loser].done

	if !strings.Contains(streams[loser].rec.Body.String(), "event: bye") {
		t.Fatal("replaced stream did not receive bye")
	}
	if strings.Contains(streams[1-loser].rec.Body.String(), "event: bye") {
		t.Fatal("active stream received bye")
	}

	h.mu.Lock()
	left := len(h.conns)
	h.mu.Unlock()
	if left != 0 {
		t.Fatalf("connections left: %d", left)
	}
}
