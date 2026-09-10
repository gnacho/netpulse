// Package push — cliente HTTPS del agente (SPEC-AGENTE-PILOTO §2): POST del
// payload a /api/ingest/agent con Bearer; si falla, backoff exponencial
// 5 s → 5 min y buffer en RAM acotado (cap 100, drop-oldest + contador en
// log; mismo patrón que el fix del buffer del collector). Stateless: el
// buffer vive SOLO en memoria (nada en flash).
package push

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

const (
	// MinBackoff / MaxBackoff: 5 s → 5 min (SPEC §2).
	MinBackoff = 5 * time.Second
	MaxBackoff = 5 * time.Minute
	// BufferCap: payloads retenidos en RAM durante una caída del servidor.
	BufferCap = 100
	// StaleAfter: un payload buffered con más edad que esto JAMÁS superará la
	// ventana anti-replay del server (maxTsDrift = 5 min): reintentarlo para
	// siempre convierte una caída >5 min en un deadlock (issue #680). Se
	// descarta en el drenado en vez de reintentarlo.
	StaleAfter = 4 * time.Minute
)

// Client empuja payloads con token, backoff y buffer acotado.
type Client struct {
	url   string
	token string
	hc    *http.Client

	minBackoff time.Duration
	maxBackoff time.Duration
	bufCap     int
	logf       func(string, ...any)

	mu      sync.Mutex
	backoff time.Duration
	buf     []*probe.Payload
	dropped uint64

	droppedStale uint64
}

// New crea el cliente contra serverURL (p. ej. "https://192.168.8.1:3000")
// con el token del equipo. hc nil → cliente por defecto con timeout 10 s.
func New(serverURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		url:        serverURL + "/api/ingest/agent",
		token:      token,
		hc:         hc,
		minBackoff: MinBackoff, maxBackoff: MaxBackoff, bufCap: BufferCap,
		logf: func(string, ...any) {},
	}
}

// SetBackoffBounds ajusta los límites (tests; producción usa 5 s→5 min).
func (c *Client) SetBackoffBounds(min, max time.Duration) { c.minBackoff, c.maxBackoff = min, max }

// SetBufferCap ajusta el cap del buffer (tests).
func (c *Client) SetBufferCap(n int) { c.bufCap = n }

// SetLogger enchufa el log (stderr en producción).
func (c *Client) SetLogger(f func(string, ...any)) { c.logf = f }

// Dropped: payloads descartados por buffer lleno (drop-oldest).
func (c *Client) Dropped() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dropped
}

// DroppedStale: payloads buffered descartados por superar StaleAfter (#680).
func (c *Client) DroppedStale() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.droppedStale
}

// Buffered: payloads pendientes de envío.
func (c *Client) Buffered() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.buf)
}

// Delay: cuánto dormir hasta el próximo ciclo (max(interval, backoff)).
func (c *Client) Delay(interval time.Duration) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.backoff > interval {
		return c.backoff
	}
	return interval
}

// Push envía el payload; si hay buffer pendiente lo drena primero (FIFO:
// los payloads llevan su propio ts). Devuelve error si el payload quedó
// buffered sin confirmar (el caller duerme Delay y reintenta en el próximo
// ciclo — el payload NO se pierde mientras quepa en el buffer).
func (c *Client) Push(ctx context.Context, p *probe.Payload) error {
	c.mu.Lock()
	if len(c.buf) > 0 {
		// Hay pendientes: drena primero (FIFO) y luego envía el nuevo; si el
		// drenado falla, el nuevo entra al buffer (drop-oldest si está lleno).
		if err := c.flushLocked(ctx); err != nil {
			c.enqueueLocked(p)
			c.mu.Unlock()
			return err
		}
		if err := c.post(ctx, p); err != nil {
			c.enqueueLocked(p)
			c.growBackoffLocked()
			c.mu.Unlock()
			return err
		}
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	if err := c.post(ctx, p); err != nil {
		c.mu.Lock()
		c.enqueueLocked(p)
		c.growBackoffLocked()
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.backoff = 0
	c.mu.Unlock()
	return nil
}

// enqueueLocked mete en el buffer con drop-oldest + contador en log.
func (c *Client) enqueueLocked(p *probe.Payload) {
	if len(c.buf) >= c.bufCap {
		c.buf = c.buf[1:]
		c.dropped++
		c.logf("[netpulse-agent] buffer lleno (%d): descartado el payload más viejo (total descartados: %d)", c.bufCap, c.dropped)
	}
	c.buf = append(c.buf, p)
}

// flushLocked intenta drenar el buffer en orden; para al primer fallo. Los
// heads más viejos que StaleAfter se descartan sin reintentar (#680): el
// server los rechazaría con 401 stale_payload y bloquearían el drenado para
// siempre.
func (c *Client) flushLocked(ctx context.Context) error {
	posted := false
	for len(c.buf) > 0 {
		if staleSince(c.buf[0], time.Now()) {
			c.logf("[netpulse-agent] descartado payload buffered stale (ts %d, %s de edad, ventana anti-replay agotada; total descartados por stale: %d)",
				c.buf[0].Ts, time.Since(time.Unix(c.buf[0].Ts, 0)).Round(time.Second), c.droppedStale+1)
			c.buf = c.buf[1:]
			c.droppedStale++
			continue
		}
		if err := c.post(ctx, c.buf[0]); err != nil {
			c.growBackoffLocked()
			return err
		}
		posted = true
		c.buf = c.buf[1:]
	}
	// El backoff solo se resetea si hubo ALGÚN post con éxito: un drenado que
	// solo descartó stales no prueba que la conectividad se haya recuperado
	// (el siguiente post fresco decidirá).
	if posted {
		c.backoff = 0
		c.logf("[netpulse-agent] buffer drenado tras reconexión")
	}
	return nil
}

// growBackoffLocked: 5 s → 10 → 20 → … → 5 min (cap).
func (c *Client) growBackoffLocked() {
	if c.backoff == 0 {
		c.backoff = c.minBackoff
		return
	}
	c.backoff *= 2
	if c.backoff > c.maxBackoff {
		c.backoff = c.maxBackoff
	}
}

// staleSince: true si el payload lleva más de StaleAfter generado (un Ts no
// positivo se considera siempre fresco: no hay dato para valorar).
func staleSince(p *probe.Payload, now time.Time) bool {
	if p.Ts <= 0 {
		return false
	}
	return now.Sub(time.Unix(p.Ts, 0)) > StaleAfter
}

// hmacSign devuelve HMAC-SHA256(token, body) en hex.
func hmacSign(token string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// post hace el POST con Bearer + HMAC; error si no hay 2xx.
func (c *Client) post(ctx context.Context, p *probe.Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Agent-Signature", hmacSign(c.token, body))
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("ingest HTTP %d", res.StatusCode)
	}
	return nil
}
