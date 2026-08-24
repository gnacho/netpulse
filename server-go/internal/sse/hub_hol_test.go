package sse

import (
	"net/http"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// slowWriter es un ResponseWriter que bloquea la escritura (ventana TCP
// llena): nunca completa Fprint, simula un cliente que no drena el socket.
type slowWriter struct{}

func (slowWriter) Header() http.Header { return http.Header{} }
func (slowWriter) Write(p []byte) (int, error) {
	time.Sleep(2500 * time.Millisecond) // bloquea el write como un socket atascado
	return len(p), nil
}
func (slowWriter) WriteHeader(int) {}
func (slowWriter) Flush()          {}

// chanWriter notifica por canal cuando recibe un write (sin compartir buffer
// entre la goroutine de Broadcast y el test: evita la data race del recorder).
type chanWriter struct {
	ch chan string
}

func (chanWriter) Header() http.Header { return http.Header{} }
func (w chanWriter) Write(p []byte) (int, error) {
	w.ch <- string(p)
	return len(p), nil
}
func (chanWriter) WriteHeader(int) {}
func (chanWriter) Flush()          {}

// TestBroadcastNoBloqueadoPorClienteLento reproduce el bug #200: un cliente
// lento no debe bloquear la entrega del broadcast (el poller lanza el
// broadcast desde su tick). Con el fix, Broadcast con un cliente lento y uno
// rápido debe devolver en <2s y el rápido recibir el payload.
func TestBroadcastNoBloqueadoPorClienteLento(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	cfg := &config.Config{AuthUser: "admin", AuthPass: "test1234"}
	_, err = auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Sesiones válidas para que Broadcast no descarte por revocada.
	fastSess := auth.CreateSession(d, "test", 1)
	slowSess := auth.CreateSession(d, "test", 1)

	// Cliente rápido (notifica por canal al recibir).
	fastRecv := make(chan string, 4)
	fast := &client{id: 1, sessionID: fastSess, w: chanWriter{ch: fastRecv}, flusher: chanWriter{ch: fastRecv}, done: make(chan struct{})}

	// Cliente lento (write bloqueante).
	slow := &client{id: 2, sessionID: slowSess, w: slowWriter{}, flusher: slowWriter{}, done: make(chan struct{})}

	h := NewHub(d, 10, nil)
	h.mu.Lock()
	h.clients[fast.id] = fast
	h.clients[slow.id] = slow
	h.mu.Unlock()

	start := time.Now()
	h.Broadcast("snapshot", map[string]any{"x": 1})
	elapsed := time.Since(start)

	// Broadcast debe volver pronto (no esperar al cliente lento).
	if elapsed > 2*time.Second {
		t.Fatalf("Broadcast bloqueado por el cliente lento: tardó %v", elapsed)
	}

	// El cliente rápido debe recibir el payload.
	select {
	case msg := <-fastRecv:
		if msg != "event: snapshot\ndata: {\"x\":1}\n\n" {
			t.Fatalf("payload inesperado: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("el cliente rápido no recibió el snapshot")
	}
}
