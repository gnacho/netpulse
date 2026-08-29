// runtime_test.go: tests del paquete runtime - parsing/validación de config
// (espejo de los configError del antiguo main), writePairedToken y emisión
// de Status del bucle contra un servidor de prueba local.
package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoadConfigFromEnvFile(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "agent.env")
	writeFile(t, env, `
# comentario
NETPULSE_SERVER=https://example.com:3000/
NETPULSE_TOKEN=abc123
NETPULSE_SLUG=patio
NETPULSE_SERVER_FP=AA-BB-CC
NETPULSE_INTERVAL=15s
NETPULSE_WAN_TARGET=1.1.1.1
NETPULSE_GW_TARGET=192.168.8.1
NETPULSE_HEARTBEAT_FILE=/tmp/hb
`)
	opts, err := LoadConfigFromEnv(env)
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if opts.Server != "https://example.com:3000" {
		t.Fatalf("Server: %q (quiero sin / final)", opts.Server)
	}
	if opts.Token != "abc123" || opts.Slug != "patio" {
		t.Fatalf("token/slug: %q/%q", opts.Token, opts.Slug)
	}
	if opts.ServerFP == "AA-BB-CC" {
		t.Fatalf("ServerFP sin normalizar: %q", opts.ServerFP)
	}
	if opts.Interval != 15*time.Second {
		t.Fatalf("Interval: %v", opts.Interval)
	}
	if opts.WanTarget != "1.1.1.1" || opts.GwTarget != "192.168.8.1" {
		t.Fatalf("targets: %q/%q", opts.WanTarget, opts.GwTarget)
	}
	if opts.HeartbeatFile != "/tmp/hb" || opts.EnvFile != env {
		t.Fatalf("files: %q/%q", opts.HeartbeatFile, opts.EnvFile)
	}
}

func TestLoadConfigIntervalFormats(t *testing.T) {
	dir := t.TempDir()
	base := "NETPULSE_SERVER=http://s\nNETPULSE_TOKEN=t\nNETPULSE_SLUG=s\n"

	env := filepath.Join(dir, "secs.env")
	writeFile(t, env, base+"NETPULSE_INTERVAL=20")
	opts, err := LoadConfigFromEnv(env)
	if err != nil || opts.Interval != 20*time.Second {
		t.Fatalf("NETPULSE_INTERVAL=20: %v %v", opts.Interval, err)
	}

	env = filepath.Join(dir, "dur.env")
	writeFile(t, env, base+"NETPULSE_INTERVAL=1m")
	opts, err = LoadConfigFromEnv(env)
	if err != nil || opts.Interval != time.Minute {
		t.Fatalf("NETPULSE_INTERVAL=1m: %v %v", opts.Interval, err)
	}

	// Valor inválido: default 30s (y no error).
	env = filepath.Join(dir, "bad.env")
	writeFile(t, env, base+"NETPULSE_INTERVAL=nada")
	opts, err = LoadConfigFromEnv(env)
	if err != nil || opts.Interval != DefaultInterval {
		t.Fatalf("NETPULSE_INTERVAL inválido: %v %v (quiero default 30s)", opts.Interval, err)
	}
}

func TestLoadConfigValidationErrors(t *testing.T) {
	dir := t.TempDir()

	env := filepath.Join(dir, "no-server.env")
	writeFile(t, env, "NETPULSE_TOKEN=t\nNETPULSE_SLUG=s\n")
	if _, err := LoadConfigFromEnv(env); err == nil || !strings.Contains(err.Error(), "NETPULSE_SERVER") {
		t.Fatalf("sin server: quiero ConfigError NETPULSE_SERVER, got %v", err)
	}

	env = filepath.Join(dir, "no-slug.env")
	writeFile(t, env, "NETPULSE_SERVER=http://s\nNETPULSE_TOKEN=t\n")
	if _, err := LoadConfigFromEnv(env); err == nil || !strings.Contains(err.Error(), "NETPULSE_SLUG") {
		t.Fatalf("sin slug: quiero ConfigError NETPULSE_SLUG, got %v", err)
	}

	env = filepath.Join(dir, "no-token.env")
	writeFile(t, env, "NETPULSE_SERVER=http://s\nNETPULSE_SLUG=s\n")
	if _, err := LoadConfigFromEnv(env); err == nil || !strings.Contains(err.Error(), "NETPULSE_TOKEN") {
		t.Fatalf("sin token: quiero ConfigError NETPULSE_TOKEN, got %v", err)
	}

	// Pairing token sustituye a TOKEN.
	env = filepath.Join(dir, "pairing.env")
	writeFile(t, env, "NETPULSE_SERVER=http://s\nNETPULSE_SLUG=s\nNETPULSE_PAIRING_TOKEN=p\n")
	if _, err := LoadConfigFromEnv(env); err != nil {
		t.Fatalf("pairing válido: no quiero error, got %v", err)
	}

	// Options.Validate directo (embedders que no usan LoadConfigFromEnv).
	if err := (Options{Token: "t"}).Validate(); err == nil {
		t.Fatal("Validate sin server/slug: quiero error")
	}
	if err := (Options{Server: "http://s", Slug: "s"}).Validate(); err == nil {
		t.Fatal("Validate sin token ni pairing: quiero error")
	}
	if err := (Options{Server: "http://s", Slug: "s", Token: "t"}).Validate(); err != nil {
		t.Fatalf("Validate completo: %v", err)
	}
}

