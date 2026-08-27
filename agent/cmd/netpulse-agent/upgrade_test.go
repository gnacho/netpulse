// upgrade_test.go — Fase 6.3 (issue #243): tests de la lógica de self-update
// del agente (mapeo de arquitectura, descarga+verificación y swap atómico).
// #284: progreso de descarga y pasos reportados al servidor.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestNetpulseArch(t *testing.T) {
	cases := map[string]string{
		"amd64": "amd64",
		"arm64": "arm64",
		"arm":   "armv7", // GOARCH arm (GOARM=7) → sufijo de binario embebido
		"mips":  "mips",  // desconocido: se conserva
	}
	for in, want := range cases {
		if got := netpulseArch(in); got != want {
			t.Fatalf("netpulseArch(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeBinaryServer sirve un binario de prueba y ejercita auth/status/empty.
func fakeBinaryServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
			return
		case "/empty":
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("\x7fELF-fake-binary-content"))
	}))
}

func TestDownloadBinaryOK(t *testing.T) {
	srv := fakeBinaryServer(t, "tok")
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "agent.new")
	hc := &http.Client{}
	if _, err := downloadBinary(t.Context(), hc, srv.URL+"/binary", "tok", dest, nil); err != nil {
		t.Fatalf("downloadBinary: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "\x7fELF-fake-binary-content" {
		t.Fatalf("contenido: %q", data)
	}
	st, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("permisos: %o, want 755", st.Mode().Perm())
	}
}

func TestDownloadBinaryAuth404Empty(t *testing.T) {
	srv := fakeBinaryServer(t, "tok")
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "agent.new")
	hc := &http.Client{}

	// Token inválido → 401 → error.
	if _, err := downloadBinary(t.Context(), hc, srv.URL+"/binary", "wrong", dest, nil); err == nil {
		t.Fatal("token inválido: quiero error")
	}
	// 404 → error.
	if _, err := downloadBinary(t.Context(), hc, srv.URL+"/missing", "tok", dest, nil); err == nil {
		t.Fatal("404: quiero error")
	}
	// Cuerpo vacío → error (verificación de no-vacío).
	if _, err := downloadBinary(t.Context(), hc, srv.URL+"/empty", "tok", dest, nil); err == nil {
		t.Fatal("body vacío: quiero error")
	}
}

func TestSwapBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	if err := os.WriteFile(src, []byte("nuevo-binario"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := swapBinary(src, dst); err != nil {
		t.Fatalf("swapBinary: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if string(data) != "nuevo-binario" {
		t.Fatalf("contenido dst: %q", data)
	}
	st, _ := os.Stat(dst)
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("permisos dst: %o, want 755", st.Mode().Perm())
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("src debe desaparecer tras el swap")
	}
}

// TestDownloadBinaryProgress: el callback de progreso dispara al cruzar cada
// frontera del 10% del total (100% incluido) y termina en el 100 (#284).
// El payload es 10x el buffer de io.Copy (32 KiB) para que cada lectura cruce
// exactamente una frontera; con payloads pequeños una sola Read puede saltar
// varias fronteras de golpe (comportamiento aceptable en producción).
func TestDownloadBinaryProgress(t *testing.T) {
	payload := make([]byte, 320_000)
	for i := range payload {
		payload[i] = byte('A' + i%26)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "320000")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	var got []int
	dest := filepath.Join(t.TempDir(), "agent.new")
	if _, err := downloadBinary(t.Context(), &http.Client{}, srv.URL+"/binary", "tok", dest, func(pct int) {
		got = append(got, pct)
	}); err != nil {
		t.Fatalf("downloadBinary: %v", err)
	}
	want := []int{5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70, 75, 80, 85, 90, 95, 100}
	if len(got) != len(want) {
		t.Fatalf("progreso: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("progreso[%d]: got %d, want %d (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestReportUpgradeProgressSteps: handleUpgrade informa los pasos
// downloading/swapping/restarting al servidor antes del resultado final.
// El reinicio real no ocurre (no hay init script en el entorno de test),
// así que el flujo para en restartService y el test observa los POSTs.
func TestReportUpgradeProgressSteps(t *testing.T) {
	var mu sync.Mutex
	var steps []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agents/patio/upgrade-progress":
			var body struct {
				Step string `json:"step"`
				Pct  int    `json:"pct"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			steps = append(steps, body.Step)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case "/api/agents/patio/binary":
			data := make([]byte, 64)
			w.Header().Set("Content-Length", "64")
			_, _ = w.Write(data)
		case "/api/agents/patio/upgrade-result":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Binario "en marcha" en un temp dir: el swap cae al fallback de copia
	// (distinto filesystem) y funciona sin permisos de root.
	exe := filepath.Join(t.TempDir(), "netpulse-agent")
	if err := os.WriteFile(exe, []byte("viejo"), 0o755); err != nil {
		t.Fatalf("WriteFile exe: %v", err)
	}
	oldPath := currentBinPath
	currentBinPath = func() string { return exe }
	defer func() { currentBinPath = oldPath }()

	cfg := config{server: srv.URL, slug: "patio", token: "tok"}
	handleUpgrade(cfg, nil, "")

	mu.Lock()
	defer mu.Unlock()
	if len(steps) < 3 {
		t.Fatalf("pasos reportados: %v, quiero al menos downloading+swapping+restarting", steps)
	}
	if steps[0] != "downloading" {
		t.Fatalf("primer paso: %q, want downloading", steps[0])
	}
	hasSwapping, hasRestarting := false, false
	for _, s := range steps {
		if s == "swapping" {
			hasSwapping = true
		}
		if s == "restarting" {
			hasRestarting = true
		}
	}
	if !hasSwapping || !hasRestarting {
		t.Fatalf("pasos incompletos: %v", steps)
	}
}
