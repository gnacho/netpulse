package reinstall_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/reinstall"
)

// #1853: un reinstall que falla no debe dejar el agente parado. El servicio se
// para solo cuando el binario ya esta descargado y verificado, y si el
// reemplazo falla se vuelve a arrancar el anterior.
func TestScriptStopsOnlyOnceItCanInstall(t *testing.T) {
	s := reinstall.ScriptWithTargets(
		"test-router", strings.Repeat("a1", 32), "https://192.168.1.226:3000",
		"sha256//AAAA", map[string]string{"arm64": "cafebabe"},
		reinstall.WanTargets{},
	)

	download := strings.Index(s, "/binary?arch=$GOARCH")
	digest := strings.Index(s, "sha256 no coincide")
	stop := strings.Index(s, `"$INIT" stop`)
	replace := strings.Index(s, `mv -f /tmp/netpulse-agent.new "$BIN"`)
	if download < 0 || digest < 0 || stop < 0 || replace < 0 {
		t.Fatalf("falta un paso: download=%d digest=%d stop=%d replace=%d", download, digest, stop, replace)
	}
	if !(download < digest && digest < stop && stop < replace) {
		t.Errorf("orden incorrecto: download=%d digest=%d stop=%d replace=%d", download, digest, stop, replace)
	}

	if !strings.Contains(s, "vuelvo a arrancar el agente anterior") {
		t.Error("el fallo al reemplazar el binario debe avisar del rollback")
	}
	if !strings.Contains(s, `"$INIT" start`) {
		t.Error("el fallo al reemplazar el binario debe volver a arrancar el agente")
	}
	if !strings.Contains(s, "exit 23") {
		t.Error("falta el codigo de salida del reemplazo fallido")
	}

	// Los diagnosticos tienen que ir a stderr: el servidor solo captura stderr.
	for _, want := range []string{
		`sha256 no coincide (esperado $SHA256, obtenido $GOT)" >&2`,
		`no pude detectar el endianness de $ARCH" >&2`,
		`arch no soportado: $ARCH" >&2`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("mensaje sin stderr: %s", want)
		}
	}

	if err := exec.Command("sh", "-n", "-c", s).Run(); err != nil {
		t.Fatalf("script invalido: %v", err)
	}
}
