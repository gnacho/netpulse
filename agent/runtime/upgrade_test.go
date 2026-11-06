// upgrade_test.go - Fase 6.3 (issue #243): tests de la lógica de self-update
// del agente (mapeo de arquitectura, descarga+verificación y swap atómico).
// #284: progreso de descarga y pasos reportados al servidor.
package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
			sum := sha256.Sum256(data)
			w.Header().Set("Content-Length", "64")
			w.Header().Set("X-Checksum-Sha256", hex.EncodeToString(sum[:]))
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

	cfg := Options{Server: srv.URL, Slug: "patio", Token: "tok"}
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

func TestUpgradeRejectsMissingOrMismatchedDigest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		digest string
	}{
		{"sin digest", ""},
		{"digest que no coincide", strings.Repeat("0", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var steps []string
			var result map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/agents/patio/upgrade-progress":
					var body struct {
						Step string `json:"step"`
					}
					_ = json.NewDecoder(r.Body).Decode(&body)
					mu.Lock()
					steps = append(steps, body.Step)
					mu.Unlock()
					w.WriteHeader(http.StatusOK)
				case "/api/agents/patio/binary":
					data := make([]byte, 64)
					if tc.digest != "" {
						w.Header().Set("X-Checksum-Sha256", tc.digest)
					}
					_, _ = w.Write(data)
				case "/api/agents/patio/upgrade-result":
					mu.Lock()
					result = nil
					_ = json.NewDecoder(r.Body).Decode(&result)
					mu.Unlock()
					w.WriteHeader(http.StatusOK)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			oldTmp := tmpBinPath
			tmpBinPath = filepath.Join(t.TempDir(), "netpulse-agent.new")
			defer func() { tmpBinPath = oldTmp }()
			oldPath := currentBinPath
			currentBinPath = func() string { return filepath.Join(t.TempDir(), "netpulse-agent") }
			defer func() { currentBinPath = oldPath }()

			handleUpgrade(Options{Server: srv.URL, Slug: "patio", Token: "tok"}, nil, "")

			mu.Lock()
			defer mu.Unlock()
			for _, step := range steps {
				if step == "swapping" || step == "restarting" {
					t.Fatalf("no debía instalar nada, pasos: %v", steps)
				}
			}
			if result == nil {
				t.Fatal("no se reportó el resultado del upgrade")
			}
			if ok, _ := result["ok"].(bool); ok {
				t.Fatalf("el upgrade debía fallar, resultado: %v", result)
			}
		})
	}
}

// stubNoInitScript apunta el script procd a una ruta que no existe y devuelve
// la restauración, para ejercitar la vía sin supervisor (#1900).
func stubNoInitScript(t *testing.T) {
	t.Helper()
	old := agentInitScript
	agentInitScript = filepath.Join(t.TempDir(), "sin-init")
	t.Cleanup(func() { agentInitScript = old })
}

// stubExecSelf sustituye el exec del proceso y registra con qué se llamó.
func stubExecSelf(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	old := execSelf
	execSelf = func(bin string, argv []string, env []string) error {
		calls = append(calls, bin)
		return err
	}
	t.Cleanup(func() { execSelf = old })
	return &calls
}

// #1900 pata 1: sin script procd el agente se reemplaza a sí mismo con el
// binario que acaba de instalar. Antes devolvía error, el proceso seguía con
// el binario viejo en memoria y el equipo quedaba mintiendo hasta el reboot.
func TestRestartServiceExecsTheNewBinaryWithoutProcd(t *testing.T) {
	stubNoInitScript(t)
	calls := stubExecSelf(t, nil)
	bin := filepath.Join(t.TempDir(), "netpulse-agent")
	if err := restartService(bin); err != nil {
		t.Fatalf("restartService: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != bin {
		t.Fatalf("exec con %v, quiero exactamente %q", *calls, bin)
	}
}

// Con script procd se le pide el reinicio y no se toca el proceso: el
// supervisor es quien lo relanza.
func TestRestartServiceUsesTheProcdScriptWhenItExists(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "reiniciado")
	script := filepath.Join(dir, "netpulse-agent")
	body := "#!/bin/sh\ntouch " + marker + "\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile script: %v", err)
	}
	old := agentInitScript
	agentInitScript = script
	defer func() { agentInitScript = old }()
	calls := stubExecSelf(t, nil)

	if err := restartService("/tmp/otro"); err != nil {
		t.Fatalf("restartService: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("con script procd no se debe hacer exec: %v", *calls)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("el script no llegó a ejecutarse: %v", err)
	}
}

// Si el exec también falla, el error dice por dónde se intentó.
func TestRestartServiceReportsExecFailure(t *testing.T) {
	stubNoInitScript(t)
	stubExecSelf(t, os.ErrPermission)
	err := restartService("/tmp/netpulse-agent")
	if err == nil || !strings.Contains(err.Error(), "/tmp/netpulse-agent") {
		t.Fatalf("error = %v, quiero uno que nombre el binario", err)
	}
}

// #1900 de punta a punta: el agente reporta ok antes de reiniciar y, si el
// reinicio no arranca, reporta el fallo con el motivo para que el servidor no
// se quede en "restarting" y la UI no diga que está al día.
func TestUpgradeReportsRestartFailure(t *testing.T) {
	var mu sync.Mutex
	var results []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agents/patio/binary":
			data := make([]byte, 64)
			sum := sha256.Sum256(data)
			w.Header().Set("Content-Length", "64")
			w.Header().Set("X-Checksum-Sha256", hex.EncodeToString(sum[:]))
			_, _ = w.Write(data)
		case "/api/agents/patio/upgrade-result":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			results = append(results, body)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	oldTmp := tmpBinPath
	tmpBinPath = filepath.Join(dir, "agent.new")
	defer func() { tmpBinPath = oldTmp }()
	exe := filepath.Join(dir, "netpulse-agent")
	if err := os.WriteFile(exe, []byte("viejo"), 0o755); err != nil {
		t.Fatalf("WriteFile exe: %v", err)
	}
	oldPath := currentBinPath
	currentBinPath = func() string { return exe }
	defer func() { currentBinPath = oldPath }()
	stubNoInitScript(t)
	stubExecSelf(t, os.ErrPermission)

	handleUpgrade(Options{Server: srv.URL, Slug: "patio", Token: "tok"}, nil, "")

	mu.Lock()
	defer mu.Unlock()
	if len(results) != 2 {
		t.Fatalf("resultados = %v, quiero ok y then fallo", results)
	}
	if ok, _ := results[0]["ok"].(bool); !ok {
		t.Fatalf("primer resultado: %v, quiero ok=true antes del reinicio", results[0])
	}
	if ok, _ := results[1]["ok"].(bool); ok {
		t.Fatalf("segundo resultado: %v, quiero ok=false", results[1])
	}
	if msg, _ := results[1]["error"].(string); !strings.Contains(msg, "reinicio falló") {
		t.Fatalf("error reportado = %q", msg)
	}
}
