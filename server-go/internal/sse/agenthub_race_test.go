package sse

import (
	"testing"
	"time"
)

// TestAgentHubRemovePorSlugMataNuevaConexion reproduce el bug #199: el
// reemplazo de un stream (HandleStream escribe "bye" y llama remove(slug))
// y luego el defer h.remove(slug) del handler viejo (desplanificado) borra
// la conexión NUEVA registrada después. La limpieza debe ser por identidad.
func TestAgentHubRemovePorSlugMataNuevaConexion(t *testing.T) {
	h := NewAgentHub(func(slug, token string) bool { return true })

	// Conexión vieja A registrada.
	a := &agentConn{slug: "s1", done: make(chan struct{})}
	h.mu.Lock()
	h.conns["s1"] = a
	h.mu.Unlock()

	// A despierta al cerrarse su done y ejecuta su defer (removeConn(a), por
	// identidad) DESPLANIFICADO 200 ms (simula la goroutine del handler viejo).
	aCleanupDone := make(chan struct{})
	go func() {
		<-a.done
		time.Sleep(200 * time.Millisecond)
		h.removeConn(a)
		close(aCleanupDone)
	}()

	// Reemplazo: HandleStream cierra la conexión anterior (por identidad).
	h.removeConn(a)
	time.Sleep(100 * time.Millisecond)

	// B se registra (nueva conexión del mismo slug).
	b := &agentConn{slug: "s1", done: make(chan struct{})}
	h.mu.Lock()
	h.conns["s1"] = b
	h.mu.Unlock()

	<-aCleanupDone

	h.mu.Lock()
	stillRegistered := h.conns["s1"] == b
	h.mu.Unlock()
	select {
	case <-b.done:
		t.Fatal("BUG #199: la limpieza de A cerró la conexión nueva B")
	default:
	}
	if !stillRegistered {
		t.Fatal("BUG #199: la limpieza de A borró la conexión nueva B del map")
	}
}
