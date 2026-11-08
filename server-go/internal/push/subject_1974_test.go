// subject_1974_test.go - contrato del sujeto VAPID: Apple rechaza con 403
// BadJwtToken un sub mailto:localhost (#1974). El Notifier debe enviar un
// https: valido o un mailto: con dominio real.
package push_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/push"
)

func TestSubjectFor(t *testing.T) {
	const fallback = "https://github.com/gnacho/netpulse"
	cases := []struct {
		name     string
		conf     string
		public   string
		expected string
	}{
		{"configured wins", "mailto:ops@example.com", "https://net.example.com", "mailto:ops@example.com"},
		{"public https", "", "https://net.example.com", "https://net.example.com"},
		{"public https with trailing space", "", "  https://net.example.com  ", "https://net.example.com"},
		{"public http is not valid", "", "http://192.168.1.50:8080", fallback},
		{"localhost email is not valid", "mailto:netpulse@localhost", "", fallback},
		{"email without domain dot is not valid", "mailto:ops@intranet", "", fallback},
		{"empty email address", "mailto:@example.com", "", fallback},
		{"nothing configured", "", "", fallback},
		{"fallback is never localhost", "", "", fallback},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := push.SubjectFor(c.conf, c.public)
			if got != c.expected {
				t.Fatalf("SubjectFor(%q, %q) = %q, esperaba %q", c.conf, c.public, got, c.expected)
			}
			if strings.Contains(strings.ToLower(got), "localhost") {
				t.Fatalf("subject con localhost: %q", got)
			}
		})
	}
}

// El JWT enviado al push service lleva el sub elegido, no el constante viejo.
func TestNotifierSendsConfiguredSubject(t *testing.T) {
	d, _ := openDB(t)
	ua := newBrowserSub(t)

	got := make(chan capturedPush, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		got <- capturedPush{method: r.Method, header: r.Header.Clone()}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	if _, err := push.UpsertSubscription(d, push.Subscription{
		Endpoint: "https://push.example/send", Auth: ua.authB, P256dh: ua.pubB,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	n := newNotifier(t, d, testPushClient(srv))
	n.SetSubject("mailto:ops@example.com")
	n.Notify(testEvent())

	var req capturedPush
	select {
	case req = <-got:
	case <-time.After(15 * time.Second):
		t.Fatal("el push service simulado no recibio nada en 15 s")
	}

	sub := jwtSubject(t, req.header.Get("Authorization"))
	if sub != "mailto:ops@example.com" {
		t.Fatalf("sub del JWT = %q, esperaba mailto:ops@example.com", sub)
	}
}

// jwtSubject extrae el claim sub del JWT de la cabecera VAPID (vapid t=<jwt>, k=...).
func jwtSubject(t *testing.T, authz string) string {
	t.Helper()
	const marker = "t="
	i := strings.Index(authz, marker)
	if i < 0 {
		t.Fatalf("Authorization sin token: %q", authz)
	}
	tok := authz[i+len(marker):]
	if j := strings.IndexByte(tok, ','); j >= 0 {
		tok = tok[:j]
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT con %d partes: %q", len(parts), tok)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("payload base64: %v", err)
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("claims JSON: %v", err)
	}
	return claims.Sub
}
