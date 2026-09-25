package sse

// FORK: a write racing a client's departure must not reach its dead
// ResponseWriter - net/http panics there and takes the server down.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// deadAfterWriter panics on use once its request is over, as net/http's own
// ResponseWriter does after the handler has returned.
type deadAfterWriter struct {
	h    http.Header
	dead atomic.Bool
}

func (w *deadAfterWriter) Header() http.Header { return w.h }
func (w *deadAfterWriter) Write(p []byte) (int, error) {
	if w.dead.Load() {
		panic("write after the handler returned")
	}
	return len(p), nil
}
func (w *deadAfterWriter) WriteHeader(int) {}
func (w *deadAfterWriter) Flush() {
	if w.dead.Load() {
		panic("flush after the handler returned")
	}
}

func TestAWriteAfterTheClientLeftIsRefused(t *testing.T) {
	h := NewHub(nil, 10, nil)
	w := &deadAfterWriter{h: http.Header{}}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/stream", nil).WithContext(ctx)
	returned := make(chan struct{})
	go func() { h.HandleStream(w, req); close(returned) }()

	var c *client
	for i := 0; i < 100 && c == nil; i++ {
		h.mu.Lock()
		for _, x := range h.clients {
			c = x
		}
		h.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("the stream never registered its client")
	}
	cancel()
	<-returned
	w.dead.Store(true) // what net/http does once the handler has returned

	// A Broadcast that listed the client before it left gets here.
	if err := c.write("event: x\ndata: {}\n\n"); err != errFinished {
		t.Fatalf("write after the client left: %v, want errFinished", err)
	}
}

func TestAnAgentCommandAfterTheAgentLeftIsRefused(t *testing.T) {
	c := &agentConn{w: &deadAfterWriter{h: http.Header{}}, done: make(chan struct{})}
	c.wmu.Lock()
	c.finished = true
	c.wmu.Unlock()
	c.w.(*deadAfterWriter).dead.Store(true)
	if err := c.write("event: x\ndata: {}\n\n"); err != errFinished {
		t.Fatalf("write after the agent left: %v, want errFinished", err)
	}
}
