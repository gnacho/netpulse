package security

import (
	"strings"
	"testing"
)

func TestCSPKeepsFontsFirstParty(t *testing.T) {
	for _, h := range Headers {
		if h.K != "Content-Security-Policy" {
			continue
		}
		if strings.Contains(h.V, "fonts.googleapis.com") || strings.Contains(h.V, "fonts.gstatic.com") {
			t.Fatalf("CSP still allows third-party fonts: %s", h.V)
		}
		if !strings.Contains(h.V, "font-src 'self'") {
			t.Fatalf("CSP must allow first-party fonts: %s", h.V)
		}
		return
	}
	t.Fatal("Content-Security-Policy header missing")
}
