// push_test.go — cliente push contra httptest (SPEC-AGENTE-PILOTO §2):
// Bearer en cada POST, backoff 5s→5m con reset al éxito, buffer RAM cap con
// drop-oldest + contador, y drenado FIFO al reconectar.
package push

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

type sink struct {
	mu       sync.Mutex
	tokens   []string
	payloads []probe.Payload
	fail     bool
}

func (s *sink) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = append(s.tokens, r.Header.Get("Authorization"))
	if s.fail {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	var p probe.Payload
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.payloads = append(s.payloads, p)
	w.WriteHeader(http.StatusAccepted)
}

func mkPayload(ts int64) *probe.Payload {
	return &probe.Payload{Router: "patio", Ts: ts, Version: "0.1.0"}
}

func TestPushEnviaBearer(t *testing.T) {
	s := &sink{}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()
	c := New(srv.URL, "tok123", srv.Client())
	if err := c.Push(context.Background(), mkPayload(1)); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(s.tokens) != 1 || s.tokens[0] != "Bearer tok123" {
		t.Fatalf("Authorization: %v", s.tokens)
	}
	if s.payloads[0].Router != "patio" || s.payloads[0].Version != "0.1.0" {
		t.Fatalf("payload: %+v", s.payloads[0])
	}
}

func TestPushBackoffCreceYSeResetea(t *testing.T) {
	s := &sink{fail: true}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()
	c := New(srv.URL, "tok", srv.Client())
	c.SetBackoffBounds(5*time.Second, 5*time.Minute)
	interval := 15 * time.Second

	// 1er fallo → backoff 5s (Delay sigue siendo el intervalo, 15s > 5s)
	_ = c.Push(context.Background(), mkPayload(1))
	if d := c.Delay(interval); d != interval {
		t.Fatalf("delay 1er fallo: %s", d)
	}
	// 2º fallo → 10 s; 3º → 20 s (> intervalo, Delay manda el backoff)
	_ = c.Push(context.Background(), mkPayload(2))
	_ = c.Push(context.Background(), mkPayload(3))
	if d := c.Delay(interval); d != 20*time.Second {
		t.Fatalf("backoff tras 3 fallos: %s", d)
	}
	// Cap 5 min: muchos fallos seguidos no lo superan
	for i := 0; i < 20; i++ {
		_ = c.Push(context.Background(), mkPayload(int64(i)))
	}
	if d := c.Delay(interval); d != 5*time.Minute {
		t.Fatalf("cap backoff: %s", d)
	}
	// Éxito → reset: el buffer se drena y Delay vuelve al intervalo
	s.mu.Lock()
	s.fail = false
	s.mu.Unlock()
	if err := c.Push(context.Background(), mkPayload(999)); err != nil {
		t.Fatalf("push tras recuperación: %v", err)
	}
	if d := c.Delay(interval); d != interval {
		t.Fatalf("delay tras éxito: %s", d)
	}
	if c.Buffered() != 0 {
		t.Fatalf("buffer debería estar drenado: %d", c.Buffered())
	}
}

func TestPushBufferDropOldestYDrenadoFIFO(t *testing.T) {
	s := &sink{fail: true}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()
	c := New(srv.URL, "tok", srv.Client())
	c.SetBufferCap(100)

	// Servidor caído: 105 pushes → buffer 100, 5 descartados (los más viejos)
	for i := 1; i <= 105; i++ {
		_ = c.Push(context.Background(), mkPayload(int64(i)))
	}
	if c.Buffered() != 100 {
		t.Fatalf("buffer: %d", c.Buffered())
	}
	if c.Dropped() != 5 {
		t.Fatalf("descartados: %d", c.Dropped())
	}

	// Reconexión: drenado FIFO — el primer ts recibido tras la caída es 6
	s.mu.Lock()
	s.fail = false
	s.payloads = nil
	s.mu.Unlock()
	if err := c.Push(context.Background(), mkPayload(106)); err != nil {
		t.Fatalf("drenado: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.payloads) != 101 {
		t.Fatalf("drenados: %d", len(s.payloads))
	}
	if s.payloads[0].Ts != 6 || s.payloads[100].Ts != 106 {
		t.Fatalf("orden FIFO: primero=%d último=%d", s.payloads[0].Ts, s.payloads[100].Ts)
	}
}

func TestPushErrorNo2xx(t *testing.T) {
	s := &sink{fail: true} // 503
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()
	c := New(srv.URL, "tok", srv.Client())
	if err := c.Push(context.Background(), mkPayload(1)); err == nil {
		t.Fatal("503 debería devolver error")
	}
	if c.Buffered() != 1 {
		t.Fatalf("payload debería quedar buffered: %d", c.Buffered())
	}
}
