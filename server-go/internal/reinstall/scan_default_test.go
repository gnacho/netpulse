package reinstall_test

// scan_default_test.go - #1558: the env the reinstall writes must default
// NETPULSE_SCAN_INTERVAL to 0 and still keep a value the unit already had.
// These run the generated block with sh instead of matching strings: the
// value depends on what the remote env file holds.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// envBlockForTest extracts the env rewrite block from the reinstall script,
// the one shared with EnvScript.
func envBlockForTest(t *testing.T) string {
	t.Helper()
	s := scriptForTest()
	start := strings.Index(s, `USER_VARS=""`)
	if start < 0 {
		t.Fatal("no encuentro el bloque de env en el script")
	}
	end := strings.Index(s[start:], `chmod 600 "$ENV_FILE"`)
	if end < 0 {
		t.Fatal("el bloque de env no termina con chmod")
	}
	return s[start : start+end+len(`chmod 600 "$ENV_FILE"`)]
}

func runEnvBlock(t *testing.T, prev string) string {
	t.Helper()
	dir := t.TempDir()
	envPath := filepath.Join(dir, "netpulse-agent.env")
	if prev != "" {
		if err := os.WriteFile(envPath, []byte(prev), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := "ENV_FILE=" + envPath + "\n" +
		"SERVER=http://192.168.1.226:3000\n" +
		"SLUG=test-router\n" +
		"TOKEN=" + strings.Repeat("a1", 32) + "\n" +
		"SERVER_FP=\n" +
		envBlockForTest(t) + "\n"
	if out, err := exec.Command("sh", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("el bloque de env falla: %v\n%s", err, out)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestEnvDefaultsScanOff: sin SCAN_INTERVAL previo, el env sale con 0 activo.
func TestEnvDefaultsScanOff(t *testing.T) {
	env := runEnvBlock(t, "")
	if !strings.Contains(env, "\nNETPULSE_SCAN_INTERVAL=0\n") {
		t.Fatalf("falta el default 0 activo:\n%s", env)
	}
	// Comentado ya no sirve: el agente no lo leería.
	if strings.Contains(env, "# NETPULSE_SCAN_INTERVAL=30m") {
		t.Fatalf("el default viejo sigue comentado:\n%s", env)
	}
}

// TestEnvKeepsScanOverride: un valor manual sobrevive al reinstall y no se
// duplica (#851).
func TestEnvKeepsScanOverride(t *testing.T) {
	env := runEnvBlock(t, "NETPULSE_SCAN_INTERVAL=45m\nNETPULSE_HEARTBEAT_FILE=/tmp/hb\n")
	if !strings.Contains(env, "\nNETPULSE_SCAN_INTERVAL=45m\n") {
		t.Fatalf("override perdido:\n%s", env)
	}
	if got := strings.Count(env, "NETPULSE_SCAN_INTERVAL="); got != 1 {
		t.Fatalf("SCAN_INTERVAL duplicado (%d):\n%s", got, env)
	}
	if !strings.Contains(env, "\nNETPULSE_HEARTBEAT_FILE=/tmp/hb\n") {
		t.Fatalf("var de usuario perdida:\n%s", env)
	}
}
