// Package ntfy - canal de notificaciones vía ntfy (ntfy.sh o self-hosted,
// issue #766). Mismo contrato que el resto de la cadena (#326): solo eventos
// urgentes no suprimidos, cola con reintentos y fail-silent (una caída del
// server nunca rompe el flujo de alertas).
//
// Publicar en ntfy es un POST HTTP a <server>/<topic> con el mensaje en el
// body y metadatos en cabeceras (Title/Tags/Priority); auth opcional con
// Bearer token para topics reservados.
package ntfy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

const (
	kvKeyServer  = "ntfy.server"
	kvKeyTopic   = "ntfy.topic"
	kvKeyToken   = "ntfy.token"
	kvKeyEnabled = "ntfy.enabled"

	// DefaultServer: ntfy.sh público; un self-hosted se configura igual.
	DefaultServer = "https://ntfy.sh"

	queueCap    = 64
	maxRetries  = 3
	baseRetry   = 2 * time.Second
	sendTimeout = 10 * time.Second
	maxMsgLen   = 4096
)

type kvStore interface {
	Get(key string) (string, bool)
	Set(key, value string) error
}

// Config persistida en kv. El topic actúa como secreto (quien lo conoce
// puede suscribirse): se recomienda uno largo y aleatorio.
type Config struct {
	Server  string `json:"server"`
	Topic   string `json:"topic"`
	Token   string `json:"token,omitempty"` // write-only: nunca vuelve por la API
	Enabled bool   `json:"enabled"`
}

// LoadConfig lee la config con defaults sanos (server vacío = ntfy.sh).
func LoadConfig(kv kvStore) Config {
	cfg := Config{}
	if v, ok := kv.Get(kvKeyServer); ok && strings.TrimSpace(v) != "" {
		cfg.Server = strings.TrimRight(strings.TrimSpace(v), "/")
	}
	if cfg.Server == "" {
		cfg.Server = DefaultServer
	}
	if v, ok := kv.Get(kvKeyTopic); ok {
		cfg.Topic = v
	}
	if v, ok := kv.Get(kvKeyToken); ok {
		cfg.Token = v
	}
	if v, ok := kv.Get(kvKeyEnabled); ok {
		cfg.Enabled = v == "true"
	}
	return cfg
}

// SaveConfig valida y persiste. Topic: [a-zA-Z0-9_-]{1,64}; server http(s).
func SaveConfig(kv kvStore, cfg Config) error {
	cfg.Server = strings.TrimRight(strings.TrimSpace(cfg.Server), "/")
	if cfg.Server == "" {
		cfg.Server = DefaultServer
	}
	if !strings.HasPrefix(cfg.Server, "http://") && !strings.HasPrefix(cfg.Server, "https://") {
		return fmt.Errorf("server debe ser una URL http(s)")
	}
	topic := strings.TrimSpace(cfg.Topic)
	if topic == "" {
		// Vacío = limpiar el canal (disabled): se permite.
		cfg.Topic = ""
	} else if !validTopic(topic) {
		return fmt.Errorf("topic inválido (letras, números, - y _, máx 64)")
	}
	if err := kv.Set(kvKeyServer, cfg.Server); err != nil {
		return fmt.Errorf("save server: %w", err)
	}
	if err := kv.Set(kvKeyTopic, cfg.Topic); err != nil {
		return fmt.Errorf("save topic: %w", err)
	}
	if err := kv.Set(kvKeyToken, strings.TrimSpace(cfg.Token)); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	enabled := "false"
	if cfg.Enabled {
		enabled = "true"
	}
	if err := kv.Set(kvKeyEnabled, enabled); err != nil {
		return fmt.Errorf("save enabled: %w", err)
	}
	return nil
}

