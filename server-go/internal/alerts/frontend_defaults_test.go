package alerts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// La config por defecto vive duplicada: DefaultConfig() aqui y
// DEFAULT_ALERTS_CONFIG en el frontend. Si una copia cambia sin la otra, el
// panel de ajustes del demo y el fallback live arrancan con niveles que el
// servidor no usa (#1672).
func TestFrontendDefaultsMatchBackend(t *testing.T) {
	const rel = "../../../app/src/data/alertConfig.ts"
	raw, err := os.ReadFile(filepath.Join(rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	src := string(raw)
	start := strings.Index(src, "export const DEFAULT_ALERTS_CONFIG")
	if start < 0 {
		t.Fatalf("%s: DEFAULT_ALERTS_CONFIG not found", rel)
	}
	block := src[start:]
	if end := strings.Index(block, "\n}"); end >= 0 {
		block = block[:end]
	}

	entries := regexp.MustCompile(`(\w+):\s*'(\w+)'`).FindAllStringSubmatch(block, -1)
	if len(entries) != len(DefaultConfig()) {
		t.Fatalf("frontend declares %d categories, backend %d: %q", len(entries), len(DefaultConfig()), block)
	}

	frontend := make(map[string]string, len(entries))
	for _, m := range entries {
		frontend[m[1]] = m[2]
	}

	for cat, want := range DefaultConfig() {
		got, ok := frontend[cat]
		if !ok {
			t.Errorf("frontend is missing category %q", cat)
			continue
		}
		if got != want {
			t.Errorf("category %q: frontend default %q, backend default %q", cat, got, want)
		}
	}
}
