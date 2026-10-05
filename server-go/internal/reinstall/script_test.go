// script_test.go — contrato de reinstall.Script (#463/#457): instalación
// completa del agente (binario verificado, init self-heal, watchdog, cron).
package reinstall_test

import (
	"os/exec"
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

// #851: el env generado documenta las opciones disponibles como líneas
// comentadas con sus defaults.
func TestScriptEnvDocumentsDefaults(t *testing.T) {
	s := scriptForTest()
	for _, want := range []string{
		"# NETPULSE_INTERVAL=30",
		"# NETPULSE_SCAN_INTERVAL=30m",
		"# NETPULSE_WAN_TARGET=1.1.1.1",
		"# NETPULSE_GW_TARGET=192.168.8.1",
		"# NETPULSE_HEARTBEAT_FILE=/tmp/netpulse-agent.heartbeat",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("env generado sin %q", want)
		}
	}
}

// #851: el rewrite del env conserva las NETPULSE_* del usuario (p. ej.
// NETPULSE_SCAN_INTERVAL=0) en vez de pisarlas.
// TestScriptWritesActiveTargets (#1204): con objetivos resueltos por el
// server, el env lleva las líneas ACTIVAS (no comentadas); sin objetivos,
// siguen documentadas como antes.
func TestScriptWritesActiveTargets(t *testing.T) {
	slug, token, url, fp := "test-router", strings.Repeat("a1", 32), "http://192.168.1.226:3000", ""
	digests := map[string]string{"arm64": "cafebabe"}
	withWan := reinstall.ScriptWithTargets(slug, token, url, fp, digests, reinstall.WanTargets{WanTarget: "1.1.1.1"})
	if !strings.Contains(withWan, "\nNETPULSE_WAN_TARGET=1.1.1.1\n") {
		t.Errorf("gateway sin línea WAN activa")
	}
	if strings.Contains(withWan, "\nNETPULSE_GW_TARGET=") {
		t.Errorf("el gateway no debe llevar GW_TARGET activo")
	}
	withGw := reinstall.ScriptWithTargets(slug, token, url, fp, digests, reinstall.WanTargets{GwTarget: "192.168.1.1"})
	if !strings.Contains(withGw, "\nNETPULSE_GW_TARGET=192.168.1.1\n") {
		t.Errorf("AP sin línea GW activa")
	}
	if strings.Contains(withGw, "\nNETPULSE_WAN_TARGET=") {
		t.Errorf("el AP no debe llevar WAN_TARGET activo")
	}
	plain := scriptForTest()
	if !strings.Contains(plain, "# NETPULSE_WAN_TARGET=1.1.1.1") || !strings.Contains(plain, "# NETPULSE_GW_TARGET=192.168.8.1") {
		t.Errorf("sin objetivos las líneas deben seguir comentadas")
	}
}

