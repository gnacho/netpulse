// ntfy_test.go - tests del canal ntfy (#766): publicacion, reintentos,
// no-retry en 4xx, formato y config (topic/server/token write-only).
package ntfy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

type fakeKV struct{ m map[string]string }

func (f *fakeKV) Get(key string) (string, bool) {
	v, ok := f.m[key]
	return v, ok
}
func (f *fakeKV) Set(key, value string) error {
	if f.m == nil {
		f.m = map[string]string{}
	}
	f.m[key] = value
	return nil
}

func TestPublishPostsTitleAndPriority(t *testing.T) {
	var gotPath, gotTitle, gotPrio, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTitle = r.Header.Get("Title")
		gotPrio = r.Header.Get("Priority")
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(200)
	}))
	defer srv.Close()

	n := &Notifier{client: srv.Client(), done: make(chan struct{})}
	cfg := Config{Server: srv.URL, Topic: "mi-topic-secreto", Token: "tk123"}
	if err := n.publish(cfg, "Router caído", "🔴 descripción", "urgent"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if gotPath != "/mi-topic-secreto" {
		t.Fatalf("path: %s", gotPath)
	}
	if gotTitle != "Router caído" || gotPrio != "urgent" {
		t.Fatalf("headers: %q %q", gotTitle, gotPrio)
	}
	if gotAuth != "Bearer tk123" {
		t.Fatalf("auth: %q", gotAuth)
	}
	if gotBody == "" {
		t.Fatalf("body vacío")
	}
}

func TestPublishNoRetryOn4xx(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(403)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()
	n := &Notifier{client: srv.Client(), done: make(chan struct{})}
	err := n.sendWithRetry(Config{Server: srv.URL, Topic: "x"}, alerts.AlertEvent{Title: "t", Urgent: true})
	if err == nil {
		t.Fatalf("403 debe fallar")
	}
	if calls != 1 {
		t.Fatalf("4xx no reintenta: %d llamadas", calls)
	}
}

func TestPublishRetriesOn5xxThenOK(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(502)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := &Notifier{client: srv.Client(), done: make(chan struct{})}
	// Retry inmediato no existe: baseRetry 2s haría el test lento; aceptamos
	// la espera del primer reintento con un timeout holgado.
	done := make(chan error, 1)
	go func() {
		done <- n.sendWithRetry(Config{Server: srv.URL, Topic: "x"}, alerts.AlertEvent{Title: "t"})
	}()
	<-done
	if calls != 2 {
		t.Fatalf("esperaba 2 llamadas (1 fallo + reintento OK): %d", calls)
	}
}

func TestNotifyQueueNoConfigNoop(t *testing.T) {
	kv := &fakeKV{}
	n := NewNotifier(kv)
	n.Notify(alerts.AlertEvent{Title: "x", Urgent: true})
	n.Close() // sin config no debe intentar enviar ni bloquear
}

func TestConfigValidation(t *testing.T) {
	kv := &fakeKV{}
	if err := SaveConfig(kv, Config{Topic: "tema con espacios"}); err == nil {
		t.Fatalf("topic inválido debe fallar")
	}
	if err := SaveConfig(kv, Config{Server: "ftp://x", Topic: "ok"}); err == nil {
		t.Fatalf("server no-http debe fallar")
	}
	if err := SaveConfig(kv, Config{Topic: "netpulse_abc-123", Enabled: true}); err != nil {
		t.Fatalf("valida: %v", err)
	}
	got := LoadConfig(kv)
	if got.Server != DefaultServer || got.Topic != "netpulse_abc-123" || !got.Enabled {
		t.Fatalf("roundtrip: %+v", got)
	}
	// Topic vacío permitido (limpiar canal): LoadConfig devuelve default.
	if err := SaveConfig(kv, Config{}); err != nil {
		t.Fatalf("limpiar: %v", err)
	}
	if got := LoadConfig(kv); got.Topic != "" || got.Enabled {
		t.Fatalf("tras limpiar: %+v", got)
	}
}

