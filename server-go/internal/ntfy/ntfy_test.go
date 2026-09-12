// ntfy_test.go - tests del canal ntfy (#766): publicacion, reintentos,
// no-retry en 4xx, formato y config (topic/server/token write-only).
package ntfy

import (
	"net/http"
	"net/http/httptest"
	"testing"

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

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
