package reinstall_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/reinstall"
)

// #1855: despues del swap el binario nuevo ya esta instalado y el servicio
// sigue parado, asi que ningun paso de configuracion puede abortar el script:
// cada uno avisa por stderr y el restart del final se ejecuta igualmente.
func TestScriptSurvivesAFailedConfigStep(t *testing.T) {
	token := strings.Repeat("a1", 32)
	s := reinstall.ScriptWithTargets(
		"test-router", token, "https://192.168.1.226:3000", "sha256//AAAA",
		map[string]string{"arm64": "cafebabe"},
		reinstall.WanTargets{},
	)

	for _, want := range []string{
		`ENV_TMP="$ENV_FILE.new.$$"`,
		`mv -f "$ENV_TMP" "$ENV_FILE" || return 1`,
		`warn "no pude escribir $ENV_FILE; se queda el anterior"`,
		`INIT_TMP="/tmp/netpulse-agent.init.$$"`,
		`warn "no pude instalar $INIT"`,
		`rm -f /usr/sbin/netpulse-watchdog || warn`,
		`"$INIT" enable >/dev/null 2>&1 || warn "no pude habilitar $INIT"`,
		`if ! "$INIT" restart >/dev/null 2>&1; then`,
		`exit 24`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("el script no contiene %q", want)
		}
	}

	swap := strings.Index(s, `mv -f /tmp/netpulse-agent.new "$BIN"`)
	enable := strings.Index(s, `"$INIT" enable`)
	restart := strings.Index(s, `if ! "$INIT" restart`)
	if swap < 0 || enable < 0 || restart < 0 || !(swap < enable && enable < restart) {
		t.Fatalf("orden inesperado: swap=%d enable=%d restart=%d", swap, enable, restart)
	}

	if err := exec.Command("sh", "-n", "-c", s).Run(); err != nil {
		t.Fatalf("script invalido: %v", err)
	}
}