func validTopic(t string) bool {
	if len(t) == 0 || len(t) > 64 {
		return false
	}
	for _, r := range t {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Notifier satisface alerts.Notifier.
type Notifier struct {
	kv     kvStore
	queue  chan alerts.AlertEvent
	done   chan struct{}
	wg     sync.WaitGroup
	client *http.Client
}

// NewNotifier arranca el worker de la cola.
func NewNotifier(kv kvStore) *Notifier {
	n := &Notifier{
		kv:     kv,
		queue:  make(chan alerts.AlertEvent, queueCap),
		done:   make(chan struct{}),
		client: &http.Client{Timeout: sendTimeout},
	}
	n.wg.Add(1)
	go n.worker()
	return n
}

func (n *Notifier) Notify(ev alerts.AlertEvent) {
	select {
	case n.queue <- ev:
	default:
		slog.Warn("ntfy: queue full, dropping alert", "title", ev.Title)
	}
}

// Close detiene el worker.
func (n *Notifier) Close() {
	close(n.done)
	n.wg.Wait()
}

func (n *Notifier) worker() {
	defer n.wg.Done()
	for {
		select {
		case <-n.done:
			return
		case ev := <-n.queue:
			cfg := LoadConfig(n.kv)
			if !cfg.Enabled || cfg.Topic == "" {
				continue
			}
			if err := n.sendWithRetry(cfg, ev); err != nil {
				slog.Error("ntfy: send failed after retries", "error", err, "title", ev.Title)
			}
		}
	}
}

func (n *Notifier) sendWithRetry(cfg Config, ev alerts.AlertEvent) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := baseRetry * time.Duration(1<<(attempt-1))
			select {
			case <-time.After(backoff):
			case <-n.done:
				return fmt.Errorf("shutdown during retry")
			}
		}
		if err := n.publish(cfg, ev.Title, formatMessage(ev), priorityOf(ev)); err != nil {
			lastErr = err
			if !isRetryable(err) {
				return err
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("max retries: %w", lastErr)
}

func priorityOf(ev alerts.AlertEvent) string {
	if ev.Urgent {
		return "urgent"
	}
	return "default"
}

// publish envía un mensaje a <server>/<topic> con Title/Priority en cabeceras.
func (n *Notifier) publish(cfg Config, title, body, priority string) error {
	url := cfg.Server + "/" + cfg.Topic
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return redactErr("new request", err, cfg)
	}
	req.Header.Set("Title", title)
	req.Header.Set("Priority", priority)
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return redactErr("http do", err, cfg)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

// redactedError conserva la cadena de errores (Unwrap) para isRetryable pero
// expone un mensaje sin el topic, que es el secreto del canal.
type redactedError struct {
	err error
	msg string
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// redactErr sustituye el topic por *** en cualquier error que pueda incluir la
// URL (<server>/<topic>).
func redactErr(prefix string, err error, cfg Config) error {
	msg := err.Error()
	if cfg.Topic != "" {
		full := cfg.Server + "/" + cfg.Topic
		msg = strings.ReplaceAll(msg, full, cfg.Server+"/***")
	}
	return &redactedError{err: err, msg: prefix + ": " + msg}
}

// isRetryable decide si un fallo de publish merece reintento: solo 5xx, 429 y
// errores de red transitorios (timeout, conexión rechazada/reseteada). Los
// permanentes (4xx, URL inválida, DNS, TLS, contexto) no se reintentan.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if code, ok := statusCode(err.Error()); ok {
		return code >= 500 || code == 429
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	for _, target := range []error{
		syscall.ECONNREFUSED,
		syscall.ECONNRESET,
		syscall.ECONNABORTED,
		syscall.EPIPE,
		io.ErrUnexpectedEOF,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// statusCode extrae el código de un error con formato "status NNN: ...".
func statusCode(msg string) (int, bool) {
	const prefix = "status "
	i := strings.Index(msg, prefix)
	if i < 0 {
		return 0, false
	}
	code, digits := 0, 0
	for _, r := range msg[i+len(prefix):] {
		if r < '0' || r > '9' {
			break
		}
		code = code*10 + int(r-'0')
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	return code, true
}

// formatMessage renderiza la alerta a texto plano (ntfy no parsea HTML).
func formatMessage(ev alerts.AlertEvent) string {
	severity := "⚠️"
	if ev.Urgent {
		severity = "🔴"
	}
	ts := time.Unix(ev.Ts, 0).Format("15:04:05")
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", severity, ev.Title)
	if ev.Description != "" {
		desc := ev.Description
		if len(desc) > 500 {
			desc = truncateUTF8(desc, 500) + "..."
		}
		b.WriteString(desc)
		b.WriteString("\n")
	}
	if ev.RouterID != "" {
		fmt.Fprintf(&b, "📡 %s\n", ev.RouterID)
	}
	fmt.Fprintf(&b, "🕐 %s", ts)
	out := b.String()
	if len(out) > maxMsgLen {
		out = truncateUTF8(out, maxMsgLen-3) + "..."
	}
	return out
}

// truncateUTF8 recorta s a lo sumo maxBytes bytes sin partir una runa
// multibyte (el corte por bytes crudo podía dejar UTF-8 inválido).
func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size > 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// MaskTopic redacta el topic (secreto del canal) para logs y diagnósticos:
// nunca expone su valor, solo su longitud.
func MaskTopic(topic string) string {
	if topic == "" {
		return ""
	}
	return fmt.Sprintf("*** (%d chars)", len(topic))
}

// SendTest publica un mensaje de prueba contra la config guardada (botón de
// la UI): es a la vez la validación del canal (ntfy no tiene getMe). Exige el
// canal activado, igual que el worker antes de enviar.
func SendTest(kv kvStore) error {
	cfg := LoadConfig(kv)
	if !cfg.Enabled {
		return fmt.Errorf("el canal ntfy está desactivado")
	}
	if cfg.Topic == "" {
		return fmt.Errorf("topic es requerido")
	}
	n := &Notifier{
		client: &http.Client{Timeout: sendTimeout},
		done:   make(chan struct{}),
	}
	defer close(n.done)
	return n.publish(cfg, "NetPulse", "✅ Notificaciones ntfy configuradas correctamente.", "default")
}
