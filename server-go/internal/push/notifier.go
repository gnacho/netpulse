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
	"net/url"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/gnacho/netpulse/server-go/internal/alertlang"
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
	// fallbackSubject: sub del JWT cuando no hay URL pública https ni subject
	// configurado. Nunca "netpulse@localhost", que Apple rechaza con 403
	// BadJwtToken (#1974). La página del proyecto es un https: valido.
	fallbackSubject = "https://github.com/gnacho/netpulse"
)

// SubjectFor devuelve el sub del JWT VAPID a partir del subject configurado
// (NETPULSE_VAPID_SUBJECT) o, si no hay, de NETPULSE_PUBLIC_URL. Solo se acepta
// una URL https: o un mailto: con dominio real: un mailto:localhost devuelve
// 403 BadJwtToken en Apple (issue #1974).
func SubjectFor(configured, publicURL string) string {
	if s := validSubject(configured); s != "" {
		return s
	}
	if s := validSubject(publicURL); s != "" {
		return s
	}
	return fallbackSubject
}

func validSubject(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(s), "mailto:") {
		addr := strings.TrimSpace(s[len("mailto:"):])
		at := strings.LastIndex(addr, "@")
		if at <= 0 || at == len(addr)-1 {
			return ""
		}
		domain := strings.ToLower(strings.TrimSpace(addr[at+1:]))
		if domain == "" || domain == "localhost" || !strings.Contains(domain, ".") {
			return ""
		}
		return s
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return s
}

// payload es el JSON cifrado que recibe el Service Worker (contrato con
// app/src/sw.ts: {title, body, category, severity, url, tag, hint}).
// Desde #689 viaja además {type, vars}: el slug estable del tipo de alerta y
// sus variables de interpolación, para que el SW traduzca la notificación al
// idioma del dispositivo. Los literales title/body/hint se mantienen como
// fallback (SW antiguo o tipo sin clave i18n).
type payload struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Type     string            `json:"type,omitempty"`
	Vars     map[string]string `json:"vars,omitempty"`
	Category string            `json:"category"`
	Severity string            `json:"severity"`
	URL      string            `json:"url"`
	Tag      string            `json:"tag"`            // alertId: dedup nativo del navegador
	Hint     string            `json:"hint,omitempty"` // sugerencia accionable (issue #310)
}

// Notifier implementa alerts.Notifier con entrega asíncrona.
type Notifier struct {
	db      *db.DB
	pub     string
	priv    string
	subject string
	client  *http.Client

	ch   chan alerts.AlertEvent
	done chan struct{} // cerrado en Close: Notify posterior es no-op
	once sync.Once
	wg   sync.WaitGroup
}

// NewNotifier crea el Notifier y arranca su worker. pub/priv son el par
// VAPID de EnsureVAPIDKeys.
func NewNotifier(d *db.DB, pub, priv string) *Notifier {
	return newNotifier(d, pub, priv, newSafeHTTPClient())
}

// NewNotifierWithClient permite inyectar un cliente HTTP. Se usa en tests que
// necesitan alcanzar un push service TLS local; produccion usa NewNotifier.
func NewNotifierWithClient(d *db.DB, pub, priv string, client *http.Client) *Notifier {
	if client == nil {
		client = newSafeHTTPClient()
	}
	return newNotifier(d, pub, priv, client)
}

func newNotifier(d *db.DB, pub, priv string, client *http.Client) *Notifier {
	n := &Notifier{
		db:      d,
		pub:     pub,
		priv:    priv,
		subject: fallbackSubject,
		client:  client,
		ch:      make(chan alerts.AlertEvent, queueCap),
		done:    make(chan struct{}),
	}
	n.wg.Add(1)
	go n.worker()
	return n
}

// SetSubject fija el sub del JWT VAPID (ver SubjectFor). Sin llamada se usa
// fallbackSubject, que es un https: valido para los push services.
func (n *Notifier) SetSubject(subject string) {
	if s := validSubject(subject); s != "" {
		n.subject = s
	}
}

// Notify encola el evento y vuelve inmediatamente (nunca bloquea el poll).
// Si la cola está llena descarta con aviso: el evento ya está en /api/alerts.
func (n *Notifier) Notify(ev alerts.AlertEvent) {
	select {
	case <-n.done:
		return
	case n.ch <- ev:
	default:
		slog.Warn("push: queue full, notification dropped", "alertId", ev.ID, "title", ev.Title)
	}
}

