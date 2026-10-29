package rintegration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEscapeEnvValueRoundTrip(t *testing.T) {
	cases := []string{
		"plain",
		"with space",
		`back\slash`,
		"tab\there",
		"line\nbreak",
		`quote"inside`,
		"español con acentos",
	}
	for _, c := range cases {
		if got := UnescapeEnvValue(EscapeEnvValue(c)); got != c {
			t.Fatalf("roundtrip %q -> %q", c, got)
		}
	}
}

func TestWriteReadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.env")
	env := map[string]string{
		"ROUTEROS_PASS":     "p@ss word\nwith\\newline",
		"NETPULSE_TOKEN":    "abc123",
		"NETPULSE_INTERVAL": "30",
	}
	if err := writeEnvFile(path, env); err != nil {
		t.Fatalf("writeEnvFile: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", fi.Mode().Perm())
	}
	got, err := readEnvFile(path)
	if err != nil {
		t.Fatalf("readEnvFile: %v", err)
	}
	for k, want := range env {
		if got[k] != want {
			t.Fatalf("env[%s] = %q, want %q", k, got[k], want)
		}
	}
}
