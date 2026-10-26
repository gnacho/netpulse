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
	url      string
	token    string
	hc       *http.Client
	onEvent  func(Event)
	minRetry time.Duration
	maxRetry time.Duration
	logf     func(string, ...any)
}

// New crea un cliente SSE. serverURL es la URL base del servidor
// (p. ej. "http://192.168.1.226:3000"). hc permite compartir el transporte
// (incluido el pinning SPKI del agente); nil → http.Client por defecto.
func New(serverURL, slug, token string, hc *http.Client, onEvent func(Event)) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 0}
	}
	return &Client{
		url:      serverURL + "/api/agents/" + slug + "/stream",
		token:    token,
		hc:       hc,
		onEvent:  onEvent,
		minRetry: 5 * time.Second,
		maxRetry: 5 * time.Minute,
		logf:     func(string, ...any) {},
	}
}

// SetLogger enchufa el log.
func (c *Client) SetLogger(f func(string, ...any)) { c.logf = f }

// Run conecta al SSE y llama a onEvent por cada comando. Bloquea hasta que
// ctx se cancele. Se reconecta automáticamente si la conexión cae. El backoff
// se RESETEA tras una conexión que llegó a establecerse: solo crece mientras
// los intentos fallan (si no, un agente que sobrevive a varios reinicios del
// server acaba reintentando cada 5 min para siempre, #284).
func (c *Client) Run(ctx context.Context) {
	retry := c.minRetry
	for {
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.logf("[netpulse-agent] SSE desconectado (%v), reintentando en %s", err, retry)
		}
		delay, next := c.retryDelay(retry, err == nil)
		retry = next
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// retryDelay decide cuánto esperar antes del siguiente intento y el valor
// acumulado para el siguiente fallo. Una conexión que llegó a establecerse
// reinicia el backoff (#284); solo los fallos consecutivos lo hacen crecer
// hasta maxRetry (#1563: el `retry *= 2` anterior también corría tras una
// conexión limpia, así que el reinicio no servía de nada).
func (c *Client) retryDelay(current time.Duration, established bool) (delay, next time.Duration) {
	if established {
		return c.minRetry, c.minRetry
	}
	next = current * 2
	if next > c.maxRetry {
		next = c.maxRetry
	}
	return current, next
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
