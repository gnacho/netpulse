// script_test.go — contrato de reinstall.Script (#463/#457): instalación
// completa del agente (binario verificado, init self-heal, watchdog, cron).
package reinstall_test

import (
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/reinstall"
)

func scriptForTest() string {
	return reinstall.Script(
		"test-router",
		strings.Repeat("a1", 32), // 64 hex como un token real
		"http://192.168.1.226:3000",
		"",
		map[string]string{
			"arm64":  "cafebabe",
			"arm":    "deadbeef",
			"amd64":  "abcd1234",
			"mipsle": "0123feed",
			"mips":   "4567beef",
		},
	)
}

func TestScriptConfig(t *testing.T) {
	s := scriptForTest()
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

func TestScriptArchAndDigests(t *testing.T) {
	s := scriptForTest()
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
	if strings.Contains(s, "arch=armv7\"") {
		t.Error("el script no debe pedir arch=armv7 (normalizeArch espera arm)")
	}
	// #488: uname -m "mips" no distingue endianness; el script lo detecta
	// con el byte EI_DATA del ELF y mapea a mipsle/mips con su digest.
	for _, want := range []string{
		`1) GOARCH=mipsle; SHA256="0123feed"`,
		`2) GOARCH=mips;  SHA256="4567beef"`,
		`head -c 6 /bin/sh | tail -c 1 | tr '\001\002' '12'`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script sin %q", want)
		}
	}
	if strings.Count(s, "mipsle") < 2 {
		t.Errorf("el self-heal del init también debe resolver mipsle (apariciones: %d)",
			strings.Count(s, "mipsle"))
	}
	if !strings.Contains(s, `GOT=$(sha256sum /tmp/netpulse-agent.new | awk '{print $1}')`) {
		t.Error("sin verificación sha256sum")
	}
	if !strings.Contains(s, "exit 21") {
		t.Error("sin código de salida 21 para sha256 mismatch")
	}
}

func TestScriptSelfHealInit(t *testing.T) {
	s := scriptForTest()
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
	if strings.Count(s, "armv7l|armv7|armhf|arm") != 2 {
		t.Errorf("case de arch incompleto (apariciones: %d, esperadas 2)",
			strings.Count(s, "armv7l|armv7|armhf|arm"))
	}
}

func TestScriptWatchdogAndCron(t *testing.T) {
	s := scriptForTest()
	for _, want := range []string{
		"pidof netpulse-agent",
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
	if !strings.Contains(s, "grep -v netpulse-watchdog") {
		t.Error("el cron no es idempotente")
	}
}

func TestScriptFinishesWithStart(t *testing.T) {
	s := scriptForTest()
	if !strings.HasSuffix(strings.TrimSpace(s), "\"$INIT\" enable\n\"$INIT\" restart") {
		t.Error("el script debe terminar con enable + restart")
	}
}

func TestTokenPushScriptConfig(t *testing.T) {
	s := reinstall.TokenPushScript("test-router", strings.Repeat("c3", 32))
	for _, want := range []string{
		"/etc/netpulse-agent.env",
		"NETPULSE_TOKEN=" + strings.Repeat("c3", 32),
		"sed -n 's/^NETPULSE_SERVER=//p' \"$ENV_FILE\"",
		"sed -n 's/^NETPULSE_SLUG=//p' \"$ENV_FILE\"",
		"chmod 600 \"$ENV_FILE.tmp\"",
		"mv -f \"$ENV_FILE.tmp\" \"$ENV_FILE\"",
		"\"$INIT\" restart",
		"exit 30",
		"exit 31",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("TokenPushScript sin %q", want)
		}
	}
	// No debe descargar binario ni tocar init/watchdog/cron (rotate ligero).
	if strings.Contains(s, "/binary?arch=") {
		t.Error("TokenPushScript no debe descargar el binario")
	}
	for _, notWant := range []string{"watchdog", "crontab", "/etc/init.d/netpulse-agent >", "GOT=$(sha256sum"} {
		if strings.Contains(s, notWant) {
			t.Errorf("TokenPushScript no debe contener %q (rotate ligero)", notWant)
		}
	}
}

func TestScriptEmptyDigestSkipsVerify(t *testing.T) {
	s := reinstall.Script(
		"r", strings.Repeat("b2", 32), "http://s:3000", "",
		map[string]string{"arm64": "", "arm": "", "amd64": ""},
	)
	if !strings.Contains(s, `GOARCH=arm64; SHA256=""`) {
		t.Error("sin digest, SHA256 debe quedar vacío")
	}
	if !strings.Contains(s, `if [ -n "$SHA256" ]; then`) {
		t.Error("la verificación debe estar protegida contra digest vacío")
	}
}

// #851: con server https y FP derivado, el .env generado debe llevar
// NETPULSE_SERVER_FP o el agente entra en bucle fatal al arrancar.
func TestScriptIncludesServerFP(t *testing.T) {
	fp := strings.Repeat("c1", 32)
	s := reinstall.Script(
		"r", strings.Repeat("b2", 32), "https://np.example.org:3443", fp,
		map[string]string{"arm64": "x"},
	)
	for _, want := range []string{
		`SERVER="https://np.example.org:3443"`,
		`SERVER_FP="` + fp + `"`,
		`echo "NETPULSE_SERVER_FP=$SERVER_FP" >> "$ENV_FILE"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script con FP sin %q", want)
		}
	}
}

// #851: http plano o FP no derivable → script sin FP (comportamiento previo).
func TestScriptEmptyFPSkipsFPLine(t *testing.T) {
	for _, u := range []string{"http://192.168.1.226:3000", "https://np.example.org"} {
		s := reinstall.Script("r", "tok", u, "", map[string]string{"arm64": "x"})
		if !strings.Contains(s, `SERVER_FP=""`) {
			t.Errorf("script sin %q para %q", `SERVER_FP=""`, u)
		}
		if !strings.Contains(s, `if [ -n "$SERVER_FP" ]; then`) {
			t.Errorf("script sin guard de FP para %q", u)
		}
		if strings.Contains(s, "NETPULSE_SERVER_FP=c1") {
			t.Errorf("script con FP concreto pese a FP vacío (%q)", u)
		}
	}
}

// #851: el rotate del token debe conservar el FP del env existente; si se
// pierde, el agente cae en el bucle fatal de "HTTPS requiere NETPULSE_SERVER_FP".
func TestTokenPushPreservesServerFP(t *testing.T) {
	s := reinstall.TokenPushScript("test-router", strings.Repeat("c3", 32))
	for _, want := range []string{
		`sed -n 's/^NETPULSE_SERVER_FP=//p' "$ENV_FILE"`,
		`echo "NETPULSE_SERVER_FP=$FP" >> "$ENV_FILE.tmp"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("TokenPushScript sin %q", want)
		}
	}
}
