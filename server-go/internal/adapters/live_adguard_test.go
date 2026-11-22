package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestLiveAdGuardQueryLogTLSConfig(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		scheme   string
		insecure string
		useTLS   bool
		wantErr  bool
	}{
		{name: "legacy stored HTTP without TLS keys", source: "stored", useTLS: false},
		{name: "stored HTTPS strict by default", source: "stored", scheme: "https", useTLS: true, wantErr: true},
		{name: "stored HTTPS explicit strict zero", source: "stored", scheme: "https", insecure: "0", useTLS: true, wantErr: true},
		{name: "stored HTTPS explicit opt-in", source: "stored", scheme: "https", insecure: "1", useTLS: true},
		{name: "environment HTTPS strict zero", source: "environment", insecure: "0", useTLS: true, wantErr: true},
		{name: "environment HTTPS explicit opt-in", source: "environment", insecure: "1", useTLS: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(sampleQueryLog))
			})
			var srv *httptest.Server
			if tt.useTLS {
				srv = httptest.NewTLSServer(handler)
			} else {
				srv = httptest.NewServer(handler)
			}
			defer srv.Close()

			var live *Live
			if tt.source == "environment" {
				live = NewLive(&config.Config{Adguard: &config.AdGuard{
					URL:                srv.URL,
					User:               "admin",
					Pass:               "secret",
					InsecureSkipVerify: tt.insecure == "1",
				}}, nil, nil, nil)
			} else {
				live = newStoredAdGuardLive(t, srv.URL, tt.scheme, tt.insecure)
			}
			client, glinet := live.getAdguardClient()
			if client == nil || glinet != nil {
				t.Fatalf("standard client = %v, GL.iNet client = %v", client, glinet)
			}
			page, err := client.QueryLog(context.Background(), "", 1000)
			if tt.wantErr {
				if err == nil {
					t.Fatal("self-signed certificate was accepted without opt-in")
				}
				return
			}
			if err != nil {
				t.Fatalf("QueryLog: %v", err)
			}
			if len(page.Queries) != 2 {
				t.Fatalf("queries = %d, want 2", len(page.Queries))
			}
		})
	}
}

type adGuardTestSetting struct {
	key   string
	value string
}

func newStoredAdGuardLive(t *testing.T, rawURL, scheme, insecure string) *Live {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	settings := []adGuardTestSetting{
		{key: "adguard_mode", value: "standard"},
		{key: "adguard_host", value: u.Hostname()},
		{key: "adguard_port", value: strconv.Itoa(port)},
		{key: "adguard_user", value: "admin"},
		{key: "adguard_pass", value: "secret"},
	}
	if scheme != "" {
		settings = append(settings, adGuardTestSetting{key: "adguard_scheme", value: scheme})
	}
	if insecure != "" {
		settings = append(settings, adGuardTestSetting{key: "adguard_insecure", value: insecure})
	}
	for _, setting := range settings {
		if _, err := d.Exec("INSERT INTO kv (key, value) VALUES (?, ?)", setting.key, setting.value); err != nil {
			t.Fatalf("insert %s: %v", setting.key, err)
		}
	}
	return NewLive(&config.Config{}, d, nil, nil)
}
