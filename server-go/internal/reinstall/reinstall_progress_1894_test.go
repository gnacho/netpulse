// reinstall_progress_1894_test.go — el install cuenta lo que hace por stdout.
// Sin esas líneas una instalación correcta no dejaba ni un renglón en el
// journal (todo iba por stderr y el pool solo devolvía stdout).
package reinstall_test

import (
	"strings"
	"testing"
)

func TestScriptProgress_1894(t *testing.T) {
	s := scriptForTest()
	for _, want := range []string{
		`say() { echo "netpulse-agent: $*"; }`,
		`say "arquitectura $ARCH ($GOARCH)"`,
		`say "binario descargado y verificado"`,
		`say "agente arrancado"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("el script no lleva %q", want)
		}
	}
}

// El aviso de fallo sigue yendo a stderr: no se mezcla con el progreso.
func TestScriptWarnStillOnStderr_1894(t *testing.T) {
	s := scriptForTest()
	if !strings.Contains(s, `warn() { echo "netpulse-agent: $*" >&2; }`) {
		t.Fatal("warn() debe seguir escribiendo a stderr")
	}
}

// #1894: say() estaba definido en el bloque de config, detrás de la primera
// llamada. El script moría con "say: not found" y el reinstall no llegaba ni a
// instalar el binario.
func TestScriptDefinesSayBeforeUse_1894(t *testing.T) {
	s := scriptForTest()
	def := strings.Index(s, `say() { echo "netpulse-agent: $*"; }`)
	if def < 0 {
		t.Fatal("falta la definición de say()")
	}
	use := strings.Index(s, `say "`)
	if use < 0 {
		t.Fatal("el script no usa say()")
	}
	if def > use {
		t.Fatalf("say() se define en %d y se usa en %d", def, use)
	}
}
