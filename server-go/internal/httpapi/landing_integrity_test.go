package httpapi_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var scriptSrcRe = regexp.MustCompile(`<script[^>]*src="([^"]+)"[^>]*>`)

func TestLandingScriptsArePinned(t *testing.T) {
	for _, name := range []string{"index.html", "features.html", "home-assistant.html"} {
		path := "../../../web/" + name
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range scriptSrcRe.FindAllStringSubmatch(string(b), -1) {
			src, tag := m[1], m[0]
			if strings.HasPrefix(src, "//") {
				t.Errorf("%s: protocol-relative script %s", name, src)
			}
			if strings.HasPrefix(src, "http") && !strings.Contains(tag, "integrity=") {
				t.Errorf("%s: external script without integrity: %s", name, src)
			}
		}
	}
}

func TestAppIndexUsesFirstPartyFonts(t *testing.T) {
	b, err := os.ReadFile("../../../app/index.html")
	if err != nil {
		t.Fatalf("read app/index.html: %v", err)
	}
	html := string(b)
	if strings.Contains(html, "fonts.googleapis.com") || strings.Contains(html, "fonts.gstatic.com") {
		t.Fatal("app/index.html still loads Google Fonts")
	}
	if !strings.Contains(html, `href="/fonts/fonts.css"`) {
		t.Fatal("app/index.html does not load the self-hosted fonts")
	}
}
