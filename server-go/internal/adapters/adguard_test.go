package adapters

import (
	"context"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/config"
)

func TestAdGuardClientEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		rawURL     string
		wantScheme string
		wantHost   string
		wantPort   int
	}{
		{name: "HTTP default", rawURL: "http://adguard.example.com", wantScheme: "http", wantHost: "adguard.example.com", wantPort: 80},
		{name: "HTTPS default", rawURL: "https://adguard.example.com", wantScheme: "https", wantHost: "adguard.example.com", wantPort: 443},
		{name: "HTTP explicit port", rawURL: "http://adguard.example.com:3000", wantScheme: "http", wantHost: "adguard.example.com", wantPort: 3000},
		{name: "HTTPS explicit port", rawURL: "https://adguard.example.com:8443", wantScheme: "https", wantHost: "adguard.example.com", wantPort: 8443},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewAdGuardClient(tt.rawURL, "admin", "secret")
			scheme, host, port := client.endpoint()
			if scheme != tt.wantScheme || host != tt.wantHost || port != tt.wantPort {
				t.Fatalf("endpoint = %s://%s:%d, want %s://%s:%d", scheme, host, port, tt.wantScheme, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestPollAdGuardInactivePreservesEndpoint(t *testing.T) {
	live := NewLive(&config.Config{Adguard: &config.AdGuard{
		URL:  "https://adguard.example.com",
		User: "admin",
		Pass: "secret",
	}}, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stats := live.pollAdGuard(ctx)
	if stats == nil {
		t.Fatal("inactive stats are nil")
	}
	if stats.Scheme != "https" || stats.Host != "adguard.example.com" || stats.Port != 443 {
		t.Fatalf("inactive endpoint = %s://%s:%d, want https://adguard.example.com:443", stats.Scheme, stats.Host, stats.Port)
	}
}
