package rintegration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerEnvRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	env := map[string]string{
		"ROUTEROS_HOST":     "10.0.0.1",
		"ROUTEROS_USER":     "netpulse",
		"ROUTEROS_PASS":     "s3cr3t#with=chars",
		"NETPULSE_INTERVAL": "30s",
		"NETPULSE_TOKEN":    "abc123",
	}
	if err := m.WriteEnv("chr-lab", env); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "chr-lab.env"))
	if err != nil {
		t.Fatalf("stat env file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("env file mode = %o, want 600", perm)
	}

	got, err := m.Env("chr-lab")
	if err != nil {
		t.Fatalf("Env: %v", err)
	}
	for k, want := range env {
		if got[k] != want {
			t.Fatalf("%s = %q, want %q", k, got[k], want)
		}
	}
}

func TestParseEnvHandlesCRLFAndBlankLines(t *testing.T) {
	body := "A=1\r\n\r\nB=two=three\r\nbroken\r\n"
	got := ParseEnv(body)
	if got["A"] != "1" {
		t.Fatalf("A = %q, want 1", got["A"])
	}
	if got["B"] != "two=three" {
		t.Fatalf("B = %q, want two=three", got["B"])
	}
	if _, ok := got["broken"]; ok {
		t.Fatalf("line without = must be skipped")
	}
}
