// notifier.go — push.Notifier: implementa alerts.Notifier (SPEC-PUSH §1).
// Notify NUNCA bloquea al llamador (el motor de alertas lo invoca desde el
// ciclo de sondeo): encola el evento y un worker único lo drena, enviando a
// todas las suscripciones con timeout de 10 s por envío. 404/410 del push
// service → suscripción muerta → se purga de la DB.
package push

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

const (
	// queueCap acota la cola: ráfagas de urgentes no pueden crecer sin
	// límite; si se llena se descarta (el panel ya muestra la alerta).
	queueCap = 64
	// sendTimeout por envío individual (SPEC-PUSH §1: 10 s).
	sendTimeout = 10 * time.Second
	// ttl de la notificación en el push service (1 h: si el móvil está
	// apagado más tiempo, la alerta ya es historia).
	ttlSeconds = 3600
	// subscriber VAPID (sub del JWT; informativo para el push service).
	// webpush-go antepone "mailto:" si no empieza por "https:".
	subscriber = "netpulse@localhost"
)

// payload es el JSON cifrado que recibe el Service Worker (contrato con
// app/src/sw.ts: {title, body, category, severity, url, tag}).
type payload struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	URL      string `json:"url"`
	Tag      string `json:"tag"` // alertId: dedup nativo del navegador
}

// Notifier implementa alerts.Notifier con entrega asíncrona.
type Notifier struct {
	db     *db.DB
	pub    string
	priv   string
	client *http.Client

	ch   chan alerts.AlertEvent
	done chan struct{} // cerrado en Close: Notify posterior es no-op
	once sync.Once
	wg   sync.WaitGroup
}

// NewNotifier crea el Notifier y arranca su worker. pub/priv son el par
// VAPID de EnsureVAPIDKeys.
func NewNotifier(d *db.DB, pub, priv string) *Notifier {
	n := &Notifier{
		db:     d,
		pub:    pub,
		priv:   priv,
		client: &http.Client{Timeout: sendTimeout},
		ch:     make(chan alerts.AlertEvent, queueCap),
		done:   make(chan struct{}),
	}
	n.wg.Add(1)
	go n.worker()
	return n
}

// Notify encola el evento y vuelve inmediatamente (nunca bloquea el poll).
// Si la cola está llena descarta con aviso: el evento ya está en /api/alerts.
func (n *Notifier) Notify(ev alerts.AlertEvent) {
	select {
	case <-n.done:
		return
	case n.ch <- ev:
	default:
		slog.Warn("push: cola llena, notificación descartada", "alertId", ev.ID, "title", ev.Title)
	}
}

// Close para el worker y espera al envío en curso (acotado por sendTimeout).
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
		n.sendAll(ev)
	}
}

// payloadJSON serializa el cuerpo de la notificación (SPEC-PUSH §1).
func payloadJSON(ev alerts.AlertEvent) ([]byte, error) {
	return json.Marshal(payload{
		Title:    ev.Title,
		Body:     ev.Description,
		Category: ev.Category,
		Severity: ev.Severity,
		URL:      "/alerts",
		Tag:      ev.ID,
	})
}

// sendAll envía el evento a TODAS las suscripciones y purga las muertas.
func (n *Notifier) sendAll(ev alerts.AlertEvent) {
	body, err := payloadJSON(ev)
	if err != nil {
		slog.Error("push: payload no serializable", "alertId", ev.ID, "error", err)
		return
	}
	subs, err := ListSubscriptions(n.db)
	if err != nil {
		slog.Error("push: no se pudieron listar suscripciones", "alertId", ev.ID, "error", err)
		return
	}
	for _, s := range subs {
		n.sendOne(ev, body, s)
	}
}

func (n *Notifier) sendOne(ev alerts.AlertEvent, body []byte, s Subscription) {
	resp, err := webpush.SendNotificationWithContext(context.Background(), body,
		&webpush.Subscription{
			Endpoint: s.Endpoint,
			Keys:     webpush.Keys{Auth: s.Auth, P256dh: s.P256dh},
		},
		&webpush.Options{
			HTTPClient:      n.client,
			Subscriber:      subscriber,
			VAPIDPublicKey:  n.pub,
			VAPIDPrivateKey: n.priv,
			TTL:             ttlSeconds,
		})
	if err != nil {
		slog.Warn("push: envío fallido", "alertId", ev.ID, "endpoint", s.Endpoint, "error", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		// Suscripción muerta (expirada o dada de baja): purgar (SPEC-PUSH §1).
		if err := DeleteSubscription(n.db, s.Endpoint); err != nil {
			slog.Error("push: no se pudo purgar suscripción muerta", "endpoint", s.Endpoint, "error", err)
		}
		slog.Info("push: suscripción purgada", "endpoint", s.Endpoint, "status", resp.StatusCode)
		return
	}
	if resp.StatusCode >= 400 {
		slog.Warn("push: push service respondió error", "alertId", ev.ID, "endpoint", s.Endpoint, "status", resp.StatusCode)
	}
}