func TestLoadConfigProcessEnvWins(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "agent.env")
	writeFile(t, env, "NETPULSE_SERVER=http://from-file\nNETPULSE_TOKEN=f\nNETPULSE_SLUG=s\n")

	t.Setenv("NETPULSE_SERVER", "http://from-env")
	opts, err := LoadConfigFromEnv(env)
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if opts.Server != "http://from-env" {
		t.Fatalf("el env del proceso debe ganar: %q", opts.Server)
	}
}

func TestLoadConfigMissingEnvFileDefaults(t *testing.T) {
	t.Setenv("NETPULSE_SERVER", "http://s")
	t.Setenv("NETPULSE_TOKEN", "t")
	t.Setenv("NETPULSE_SLUG", "s")
	opts, err := LoadConfigFromEnv(filepath.Join(t.TempDir(), "no-existe.env"))
	if err != nil {
		t.Fatalf("env file inexistente no es error: %v", err)
	}
	if opts.Interval != DefaultInterval {
		t.Fatalf("Interval default: %v", opts.Interval)
	}
}

func TestWritePairedToken(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "agent.env")
	writeFile(t, env, "NETPULSE_SERVER=http://s\nNETPULSE_PAIRING_TOKEN=pt\nNETPULSE_SLUG=s\n")

	if err := writePairedToken(env, "real-token"); err != nil {
		t.Fatalf("writePairedToken: %v", err)
	}
	data, err := os.ReadFile(env)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "PAIRING") {
		t.Fatalf("PAIRING_TOKEN debe desaparecer: %q", content)
	}
	if !strings.Contains(content, "NETPULSE_TOKEN=real-token") {
		t.Fatalf("TOKEN nuevo debe estar: %q", content)
	}
	if !strings.Contains(content, "NETPULSE_SERVER=http://s") {
		t.Fatalf("conserva el resto: %q", content)
	}
	st, _ := os.Stat(env)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("permisos: %o", st.Mode().Perm())
	}

	// Env file inexistente: se crea solo con el token.
	env2 := filepath.Join(dir, "nuevo.env")
	if err := writePairedToken(env2, "tk"); err != nil {
		t.Fatalf("writePairedToken nuevo: %v", err)
	}
	data2, _ := os.ReadFile(env2)
	if string(data2) != "NETPULSE_TOKEN=tk\n" {
		t.Fatalf("nuevo env: %q", data2)
	}
}

// TestRunEmitsStatus: bucle completo contra un servidor de prueba local; el
// callback OnStatus debe ver Running=true y luego un push confirmado.
func TestRunEmitsStatus(t *testing.T) {
	var mu sync.Mutex
	received := make(chan Status, 16)
	onStatus := func(st Status) {
		mu.Lock()
		defer mu.Unlock()
		received <- st
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ingest/agent":
			w.WriteHeader(http.StatusAccepted)
		case "/api/agents/patio/stream":
			// SSE: cuelga hasta que el test cancele el ctx (el cliente cierra).
			<-r.Context().Done()
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	opts := Options{
		Server:        srv.URL,
		Token:         "tok",
		Slug:          "patio",
		Interval:      200 * time.Millisecond,
		HeartbeatFile: filepath.Join(dir, "hb"),
		Version:       "test",
		OnStatus:      onStatus,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opts) }()

	seenRunning, seenPush := false, false
	deadline := time.After(15 * time.Second)
	for !(seenRunning && seenPush) {
		select {
		case st := <-received:
			if st.Running && st.Slug == "patio" && st.Server == srv.URL {
				seenRunning = true
			}
			if st.PushOk && !st.LastPush.IsZero() && st.LastError == "" {
				seenPush = true
			}
		case <-deadline:
			t.Fatal("no se recibió Running+PushOk via OnStatus")
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run no terminó tras cancelar ctx")
	}

	// Parada: último estado con Running=false.
	var last Status
	for {
		select {
		case last = <-received:
		default:
			goto checked
		}
	}
checked:
	if last.Running {
		t.Fatalf("tras cancelar, Running debe ser false: %+v", last)
	}
}

// TestRunEmitsPushError: servidor de ingest caído → estado PushOk=false con
// LastError.
func TestRunEmitsPushError(t *testing.T) {
	received := make(chan Status, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ingest/agent" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/agents/patio/stream" {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	opts := Options{
		Server:   srv.URL,
		Token:    "tok",
		Slug:     "patio",
		Interval: 200 * time.Millisecond,
		OnStatus: func(st Status) { received <- st },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opts) }()

	deadline := time.After(15 * time.Second)
	for {
		select {
		case st := <-received:
			if !st.Running {
				continue
			}
			if !st.PushOk && st.LastError != "" {
				cancel()
				if err := <-done; err != nil {
					t.Fatalf("Run: %v", err)
				}
				return
			}
		case <-deadline:
			cancel()
			t.Fatal("no se recibió PushOk=false con LastError")
		}
	}
}

// TestRunInvalidOptions: Run sin config válida falla rápido sin arrancar.
func TestRunInvalidOptions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := Run(ctx, Options{}); err == nil {
		t.Fatal("Run sin options: quiero error de validación")
	}
}
