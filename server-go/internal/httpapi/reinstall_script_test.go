// reinstall_script_test.go — contrato de reinstallScript (#463): instalación
// completa del agente (binario verificado, init self-heal, watchdog, cron).
package httpapi

import (
	"strings"
	"testing"
)

func reinstallScriptForTest(t *testing.T) string {
	t.Helper()
	return reinstallScript(
		"test-router",
		strings.Repeat("a1", 32), // 64 hex como un token real
		"http://192.168.1.226:3000",
		map[string]string{
			"arm64": "cafebabe",
			"arm":   "deadbeef",
			"amd64": "abcd1234",
		},
	)
}

func TestReinstallScriptConfig(t *testing.T) {
	s := reinstallScriptForTest(t)
	for _, want := range []string{
		`SERVER="http://192.168.1.226:3000"`,
		`SLUG="test-router"`,
		`NETPULSE_SERVER=$SERVER`,
		`NETPULSE_SLUG=$SLUG`,
		`NETPULSE_TOKEN=$TOKEN`,
		"chmod 600 \"$ENV_FILE\"",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script sin %q", want)
		}
	}
}

func TestReinstallScriptArchAndDigests(t *testing.T) {
	s := reinstallScriptForTest(t)
	// Mapeo uname → goarch con alias arm completos.
	for _, want := range []string{
		"aarch64|arm64)  GOARCH=arm64; SHA256=\"cafebabe\"",
		"armv7l|armv7|armhf|arm) GOARCH=arm; SHA256=\"deadbeef\"",
		"x86_64|amd64)   GOARCH=amd64; SHA256=\"abcd1234\"",
		`/api/agents/$SLUG/binary?arch=$GOARCH`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script sin %q", want)
		}
	}
	// El armv7 legacy (que el endpoint normaliza a arm) ya no se pide.
	if strings.Contains(s, "arch=armv7\"") {
		t.Error("el script no debe pedir arch=armv7 (normalizeArch espera arm)")
	}
	// Verificación sha256 presente y aborta con exit 21 si no coincide.
	if !strings.Contains(s, `GOT=$(sha256sum /tmp/netpulse-agent.new | awk '{print $1}')`) {
		t.Error("sin verificación sha256sum")
	}
	if !strings.Contains(s, "exit 21") {
		t.Error("sin código de salida 21 para sha256 mismatch")
	}
}

func TestReinstallScriptSelfHealInit(t *testing.T) {
	s := reinstallScriptForTest(t)
	for _, want := range []string{
		"selfheal_binary()",
		`url="${NETPULSE_SERVER%/}/api/agents/${NETPULSE_SLUG}/binary?arch=${ARCH}"`,
		"logger -t netpulse-agent \"self-heal: binario restaurado\"",
		"selfheal_binary || logger -t netpulse-agent \"self-heal: no se pudo restaurar el binario\"",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("init sin self-heal: falta %q", want)
		}
	}
	// El self-heal debe conocer los tres archs (mismo case que el exterior).
	if strings.Count(s, "armv7l|armv7|armhf|arm") != 2 {
		t.Errorf("case de arch incompleto en el script (apariciones: %d, esperadas 2)",
			strings.Count(s, "armv7l|armv7|armhf|arm"))
	}
}

func TestReinstallScriptWatchdogAndCron(t *testing.T) {
	s := reinstallScriptForTest(t)
	for _, want := range []string{
		"pidof netpulse-agent", // pidof, nunca pgrep -x (BusyBox)
		`echo '*/2 * * * * /usr/sbin/netpulse-watchdog'`,
		"/etc/init.d/cron restart",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("watchdog/cron incompleto: falta %q", want)
		}
	}
	if strings.Contains(s, "pgrep -x") {
		t.Error("el watchdog no debe usar pgrep -x (bug BusyBox)")
	}
	// Idempotencia del cron: reemplaza la línea previa en vez de duplicarla.
	if !strings.Contains(s, "grep -v netpulse-watchdog") {
		t.Error("el cron no es idempotente")
	}
}

func TestReinstallScriptFinishesWithStart(t *testing.T) {
	s := reinstallScriptForTest(t)
	if !strings.HasSuffix(strings.TrimSpace(s), "\"$INIT\" enable\n\"$INIT\" restart") {
		t.Error("el script debe terminar con enable + restart")
	}
}

func TestReinstallScriptEmptyDigestSkipsVerify(t *testing.T) {
	s := reinstallScript(
		"r", strings.Repeat("b2", 32), "http://s:3000",
		map[string]string{"arm64": "", "arm": "", "amd64": ""},
	)
	// Sin digests: el case asigna SHA256 vacío y el guard salta la verificación.
	if !strings.Contains(s, `GOARCH=arm64; SHA256=""`) {
		t.Error("sin digest, SHA256 debe quedar vacío")
	}
	if !strings.Contains(s, `if [ -n "$SHA256" ]; then`) {
		t.Error("la verificación debe estar protegida contra digest vacío")
	}
}
