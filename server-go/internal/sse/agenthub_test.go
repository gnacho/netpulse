package sse

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestAgentConnWriteConcurrentNoRace verifica que llamadas concurrentes a
// agentConn.write() (desde Send, heartbeat y el "bye" de reemplazo) no
// intercalan escrituras en el ResponseWriter. El detector -race debe estar
// limpio. Sin el wmu en agentConn, este test detecta la data race.
func TestAgentConnWriteConcurrentNoRace(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &agentConn{
		slug:    "test",
		w:       rec,
		flusher: rec,
		done:    make(chan struct{}),
	}

	const goroutines = 8
	const writesEach = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < writesEach; j++ {
				if err := c.write("event: ping\ndata: {}\n\n"); err != nil {
					t.Errorf("write failed: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	totalWrites := goroutines * writesEach
	got := rec.Body.Len()
	// Cada payload "event: ping\ndata: {}\n\n" = 22 bytes.
	want := totalWrites * len("event: ping\ndata: {}\n\n")
	if got != want {
		t.Fatalf("body length = %d, want %d (interleaved/corrupted writes)", got, want)
	}
}

func TestSendPrivilegedRequiresTLSTransport(t *testing.T) {
	h := NewAgentHub(func(string, string) bool { return true })
	rec := httptest.NewRecorder()
	c := &agentConn{slug: "router", w: rec, flusher: rec, done: make(chan struct{})}
	h.conns["router"] = c

	if err := h.SendPrivileged("router", "upgrade", map[string]int{"n": 1}); !errors.Is(err, ErrInsecureAgentTransport) {
		t.Fatalf("HTTP debía rechazarse, got %v", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("no debía escribir nada, body: %q", rec.Body.String())
	}

	h.SetAllowInsecureCommands(true)
	if err := h.SendPrivileged("router", "upgrade", map[string]int{"n": 1}); err != nil {
		t.Fatalf("con la válvula debía permitirse, got %v", err)
	}
	if !strings.Contains(rec.Body.String(), "event: upgrade") {
		t.Fatalf("no llegó el evento: %q", rec.Body.String())
	}
}

func TestSendPrivilegedAllowsTLSAndRejectsDisconnected(t *testing.T) {
	h := NewAgentHub(func(string, string) bool { return true })
	rec := httptest.NewRecorder()
	c := &agentConn{slug: "router", secure: true, w: rec, flusher: rec, done: make(chan struct{})}
	h.conns["router"] = c

	if err := h.SendPrivileged("router", "apply", map[string]string{"plan": "x"}); err != nil {
		t.Fatalf("TLS debía permitirse, got %v", err)
	}
	if err := h.SendPrivileged("otro", "apply", nil); !errors.Is(err, ErrAgentNotConnected) {
		t.Fatalf("desconectado debía dar ErrAgentNotConnected, got %v", err)
	}
}
