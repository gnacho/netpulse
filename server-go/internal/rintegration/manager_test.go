package rintegration

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// TestManagerNotifiesCleanExit: un hijo que termina solo (código 0) también
// notifica a onExit con err nil (#1696). El pusher es un daemon de larga
// vida: su consumidor decide que una salida limpia es inesperada.
func TestManagerNotifiesCleanExit(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	type exitEvent struct {
		slug string
		err  error
	}
	ch := make(chan exitEvent, 4)
	m.SetOnExit(func(slug string, err error) { ch <- exitEvent{slug, err} })
	if err := m.WriteEnv("clean", map[string]string{"X": "1"}); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := m.Start("clean", nil, writeScript(t, dir, "clean-pusher", "exit 0")); err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.slug != "clean" || ev.err != nil {
			t.Fatalf("onExit = (%q, %v), want (clean, nil)", ev.slug, ev.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("onExit no se disparó en una salida limpia (código 0)")
	}
	if m.Running("clean") {
		t.Fatal("el hijo muerto sigue reportándose como running")
	}
}

// TestManagerNoOnExitAfterIntentionalStop: un Stop no dispara onExit; el
// estado de un paro intencional lo escribe el consumidor, no el manager.
func TestManagerNoOnExitAfterIntentionalStop(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	fired := make(chan string, 4)
	m.SetOnExit(func(slug string, err error) { fired <- slug })
	if err := m.WriteEnv("sleeper", map[string]string{"X": "1"}); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := m.Start("sleeper", nil, writeScript(t, dir, "sleeper-pusher", "sleep 30")); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := m.Stop("sleeper"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case slug := <-fired:
		t.Fatalf("onExit se disparó en un paro intencional (slug %q)", slug)
	case <-time.After(300 * time.Millisecond):
	}
}
