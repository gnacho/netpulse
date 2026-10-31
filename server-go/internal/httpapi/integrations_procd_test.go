package httpapi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// procdInitBody extrae el cuerpo del init procd generado del heredoc INITEOF
// del instalador, quitando la linea del shebang de rc.common (el interprete
// solo existe en el router).
func procdInitBody(t *testing.T, script string) string {
	t.Helper()
	const marker = "<<'INITEOF'\n"
	start := strings.Index(script, marker)
	if start < 0 {
		t.Fatalf("install script has no INITEOF heredoc\n%s", script)
	}
	start += len(marker)
	end := strings.Index(script[start:], "\nINITEOF\n")
	if end < 0 {
		t.Fatalf("INITEOF heredoc is not terminated\n%s", script)
	}
	body := script[start : start+end]
	lines := strings.SplitN(body, "\n", 2)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "#!") {
		t.Fatalf("init body does not open with a shebang\n%s", body)
	}
	return lines[1]
}

// runProcdStartService ejecuta el cuerpo del init fuera del router: los
// helpers de procd van mockeados y cada argumento de cada llamada
// procd_set_param env se captura en capturePath (una linea por argumento),
// seguido de un resumen ENVCALLS=n RC=n.
func runProcdStartService(t *testing.T, body, envFile, capturePath string) (string, error) {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "fake-pusher")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	body = strings.Replace(body, `BIN="/usr/sbin/test-pusher"`, `BIN="`+binPath+`"`, 1)
	body = strings.Replace(body, `ENV_FILE="/etc/test-pusher.env"`, `ENV_FILE="`+envFile+`"`, 1)
	if strings.Contains(body, "/etc/test-pusher.env") || strings.Contains(body, "/usr/sbin/test-pusher") {
		t.Fatalf("init body still references on-router paths\n%s", body)
	}

	harness := `ENVCALLS=0
CAPTURE="` + capturePath + `"
procd_open_instance() { :; }
procd_close_instance() { :; }
procd_set_param() {
	[ "$1" = "env" ] || return 0
	ENVCALLS=$((ENVCALLS+1))
	shift
	while [ "$#" -gt 0 ]; do
		printf '%s\n' "$1" >> "$CAPTURE"
		shift
	done
}
` + body + `
start_service
rc=$?
printf 'ENVCALLS=%s RC=%s\n' "$ENVCALLS" "$rc" >> "$CAPTURE"
exit "$rc"
`
	cmd := exec.Command("/bin/sh", "-c", harness)
	combined, err := cmd.CombinedOutput()
	return string(combined), err
}

func TestSSHProcdInitSingleEnvCall(t *testing.T) {
	v := procdVendor()
	env := merlinTestEnv()
	script := sshInstallScript(v, env, "https://releases.example/a.tar.gz", "abc123")

	if got := strings.Count(script, "procd_set_param env"); got != 1 {
		t.Fatalf("procd install script has %d procd_set_param env calls, want exactly 1\n%s", got, script)
	}
	for _, want := range []string{
		`procd_set_param env "$@"`,
		`procd_set_param file "$ENV_FILE"`,
		`[ -r "$ENV_FILE" ]`,
		"missing or unreadable env file",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("procd install script missing %q\n%s", want, script)
		}
	}
	envIdx := strings.Index(script, `procd_set_param env "$@"`)
	cmdIdx := strings.Index(script, `procd_set_param command "$BIN"`)
	if envIdx < 0 || cmdIdx < 0 || envIdx > cmdIdx {
		t.Fatalf("env params must be set before the command param (env %d, command %d)\n%s", envIdx, cmdIdx, script)
	}
	// Todas las claves del entorno esperado deben aparecer en el fichero de
	// entorno que el instalador escribe en el router.
	for k := range env {
		if !strings.Contains(script, k+"=") {
			t.Fatalf("procd install script does not write key %q\n%s", k, script)
		}
	}
}

func TestSSHProcdInitLoadsAllEnvKeysInOneCall(t *testing.T) {
	v := procdVendor()
	env := map[string]string{
		"ASUSWRT_HOST":      "127.0.0.1",
		"ASUSWRT_PASS":      "line1\nline2",
		"ASUSWRT_USER":      "admin",
		"NETPULSE_INTERVAL": "45s",
		"NETPULSE_SERVER":   "https://np.example",
		"NETPULSE_TOKEN":    "to ken",
	}
	script := sshInstallScript(v, env, "https://releases.example/a.tar.gz", "abc123")

	dir := t.TempDir()
	envFile := filepath.Join(dir, "test-pusher.env")
	if err := os.WriteFile(envFile, []byte(envFileBody(env)), 0o600); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(dir, "capture.txt")
	out, err := runProcdStartService(t, procdInitBody(t, script), envFile, capture)
	if err != nil {
		t.Fatalf("start_service failed: %v\n%s", err, out)
	}
	data, err2 := os.ReadFile(capture)
	if err2 != nil {
		t.Fatal(err2)
	}
	// Un env de 6 claves llega como 6 argumentos de UNA llamada, en el orden
	// del fichero: el valor con espacio no se parte en palabras y el valor
	// con salto de linea escapado (\n) llega desescapado, igual que en el
	// control script de Merlin y en ParseEnv.
	want := strings.Join([]string{
		"ASUSWRT_HOST=127.0.0.1",
		"ASUSWRT_PASS=line1\nline2",
		"ASUSWRT_USER=admin",
		"NETPULSE_INTERVAL=45s",
		"NETPULSE_SERVER=https://np.example",
		"NETPULSE_TOKEN=to ken",
		"ENVCALLS=1 RC=0",
	}, "\n") + "\n"
	if string(data) != want {
		t.Fatalf("captured env args mismatch\n got: %q\nwant: %q", string(data), want)
	}
}

func TestSSHProcdInitFailsWithoutEnvFile(t *testing.T) {
	v := procdVendor()
	script := sshInstallScript(v, merlinTestEnv(), "https://releases.example/a.tar.gz", "abc123")

	dir := t.TempDir()
	envFile := filepath.Join(dir, "missing.env")
	capture := filepath.Join(dir, "capture.txt")
	out, err := runProcdStartService(t, procdInitBody(t, script), envFile, capture)
	if err == nil {
		t.Fatalf("start_service must fail without the env file\n%s", out)
	}
	if !strings.Contains(out, "missing or unreadable env file") {
		t.Fatalf("start_service must report the missing env file, got:\n%s", out)
	}
	data, err2 := os.ReadFile(capture)
	if err2 != nil {
		t.Fatal(err2)
	}
	if !strings.Contains(string(data), "RC=1") || !strings.Contains(string(data), "ENVCALLS=0") {
		t.Fatalf("start_service must fail before any env call, got: %q", string(data))
	}
}
