// upgrade_test.go — Fase 6.3 (issue #243): tests de la lógica de self-update
// del agente (mapeo de arquitectura, descarga+verificación y swap atómico).
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNetpulseArch(t *testing.T) {
	cases := map[string]string{
		"amd64":  "amd64",
		"arm64":  "arm64",
		"arm":    "armv7", // GOARCH arm (GOARM=7) → sufijo de binario embebido
		"mips":   "mips",  // desconocido: se conserva
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
	if err := downloadBinary(t.Context(), hc, srv.URL+"/binary", "tok", dest); err != nil {
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
	if err := downloadBinary(t.Context(), hc, srv.URL+"/binary", "wrong", dest); err == nil {
		t.Fatal("token inválido: quiero error")
	}
	// 404 → error.
	if err := downloadBinary(t.Context(), hc, srv.URL+"/missing", "tok", dest); err == nil {
		t.Fatal("404: quiero error")
	}
	// Cuerpo vacío → error (verificación de no-vacío).
	if err := downloadBinary(t.Context(), hc, srv.URL+"/empty", "tok", dest); err == nil {
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
