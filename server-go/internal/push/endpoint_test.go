package push_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/push"
)

func TestValidateEndpoint(t *testing.T) {
	valid := []string{
		"https://push.example/abc",
		"https://fcm.googleapis.com/wp/abc",
		"https://8.8.8.8/x",
		"https://[2001:4860:4860::8888]/x",
		"https://push.example:8443/x",
	}
	for _, ep := range valid {
		if err := push.ValidateEndpoint(ep); err != nil {
			t.Errorf("ValidateEndpoint(%q) = %v, want nil", ep, err)
		}
	}

	invalid := []string{
		"",
		"http://push.example/x",
		"ftp://push.example/x",
		"https:///x",
		"https://user:pass@push.example/x",
		"https://localhost/x",
		"https://foo.localhost/x",
		"https://127.0.0.1/x",
		"https://[::1]/x",
		"https://10.0.0.1/x",
		"https://172.16.0.1/x",
		"https://192.168.1.1/x",
		"https://169.254.169.254/latest/meta-data",
		"https://0.0.0.0/x",
		"https://100.64.0.1/x",
		"https://198.18.0.1/x",
		"https://192.0.2.1/x",
		"https://198.51.100.1/x",
		"https://203.0.113.1/x",
		"https://240.0.0.1/x",
		"https://[::ffff:127.0.0.1]/x",
		"https://[fc00::1]/x",
		"https://[fe80::1]/x",
		"https://[2001:db8::1]/x",
		"https://[64:ff9b::7f00:1]/x",
		"https://push.example:99999/x",
	}
	for _, ep := range invalid {
		if err := push.ValidateEndpoint(ep); err == nil {
			t.Errorf("ValidateEndpoint(%q) = nil, want error", ep)
		}
	}
}

func TestNotifierSkipsPrivateEndpoint(t *testing.T) {
	d, _ := openDB(t)
	ua := newBrowserSub(t)
	hit := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit <- struct{}{}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	if _, err := push.UpsertSubscription(d, push.Subscription{
		Endpoint: srv.URL, Auth: ua.authB, P256dh: ua.pubB,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	n := newNotifier(t, d)
	n.Notify(testEvent())
	select {
	case <-hit:
		t.Fatal("el notifier alcanzo un endpoint http de loopback")
	case <-time.After(500 * time.Millisecond):
	}
}
