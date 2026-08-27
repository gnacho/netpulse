// Package agenthub — hub SSE para agentes (Fase 7.3): el servidor mantiene una
// conexión SSE por agente conectado y puede enviarle comandos (refresh, etc.).
// Auth por token de agente (Bearer), no por sesión.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AgentHub gestiona conexiones SSE por slug de agente.
type AgentHub struct {
	// checkToken(slug, token) → true si el token es válido
	checkToken func(slug, token string) bool

	mu    sync.Mutex
	conns map[string]*agentConn // slug → conexión activa
	// onConnect se llama (en su propia goroutine) cuando un agente conecta
	// su stream; lo usa httpapi para enviar upgrades encolados (#284).
	onConnectMu sync.Mutex
	onConnect   func(slug string)
}

type agentConn struct {
	slug    string
	w       http.ResponseWriter
	flusher http.Flusher
	wmu     sync.Mutex
	done    chan struct{}
	once    sync.Once
}

// NewAgentHub crea el hub de agentes. checkToken valida el Bearer del agente.
func NewAgentHub(checkToken func(string, string) bool) *AgentHub {
	return &AgentHub{checkToken: checkToken, conns: map[string]*agentConn{}}
}

// SetOnConnect registra el callback de conexión de agente (#284).
func (h *AgentHub) SetOnConnect(f func(slug string)) {
	h.onConnectMu.Lock()
	h.onConnect = f
	h.onConnectMu.Unlock()
}

func (h *AgentHub) fireOnConnect(slug string) {
	h.onConnectMu.Lock()
	f := h.onConnect
	h.onConnectMu.Unlock()
	if f != nil {
		go f(slug)
	}
}

// ConnectedSlugs devuelve los slugs con agente conectado vía SSE.
func (h *AgentHub) ConnectedSlugs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.conns))
	for s := range h.conns {
		out = append(out, s)
	}
	return out
}

// Send envía un comando a un agente conectado. Devuelve false si no está
// conectado o falla la escritura (la conexión se cierra internamente).
func (h *AgentHub) Send(slug, event string, data any) bool {
	h.mu.Lock()
	c, ok := h.conns[slug]
	h.mu.Unlock()
	if !ok {
		return false
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return false
	}
	payload := fmt.Sprintf("event: %s\ndata: %s\n\n", event, raw)
	if err := c.write(payload); err != nil {
		h.removeConn(c)
		return false
	}
	return true
}

func (h *AgentHub) removeConn(c *agentConn) {
	h.mu.Lock()
	if h.conns[c.slug] == c {
		delete(h.conns, c.slug)
	}
	h.mu.Unlock()
	c.close()
}

func (h *AgentHub) remove(slug string) {
	h.mu.Lock()
	c, ok := h.conns[slug]
	if ok {
		delete(h.conns, slug)
	}
	h.mu.Unlock()
	if c != nil {
		c.close()
	}
}

func (c *agentConn) write(payload string) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	rc := http.NewResponseController(c.w)
	_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := fmt.Fprint(c.w, payload); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil {
		return err
	}
	return nil
}

func (c *agentConn) close() { c.once.Do(func() { close(c.done) }) }

// HandleStream sirve GET /api/agents/{slug}/stream. Auth por token de agente
// (Bearer, mismo que POST ingest). Un solo stream por slug: si ya hay uno
// conectado, el anterior recibe un evento "bye" y se cierra.
func (h *AgentHub) HandleStream(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}

	// Auth Bearer (mismo token que la ingesta)
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !h.checkToken(slug, token) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}

	// Cerrar conexión anterior del mismo slug. El write va FUERA del mutex:
	// un cliente lento no debe bloquear Send/remove de otros (issue #200).
	h.mu.Lock()
	old, ok := h.conns[slug]
	h.mu.Unlock()
	if ok {
		_ = old.write("event: bye\ndata: {}\n\n")
		time.Sleep(100 * time.Millisecond) // dar tiempo al flush
		h.removeConn(old)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	c := &agentConn{slug: slug, w: w, flusher: flusher, done: make(chan struct{})}
	h.mu.Lock()
	h.conns[slug] = c
	h.mu.Unlock()
	defer h.removeConn(c)

	// Enviar evento "connected" al agente (confirmación)
	_ = c.write("event: connected\ndata: {}\n\n")

	// Upgrade encolado mientras el agente estaba fuera (#284).
	h.fireOnConnect(slug)

	// Heartbeat cada 30s
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-c.done:
			return
		case <-heartbeat.C:
			if err := c.write(":hb\n\n"); err != nil {
				return
			}
		}
	}
}
