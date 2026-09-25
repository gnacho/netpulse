// main_test.go — regresión issue #57: notifierChain no debe paniquear con un
// nil-encapsulado (interfaz con tipo pero valor nil) entre sus miembros.
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// nilNotifier implementa alerts.Notifier; un puntero nil a este tipo,
// empaquetado en la interfaz, simula el bug (interfaz no-nil, valor nil).
// Notify accede a un campo del receiver (como webhook.Notifier.Notify accede
// a n.done/n.ch): con receiver nil esto paniquea si no se filtra.
type nilNotifier struct {
	done chan struct{}
}

func (n *nilNotifier) Notify(alerts.AlertEvent) {
	_ = n.done // accede al receiver: con n nil → panic (como webhook.Notify)
}

func TestNotifierChainIgnoraNilEncapsulado(t *testing.T) {
	// Cadena con: notifier real + puntero nil a *nilNotifier empaquetado en
	// la interfaz. Sin el fix, n.Notify paniquea (receiver nil).
	var real alerts.Notifier = &nilNotifier{}
	var nilPtr *nilNotifier
	chain := notifierChain{real, nilPtr}

	ev := alerts.AlertEvent{ID: "test", Title: "x", Urgent: true}
	chain.Notify(ev) // no debe paniquear
}

func TestNotifierChainConNilPlano(t *testing.T) {
	var real alerts.Notifier = &nilNotifier{}
	chain := notifierChain{real, nil}
	chain.Notify(alerts.AlertEvent{ID: "test2"}) // no debe paniquear
}

func TestIsSSEStreamPath(t *testing.T) {
	cases := map[string]bool{
		"/api/stream":                true,
		"/api/agents/gw/stream":      true,
		"/api/agents/gateway/stream": true,
		"/api/update/stream":         true, // FORK
		"/api/update/status":         false,
		"/api/agents/gw/binary":      false,
		"/api/agents/gw/refresh":     false,
		"/api/agents/gw/stream/x":    false,
		"/api/agents":                false,
		"/api/overview":              false,
		"/":                          false,
	}
	for p, want := range cases {
		if got := isSSEStreamPath(p); got != want {
			t.Errorf("isSSEStreamPath(%q) = %v, want %v", p, got, want)
		}
	}
}

// deadlineRecorder envuelve una ResponseWriter registrando si se llama a
// SetWriteDeadline y con qué deadline (para verificar el override de SSE).
type deadlineRecorder struct {
	http.ResponseWriter
	deadline time.Time
	set      bool
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadline = t
	d.set = true
	return nil
}

func TestWithSSEWriteTimeoutExtiendeDeadlineSSE(t *testing.T) {
	h := withSSEWriteTimeout(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), 24*time.Hour)
	for _, p := range []string{"/api/stream", "/api/agents/gateway/stream"} {
		rec := &deadlineRecorder{}
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if !rec.set {
			t.Fatalf("%s: debe extender el write deadline", p)
		}
		if rec.deadline.Before(time.Now().Add(23 * time.Hour)) {
			t.Fatalf("%s: deadline %v no es el esperado (24 h)", p, rec.deadline)
		}
	}
}

func TestWithSSEWriteTimeoutNoTocaNoSSE(t *testing.T) {
	h := withSSEWriteTimeout(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), 24*time.Hour)
	rec := &deadlineRecorder{}
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/overview", nil))
	if rec.set {
		t.Fatal("/api/overview no debe extender el write deadline")
	}
}

func TestWebhookHostForLog(t *testing.T) {
	// Issue #217: el log de arranque del webhook debe mostrar solo el host,
	// sin userinfo ni path (posibles credenciales embebidas).
	cases := map[string]string{
		"https://user:pass@hooks.example.com/hook/abc": "hooks.example.com",
		"https://hooks.example.com/path":               "hooks.example.com",
		"http://192.168.1.50:8080/h":                   "192.168.1.50:8080",
		"no-es-una-url":                                "(sin host)",
	}
	for raw, want := range cases {
		if got := webhookHostForLog(raw); got != want {
			t.Errorf("webhookHostForLog(%q) = %q, want %q", raw, got, want)
		}
	}
	if s := webhookHostForLog("https://user:pass@hooks.example.com/x"); strings.Contains(s, "pass") {
		t.Fatalf("el host no debe contener credenciales: %q", s)
	}
}

// #874: urgencyGate — default (sin kv) = solo urgentes; con
// "<canal>.urgent_only"="false" = pasa también lo no urgente.
func TestUrgencyGateDefaultUrgentOnly(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	spy := &spyN{}
	g := urgencyGate{n: spy, kv: &mainKVAdapter{db: d.DB}, key: "ntfy.urgent_only"}

	g.Notify(alerts.AlertEvent{ID: "u1", Urgent: true})
	g.Notify(alerts.AlertEvent{ID: "n1", Urgent: false})
	if len(spy.got) != 1 || spy.got[0].ID != "u1" {
		t.Fatalf("default: %+v", spy.got)
	}

	if _, err := d.DB.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		"ntfy.urgent_only", "false"); err != nil {
		t.Fatal(err)
	}
	g.Notify(alerts.AlertEvent{ID: "n2", Urgent: false})
	if len(spy.got) != 2 || spy.got[1].ID != "n2" {
		t.Fatalf("con urgent_only=false: %+v", spy.got)
	}
}

type spyN struct{ got []alerts.AlertEvent }

func (s *spyN) Notify(ev alerts.AlertEvent) { s.got = append(s.got, ev) }