// Close para el worker y espera al envío en curso (acotado por sendTimeout).
func (n *Notifier) Close() {
	n.once.Do(func() {
		close(n.done)
		n.wg.Wait()
	})
}

func (n *Notifier) worker() {
	defer n.wg.Done()
	for {
		select {
		case <-n.done:
			return
		case ev := <-n.ch:
			n.sendAll(ev)
		}
	}
}

// payloadJSON serializa el cuerpo de la notificación (SPEC-PUSH §1).
func payloadJSON(ev alerts.AlertEvent) ([]byte, error) {
	return json.Marshal(payload{
		Title:    ev.Title,
		Body:     ev.Description,
		Type:     ev.Type,
		Vars:     ev.Vars,
		Category: ev.Category,
		Severity: ev.Severity,
		URL:      "/alerts",
		Tag:      ev.ID,
		Hint:     ev.Hint,
	})
}

// sendAll envía el evento a TODAS las suscripciones y purga las muertas.
// El payload se serializa POR suscripción (#1334: idioma por suscriptor).
func (n *Notifier) sendAll(ev alerts.AlertEvent) {
	subs, err := ListSubscriptions(n.db)
	if err != nil {
		slog.Error("push: could not list subscriptions", "alertId", ev.ID, "error", err)
		return
	}
	for _, s := range subs {
		// #1334: la suscripción recuerda el idioma efectivo de la UI en su
		// alta; se compone el payload en ESE idioma (catálogo alertlang, el
		// mismo de la traducción global). Sin idioma (legacy): el payload ya
		// llega traducido al idioma global por alertlang.Notifier.
		evs := ev
		if s.Language != "" {
			evs = alertlang.LocalizeLang(ev, s.Language)
		}
		body, err := payloadJSON(evs)
		if err != nil {
			slog.Error("push: payload no serializable", "alertId", ev.ID, "error", err)
			continue
		}
		n.sendOne(evs, body, s)
	}
}

// wireSubject adapta el subject al formato de webpush-go: la libreria antepone
// "mailto:" salvo que empiece por "https:", asi que un mailto: explicito se
// pasa sin prefijo para no acabar en "mailto:mailto:...".
func wireSubject(s string) string {
	if strings.HasPrefix(strings.ToLower(s), "mailto:") {
		return s[len("mailto:"):]
	}
	return s
}

func pushReason(body []byte) string {
	s := strings.TrimSpace(string(body))
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func (n *Notifier) sendOne(ev alerts.AlertEvent, body []byte, s Subscription) {
	if err := ValidateEndpoint(s.Endpoint); err != nil {
		slog.Warn("push: endpoint rechazado", "endpoint", s.Endpoint, "error", err)
		return
	}
	resp, err := webpush.SendNotificationWithContext(context.Background(), body,
		&webpush.Subscription{
			Endpoint: s.Endpoint,
			Keys:     webpush.Keys{Auth: s.Auth, P256dh: s.P256dh},
		},
		&webpush.Options{
			HTTPClient:      n.client,
			Subscriber:      wireSubject(n.subject),
			VAPIDPublicKey:  n.pub,
			VAPIDPrivateKey: n.priv,
			TTL:             ttlSeconds,
		})
	if err != nil {
		slog.Warn("push: send failed", "alertId", ev.ID, "endpoint", s.Endpoint, "error", err)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		// Suscripción muerta (expirada o dada de baja): purgar (SPEC-PUSH §1).
		if err := DeleteSubscription(n.db, s.Endpoint); err != nil {
			slog.Error("push: could not purge dead subscription", "endpoint", s.Endpoint, "error", err)
		}
		slog.Info("push: subscription purged", "endpoint", s.Endpoint, "status", resp.StatusCode)
		return
	}
	if resp.StatusCode >= 400 {
		// #1974: el cuerpo del push service dice la causa (Apple devuelve
		// {"reason":"BadJwtToken"}). Sin el, un 403 pelado no diagnostica nada.
		slog.Warn("push: push service returned an error", "alertId", ev.ID, "endpoint", s.Endpoint,
			"status", resp.StatusCode, "reason", pushReason(respBody))
	}
}
