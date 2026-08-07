// Package sse — hub Server-Sent Events para /api/stream (paridad src/sse.js).
//   - X-Accel-Buffering: no + Cache-Control: no-cache (sobrescribe no-store).
//   - NUNCA Content-Encoding manual.
//   - Primer evento `snapshot` inmediato; heartbeat `:hb` cada 30 s.
//   - MAX_SSE_CLIENTS → 503 {error:'too_many_clients'}.
//   - Sesión revocada (detectado en broadcast) → evento `bye` y cierre.
//   - Formato de evento: `event: <nombre>\ndata: <JSON>\n\n`.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// Heartbeat = 30 s (HEARTBEAT_MS de sse.js:16).
const Heartbeat = 30 * time.Second

type client struct {
	id        int
	sessionID string
	w         http.ResponseWriter
	flusher   http.Flusher
	wmu       sync.Mutex
	done      chan struct{}
	once      sync.Once
}

func (c *client) write(payload string) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	rc := http.NewResponseController(c.w)
	_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := fmt.Fprint(c.w, payload); err != nil {
		return err
	}
	c.flusher.Flush()
	return nil
}

func (c *client) close() { c.once.Do(func() { close(c.done) }) }

// Hub gestiona los clientes SSE conectados.
type Hub struct {
	db          *db.DB
	maxClients  int
	getOverview func() any

	mu      sync.Mutex
	clients map[int]*client
	nextID  int
}

// NewHub crea el hub. getOverview devuelve el último overview del poller
// (o nil si aún no hay) para el snapshot inicial.
func NewHub(d *db.DB, maxClients int, getOverview func() any) *Hub {
	return &Hub{db: d, maxClients: maxClients, getOverview: getOverview, clients: map[int]*client{}, nextID: 1}
}

// Size es el número de clientes conectados.
func (h *Hub) Size() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c.id)
	h.mu.Unlock()
	c.close()
}

// Broadcast envía `event` con `data` (JSON) a todos los clientes. Si la
// sesión de un cliente ya no existe/expiró → evento `bye` y cierre (la
// comprobación solo ocurre durante un broadcast, como en Node).
func (h *Hub) Broadcast(event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	payload := fmt.Sprintf("event: %s\ndata: %s\n\n", event, raw)

	h.mu.Lock()
	list := make([]*client, 0, len(h.clients))
	for _, c := range h.clients {
		list = append(list, c)
	}
	h.mu.Unlock()

	for _, c := range list {
		if auth.GetSession(h.db, c.sessionID) == nil {
			_ = c.write("event: bye\ndata: {}\n\n")
			h.remove(c)
			continue
		}
		if err := c.write(payload); err != nil {
			h.remove(c)
		}
	}
}

// NotifyShutdown envía el evento `shutdown` a todos los clientes.
func (h *Hub) NotifyShutdown() {
	h.mu.Lock()
	list := make([]*client, 0, len(h.clients))
	for _, c := range h.clients {
		list = append(list, c)
	}
	h.mu.Unlock()
	for _, c := range list {
		_ = c.write("event: shutdown\ndata: {}\n\n")
	}
}

// HandleStream sirve GET /api/stream (requiere sesión: RequireAuth antes).
func (h *Hub) HandleStream(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	full := len(h.clients) >= h.maxClients
	h.mu.Unlock()
	if full {
		auth.WriteError(w, http.StatusServiceUnavailable, "too_many_clients")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		auth.WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	sessionID := auth.SessionIDFromContext(r.Context())

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-cache") // sobrescribe el no-store
	// Deliberadamente SIN Content-Encoding (ver sse.js:3-6).
	w.WriteHeader(http.StatusOK)

	h.mu.Lock()
	c := &client{id: h.nextID, sessionID: sessionID, w: w, flusher: flusher, done: make(chan struct{})}
	h.nextID++
	h.clients[c.id] = c
	h.mu.Unlock()
	defer h.remove(c)

	// Primer snapshot inmediato al conectar
	if h.getOverview != nil {
		if ov := h.getOverview(); ov != nil {
			if raw, err := json.Marshal(ov); err == nil {
				if err := c.write(fmt.Sprintf("event: snapshot\ndata: %s\n\n", raw)); err != nil {
					return
				}
			}
		}
	}

	heartbeat := time.NewTicker(Heartbeat)
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
