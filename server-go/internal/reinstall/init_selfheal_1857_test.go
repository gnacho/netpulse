package reinstall_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/reinstall"
)

// #1857: el init procd solo debe registrar la instancia cuando hay binario.
// Si el self-heal no lo consigue, procd arrancaria una ruta inexistente,
// reintentaria y se rendiria: el servicio queda como arrancado y el agente
// muerto sin mas rastro que una linea en el log.
func initHeredocBody(t *testing.T, script string) string {
	t.Helper()
	const marker = "<<'INITEOF'\n"
	start := strings.Index(script, marker)
	if start < 0 {
		t.Fatalf("el instalador no tiene heredoc INITEOF")
	}
	start += len(marker)
	end := strings.Index(script[start:], "\nINITEOF\n")
	if end < 0 {
		t.Fatalf("el heredoc INITEOF no esta cerrado")
	}
	return script[start : start+end+1]
}

func runInit(t *testing.T, body, bin string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "init.sh")
	script := `logger() { echo "LOG: $*"; }
procd_open_instance() { echo "OPEN $*"; }
procd_set_param() { :; }
procd_close_instance() { echo "CLOSE"; }
crontab() { return 1; }
` + body + `
BIN="` + bin + `"
ENV_FILE="` + filepath.Join(dir, "no-existe") + `"
start_service
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("no pude escribir el init de prueba: %v", err)
	}
	out, err := exec.Command("sh", path).CombinedOutput()
	if err != nil {
		t.Fatalf("el init fallo: %v\n%s", err, out)
	}
	return string(out)
}

func TestInitPassesSyntaxCheck(t *testing.T) {
	body := initHeredocBody(t, reinstall.ScriptWithTargets(
		"test-router", strings.Repeat("a1", 32), "https://192.168.1.226:8443", "sha256//AAAA",
		map[string]string{"arm64": "cafebabe"}, reinstall.WanTargets{},
	))
	if err := exec.Command("sh", "-n", "-c", body).Run(); err != nil {
		t.Fatalf("el init generado no pasa sh -n: %v", err)
	}
}

func TestInitSkipsStartWhenTheBinaryIsMissing(t *testing.T) {
	out := runInit(t, initHeredocBody(t, reinstall.ScriptWithTargets(
		"test-router", strings.Repeat("a1", 32), "https://192.168.1.226:8443", "sha256//AAAA",
		map[string]string{"arm64": "cafebabe"}, reinstall.WanTargets{},
	)), filepath.Join(t.TempDir(), "netpulse-agent"))

	if strings.Contains(out, "OPEN") {
		t.Errorf("registro la instancia sin binario:\n%s", out)
	}
	if !strings.Contains(out, "self-heal: no se pudo restaurar el binario; el servicio no arranca") {
		t.Errorf("no dijo por que no arranca:\n%s", out)
	}
}

func TestInitStartsWhenTheBinaryIsThere(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "netpulse-agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("no pude crear el binario de prueba: %v", err)
	}
	out := runInit(t, initHeredocBody(t, reinstall.ScriptWithTargets(
		"test-router", strings.Repeat("a1", 32), "https://192.168.1.226:8443", "sha256//AAAA",
		map[string]string{"arm64": "cafebabe"}, reinstall.WanTargets{},
	)), bin)

	if !strings.Contains(out, "OPEN netpulse-agent") {
		t.Errorf("no registro la instancia con el binario presente:\n%s", out)
	}
}
