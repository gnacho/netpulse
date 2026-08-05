// Package sseclient — cliente SSE del agente (Fase 7.3): mantiene una
// conexión SSE abierta al servidor para recibir comandos (refresh, etc.).
// Se reconecta automáticamente con backoff.
package sseclient

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"time"
)

// Event representa un comando recibido del servidor vía SSE.
type Event struct {
	Name string // "refresh", "connected", "bye", etc.
	Data string // payload JSON (puede estar vacío)
}

// Client mantiene una conexión SSE al servidor y notifica comandos vía
// callback. Se reconecta automáticamente con backoff (minRetry → maxRetry).
type Client struct {
	url       string
	token     string
	hc        *http.Client
	onEvent   func(Event)
	minRetry  time.Duration
	maxRetry  time.Duration
	logf      func(string, ...any)
}

// New crea un cliente SSE. serverURL es la URL base del servidor
// (p. ej. "http://192.168.1.226:3000").
func New(serverURL, slug, token string, onEvent func(Event)) *Client {
	return &Client{
		url:      serverURL + "/api/agents/" + slug + "/stream",
		token:    token,
		hc:       &http.Client{Timeout: 0}, // sin timeout: conexión persistente
		onEvent:  onEvent,
		minRetry: 5 * time.Second,
		maxRetry: 5 * time.Minute,
		logf:     func(string, ...any) {},
	}
}

// SetLogger enchufa el log.
func (c *Client) SetLogger(f func(string, ...any)) { c.logf = f }

// Run conecta al SSE y llama a onEvent por cada comando. Bloquea hasta que
// ctx se cancele. Se reconecta automáticamente si la conexión cae.
func (c *Client) Run(ctx context.Context) {
	retry := c.minRetry
	for {
		if err := c.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logf("[netpulse-agent] SSE desconectado (%v), reintentando en %s", err, retry)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retry):
		}
		retry *= 2
		if retry > c.maxRetry {
			retry = c.maxRetry
		}
	}
}

func (c *Client) connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return &httpError{res.StatusCode}
	}

	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(nil, 64<<10)

	var ev Event
	for scanner.Scan() {
		line := scanner.Text()

		// Comentario (heartbeat) — ignorar
		if strings.HasPrefix(line, ":") {
			continue
		}
		// Línea vacía = fin de evento
		if line == "" {
			if ev.Name != "" {
				c.onEvent(ev)
				ev = Event{}
			}
			continue
		}
		// Campo event:
		if strings.HasPrefix(line, "event: ") {
			ev.Name = line[7:]
			continue
		}
		// Campo data:
		if strings.HasPrefix(line, "data: ") {
			ev.Data = line[6:]
			continue
		}
	}
	return scanner.Err()
}

type httpError struct{ code int }

func (e *httpError) Error() string { return http.StatusText(e.code) }