func TestFormatMessage(t *testing.T) {
	msg := formatMessage(alerts.AlertEvent{Title: "Firmware actualizado", Description: "rt: 25.12.5 -> 25.12.7", RouterID: "gateway", Urgent: true, Ts: 1700000000})
	if msg == "" || !contains(msg, "Firmware actualizado") || !contains(msg, "gateway") {
		t.Fatalf("formato: %q", msg)
	}
}

// B1: el topic (secreto del canal) nunca se registra en claro.
func TestMaskTopic(t *testing.T) {
	got := MaskTopic("top-secreto-123")
	if contains(got, "top-secreto-123") || !contains(got, "***") {
		t.Fatalf("el topic debe quedar enmascarado: %q", got)
	}
	if got := MaskTopic(""); got != "" {
		t.Fatalf("topic vacío: %q", got)
	}
}

// B2: el error de red no debe filtrar el topic (URL saneada).
func TestPublishErrorRedactsTopic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()
	n := &Notifier{client: &http.Client{Timeout: 2 * time.Second}, done: make(chan struct{})}
	err := n.publish(Config{Server: base, Topic: "top-secreto-abc"}, "t", "b", "default")
	if err == nil {
		t.Fatalf("esperaba error con el server cerrado")
	}
	if contains(err.Error(), "top-secreto-abc") {
		t.Fatalf("el topic no debe aparecer en el error: %q", err.Error())
	}
	if !contains(err.Error(), "***") {
		t.Fatalf("esperaba el topic enmascarado: %q", err.Error())
	}
}

// B3: solo se reintenta 5xx, 429 y errores de red transitorios.
func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"status 500", errors.New("status 500: boom"), true},
		{"status 503", errors.New("status 503: boom"), true},
		{"status 429", errors.New("status 429: slow down"), true},
		{"status 400", errors.New("status 400: bad"), false},
		{"status 401", errors.New("status 401: bad"), false},
		{"status 403", errors.New("status 403: bad"), false},
		{"status 404", errors.New("status 404: bad"), false},
		{"url inválida", &url.Error{Op: "parse", URL: "http://x/y", Err: errors.New("invalid")}, false},
		{"contexto cancelado", &url.Error{Op: "Post", URL: "http://x/y", Err: context.Canceled}, false},
		{"timeout", &url.Error{Op: "Post", URL: "http://x/y", Err: context.DeadlineExceeded}, true},
		{"conexión rechazada", &url.Error{Op: "Post", URL: "http://x/y", Err: syscall.ECONNREFUSED}, true},
	}
	for _, tc := range cases {
		if got := isRetryable(tc.err); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

// B4: el truncado no debe partir runas multibyte.
func TestFormatMessageTruncatesOnRuneBoundary(t *testing.T) {
	desc := strings.Repeat("é", 600) // 1200 bytes, >500
	msg := formatMessage(alerts.AlertEvent{Title: "t", Description: desc, Ts: 1700000000})
	if !utf8.ValidString(msg) {
		t.Fatalf("mensaje con UTF-8 inválido")
	}
	if !contains(msg, "...") {
		t.Fatalf("esperaba recorte de la descripción: %q", msg)
	}

	big := strings.Repeat("日", 3000) // 9000 bytes, supera maxMsgLen
	msg = formatMessage(alerts.AlertEvent{Title: big, Ts: 1700000000})
	if !utf8.ValidString(msg) {
		t.Fatalf("mensaje largo con UTF-8 inválido")
	}
	if len(msg) > maxMsgLen {
		t.Fatalf("mensaje por encima del límite: %d bytes", len(msg))
	}
}

// B7: el test no publica si el canal está desactivado.
func TestSendTestRequiresEnabled(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(200)
	}))
	defer srv.Close()
	kv := &fakeKV{m: map[string]string{
		"ntfy.server":  srv.URL,
		"ntfy.topic":   "x",
		"ntfy.enabled": "false",
	}}
	if err := SendTest(kv); err == nil {
		t.Fatalf("test debe fallar con el canal desactivado")
	}
	if calls != 0 {
		t.Fatalf("no debe publicar desactivado: %d", calls)
	}
	kv.m["ntfy.enabled"] = "true"
	if err := SendTest(kv); err != nil {
		t.Fatalf("test con canal activo: %v", err)
	}
	if calls != 1 {
		t.Fatalf("debe publicar una vez activo: %d", calls)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
