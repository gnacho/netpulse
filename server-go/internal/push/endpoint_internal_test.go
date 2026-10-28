package push

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"100.64.0.1", "198.18.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1",
		"240.0.0.1", "0.0.0.0", "::1", "fc00::1", "fe80::1", "2001:db8::1",
		"64:ff9b::7f00:1",
	}
	for _, raw := range blocked {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf("IP invalida en el test: %s", raw)
		}
		if !blockedIP(ip) {
			t.Errorf("blockedIP(%s) = false, want true", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"} {
		ip := net.ParseIP(raw)
		if blockedIP(ip) {
			t.Errorf("blockedIP(%s) = true, want false", raw)
		}
	}
}

func TestSafeDialContextRejectsPrivateHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := safeDialContext(ctx, "tcp", "localhost:443"); err == nil {
		t.Fatal("safeDialContext acepto localhost")
	}
}

func TestSafeClientRejectsPrivateRedirect(t *testing.T) {
	client := newSafeHTTPClient()
	req, err := http.NewRequest(http.MethodGet, "https://127.0.0.1/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(req, nil); err == nil {
		t.Fatal("CheckRedirect acepto una redireccion a loopback")
	}
}
