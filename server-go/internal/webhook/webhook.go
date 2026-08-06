// Package webhook — notificador SALIENTE de alertas (Fase 8.7b).
//
// Implementa alerts.Notifier con entrega a una URL externa: firma
// HMAC-SHA256 del body, reintentos con backoff exponencial + jitter
// (4xx no se reintenta salvo 429; 5xx sí), DLQ en tabla webhook_events si
// se agotan los reintentos. Patrón del skill email-webhook-notifications
// (references/webhook-patterns.md) aplicado a NetPulse.
//
// Notify NUNCA bloquea al llamador: encola el evento y un worker único lo
// drena. Si la cola está llena se descarta (la alerta ya está en /api/alerts).
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

const (
	queueCap   = 64
	maxBodyCap = 64 << 10 // 64 KB (recomendación de seguridad del skill)
)

// payload es el contrato del webhook (firmado): event_id para idempotencia
// del receptor y datos de la alerta.
type payload struct {
	EventID     string `json:"event_id"`
	Timestamp   string `json:"timestamp"` // RFC3339
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	RouterID    string `json:"routerId,omitempty"`
	App         string `json:"app"`
}

// Notifier implementa alerts.Notifier con entrega asíncrona y DLQ.
type Notifier struct {
	cfg    config.Webhook
	db     *db.DB
	client *http.Client

	ch   chan alerts.AlertEvent
	done chan struct{}
	once sync.Once
	wg   sync.WaitGroup
}

// NewNotifier crea el Notifier y arranca su worker.
func NewNotifier(cfg config.Webhook, d *db.DB) *Notifier {
	n := &Notifier{
		cfg:    cfg,
		db:     d,
		client: &http.Client{Timeout: cfg.Timeout},
		ch:     make(chan alerts.AlertEvent, queueCap),
		done:   make(chan struct{}),
	}
	n.wg.Add(1)
	go n.worker()
	return n
}

// Notify encola el evento y vuelve inmediatamente.
func (n *Notifier) Notify(ev alerts.AlertEvent) {
	select {
	case <-n.done:
		return
	case n.ch <- ev:
	default:
		slog.Warn("webhook: cola llena, evento descartado", "alertId", ev.ID, "title", ev.Title)
	}
}

// Close para el worker y espera al envío en curso.
func (n *Notifier) Close() {
	n.once.Do(func() {
		close(n.done)
		close(n.ch)
		n.wg.Wait()
	})
}

func (n *Notifier) worker() {
	defer n.wg.Done()
	for ev := range n.ch {
		n.sendWithRetry(ev)
	}
}

// payloadJSON construye el body firmable de un evento.
func payloadJSON(ev alerts.AlertEvent) ([]byte, error) {
	return json.Marshal(payload{
		EventID:     ev.ID,
		Timestamp:   time.Unix(ev.Ts, 0).UTC().Format(time.RFC3339),
		Type:        "alert." + ev.Category,
		Severity:    ev.Severity,
		Title:       ev.Title,
		Description: ev.Description,
		Category:    ev.Category,
		RouterID:    ev.RouterID,
		App:         "netpulse",
	})
}

// sign calcula la firma HMAC-SHA256 del body crudo.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// sendWithRetry envía con backoff exponencial + jitter. 4xx (salvo 429) no se
// reintenta; 429/5xx sí. Al agotar reintentos guarda en DLQ.
func (n *Notifier) sendWithRetry(ev alerts.AlertEvent) {
	body, err := payloadJSON(ev)
	if err != nil {
		slog.Error("webhook: payload no serializable", "alertId", ev.ID, "error", err)
		return
	}
	var lastErr error
	for attempt := 0; attempt <= n.cfg.Retries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), n.cfg.Timeout)
		err = n.sendOnce(ctx, ev.ID, body)
		cancel()
		if err == nil {
			return
		}
		lastErr = err
		var retryable bool
		if httpErr, ok := err.(*httpStatusError); ok {
			// 4xx (salvo 429): error del emisor, no reintentar jamás.
			if httpErr.status >= 400 && httpErr.status < 500 && httpErr.status != http.StatusTooManyRequests {
				slog.Warn("webhook: 4xx permanente, no se reintenta", "alertId", ev.ID, "status", httpErr.status)
				n.saveDLQ(ev, body, lastErr.Error())
				return
			}
			retryable = true
		}
		if attempt < n.cfg.Retries && retryable {
			jitter := time.Duration(rand.Int63n(int64(n.cfg.RetryDelay)))
			backoff := n.cfg.RetryDelay * time.Duration(1<<attempt)
			time.Sleep(backoff + jitter)
		}
	}
	n.saveDLQ(ev, body, lastErr.Error())
}

// httpStatusError marca la respuesta HTTP para la decisión de reintento.
type httpStatusError struct {
	status int
	body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("webhook status %d: %s", e.status, e.body)
}

func (n *Notifier) sendOnce(ctx context.Context, eventID string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NetPulse-Webhook/2.0")
	req.Header.Set("X-Webhook-Signature", sign(body, n.cfg.Secret))
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyCap))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return &httpStatusError{status: resp.StatusCode, body: string(respBody)}
}

// saveDLQ persiste el evento no entregado para diagnóstico posterior.
func (n *Notifier) saveDLQ(ev alerts.AlertEvent, body []byte, reason string) {
	if n.db == nil {
		return
	}
	if _, err := n.db.Exec(
		"INSERT OR REPLACE INTO webhook_events (event_id, payload, sent_at, error) VALUES (?, ?, ?, ?)",
		ev.ID, string(body), time.Now().Unix(), reason,
	); err != nil {
		slog.Error("webhook: no se pudo guardar en DLQ", "alertId", ev.ID, "error", err)
		return
	}
	slog.Warn("webhook: evento a DLQ", "alertId", ev.ID, "error", reason)
}
