// push_test_1973_test.go - el envio de prueba usa el camino real: mismo
// cifrado, mismo VAPID, y devuelve status y reason (#1973).
package push_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/push"
)

func TestSendTestReachesTheService(t *testing.T) {
	d, _ := openDB(t)
	ua := newBrowserSub(t)

	got := make(chan capturedPush, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- capturedPush{method: r.Method, header: r.Header.Clone(), body: body}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	n := newNotifier(t, d, testPushClient(srv))
	defer n.Close()

	res := n.SendTest(push.Subscription{
		Endpoint: "https://push.example/send", Auth: ua.authB, P256dh: ua.pubB, Language: "es",
	})
	if res.Error != "" || res.Status != http.StatusCreated {
		t.Fatalf("resultado = %+v", res)
	}

	var sent capturedPush
	select {
	case sent = <-got:
	case <-time.After(15 * time.Second):
		t.Fatal("el push service simulado no recibio nada en 15 s")
	}
	if !strings.HasPrefix(sent.header.Get("Authorization"), "vapid t=") {
		t.Fatalf("Authorization VAPID: %q", sent.header.Get("Authorization"))
	}
	plain := string(decryptAES128GCM(t, ua, sent.body))
	if !strings.Contains(plain, "Notificacion de prueba") {
		t.Fatalf("payload sin el texto de prueba: %q", plain)
	}
}

func TestSendTestReportsTheReason(t *testing.T) {
	d, _ := openDB(t)
	ua := newBrowserSub(t)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"reason":"BadJwtToken"}`))
	}))
	defer srv.Close()

	if _, err := push.UpsertSubscription(d, push.Subscription{
		Endpoint: "https://push.example/send", Auth: ua.authB, P256dh: ua.pubB, UserID: 1,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	n := newNotifier(t, d, testPushClient(srv))
	defer n.Close()

	res := n.SendTest(push.Subscription{Endpoint: "https://push.example/send", Auth: ua.authB, P256dh: ua.pubB})
	if res.Status != http.StatusForbidden {
		t.Fatalf("status = %d, esperaba 403", res.Status)
	}
	if !strings.Contains(res.Reason, "BadJwtToken") {
		t.Fatalf("reason = %q, esperaba el motivo del push service", res.Reason)
	}
	if res.Expired {
		t.Fatal("un 403 no caduca la suscripcion")
	}
	if _, ok, _ := push.SubscriptionForUser(d, "https://push.example/send", 1); !ok {
		t.Fatal("la suscripcion no debe borrarse con un 403")
	}
}

func TestSendTestPurgesExpiredSubscription(t *testing.T) {
	d, _ := openDB(t)
	ua := newBrowserSub(t)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	if _, err := push.UpsertSubscription(d, push.Subscription{
		Endpoint: "https://push.example/send", Auth: ua.authB, P256dh: ua.pubB, UserID: 7,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	n := newNotifier(t, d, testPushClient(srv))
	defer n.Close()

	res := n.SendTest(push.Subscription{Endpoint: "https://push.example/send", Auth: ua.authB, P256dh: ua.pubB})
	if !res.Expired || res.Status != http.StatusGone {
		t.Fatalf("resultado = %+v", res)
	}
	if _, ok, _ := push.SubscriptionForUser(d, "https://push.example/send", 7); ok {
		t.Fatal("la suscripcion caducada debe purgarse")
	}
}

func TestSendTestRejectsInvalidEndpoint(t *testing.T) {
	d, _ := openDB(t)
	n := newNotifier(t, d)
	defer n.Close()
	res := n.SendTest(push.Subscription{Endpoint: "http://plain.example/x"})
	if res.Error == "" {
		t.Fatalf("un endpoint http debe rechazarse: %+v", res)
	}
}