func TestScriptPreservesUserVars(t *testing.T) {
	s := scriptForTest()
	for _, want := range []string{
		`USER_VARS=$(grep -E '^NETPULSE_[A-Z0-9_]+=' "$ENV_FILE"`,
		`grep -vE '^NETPULSE_(SERVER|SLUG|TOKEN|SERVER_FP|PAIRING_TOKEN|WAN_TARGET|GW_TARGET)='`,
		`printf '%s\n' "$USER_VARS" >> "$ENV_FILE"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script sin preservación de vars de usuario: falta %q", want)
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

// #879: el init entregado por el reinstall auto-repara la entrada de cron del
// antiguo watchdog (tras sysupgrade /etc sobrevive pero el binario no, y cron
// loguearía un comando inexistente cada 2 min).
func TestScriptInitSelfHealsStaleWatchdogCron(t *testing.T) {
	s := scriptForTest()
	for _, want := range []string{
		"cleanup_stale_watchdog_cron()",
		"[ -f /usr/sbin/netpulse-watchdog ] && return 0",
		"crontab -l 2>/dev/null | grep -q netpulse-watchdog || return 0",
		"cleanup_stale_watchdog_cron",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("init sin self-heal #879: falta %q", want)
		}
	}
}

// #851: el watchdog cron YA NO se instala; el script limpia los restos de
// instalaciones previas que sí lo tenían.
func TestScriptNoWatchdogCron(t *testing.T) {
	s := scriptForTest()
	for _, notWant := range []string{
		"*/2 * * * *",
		"proceso vivo pero sin latido",
		`echo '*/2`,
		"/etc/init.d/cron restart",
	} {
		if strings.Contains(s, notWant) {
			t.Errorf("script con watchdog/cron (%q) pese a #851", notWant)
		}
	}
	for _, want := range []string{
		`rm -f /usr/sbin/netpulse-watchdog`,
		`sed -i '/netpulse-watchdog/d' /etc/crontabs/root`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script sin limpieza legacy del watchdog: falta %q", want)
		}
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
		// #851: preservar FP y NETPULSE_* del usuario (p. ej. SCAN_INTERVAL).
		"sed -n 's/^NETPULSE_SERVER_FP=//p' \"$ENV_FILE\"",
		`printf '%s\n' "$USER_VARS" >> "$ENV_FILE.tmp"`,
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
	// No debe descargar binario ni tocar el init (rotate ligero).
	if strings.Contains(s, "/binary?arch=") {
		t.Error("TokenPushScript no debe descargar el binario")
	}
	for _, notWant := range []string{"*/2 * * * *", "GOT=$(sha256sum"} {
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

// FORK: with a Trust the script moves the agent to the HTTPS address, writes
// the root to the router, verifies its downloads - including the self-heal's
// - against it, and pins the server. Without one nothing of that appears.
func TestScriptWithTrust(t *testing.T) {
	root := "-----BEGIN CERTIFICATE-----\nMIIBtestroot\n-----END CERTIFICATE-----\n"
	fp := strings.Repeat("ab", 32)
	// A pin derived for the old address is replaced by the trust's.
	s := reinstall.Script("test-router", strings.Repeat("a1", 32), "http://192.0.2.10:3000", strings.Repeat("99", 32),
		map[string]string{}, reinstall.Trust{ServerURL: "https://192.0.2.10:3443", ServerFP: fp, CAPEM: []byte(root)})
	for _, want := range []string{
		`SERVER="https://192.0.2.10:3443"`,
		"cat > /etc/netpulse-ca.pem <<'CAEOF'\n" + strings.TrimSpace(root) + "\nCAEOF",
		`curl -fsSL $CURL_TLS`,
		`wget -q $WGET_TLS`,
		`SERVER_FP="` + fp + `"`,
		`ctls="--cacert /etc/netpulse-ca.pem"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	if out, err := exec.Command("sh", "-n", "-c", s).CombinedOutput(); err != nil {
		t.Fatalf("not valid sh: %v\n%s", err, out)
	}

	plain := reinstall.Script("test-router", strings.Repeat("a1", 32), "http://192.0.2.10:3000", "", map[string]string{})
	if !strings.Contains(plain, "rm -f /etc/netpulse-ca.pem") {
		t.Error("a script without a root leaves a stale one on the router")
	}
	if strings.Contains(s, "rm -f /etc/netpulse-ca.pem") {
		t.Error("a script with a root removes it")
	}
	if strings.Contains(s, strings.Repeat("99", 32)) {
		t.Error("the pin passed in survived the trust's")
	}
	if !strings.Contains(plain, `SERVER_FP=""`) {
		t.Error("a script without a pin sets one")
	}
	for _, notWant := range []string{"CAEOF", "https://"} {
		if strings.Contains(plain, notWant) {
			t.Errorf("a script without Trust contains %q", notWant)
		}
	}
	if out, err := exec.Command("sh", "-n", "-c", plain).CombinedOutput(); err != nil {
		t.Fatalf("not valid sh: %v\n%s", err, out)
	}
}
