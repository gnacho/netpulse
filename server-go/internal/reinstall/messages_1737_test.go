package reinstall_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/reinstall"
)

// #1737: un fallo del instalador tiene que dejar rastro en stderr. El servidor
// ya devuelve esa ultima linea al operador, pero un wget silencioso o un
// opkg que falla sin decir nada dejaban solo "Process exited with status 1".
func TestScriptExplainsItsFailures(t *testing.T) {
	s := reinstall.ScriptWithTargets(
		"test-router", strings.Repeat("a1", 32), "https://192.168.1.226:3000", "sha256//AAAA",
		map[string]string{"arm64": "cafebabe"},
		reinstall.WanTargets{},
	)

	for _, want := range []string{
		`could not download the binary from $SERVER (curl)`,
		`could not download the binary from $SERVER (wget)`,
		`the .ipk package did not install`,
		`the .apk package did not install`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("el script no explica el fallo: %q", want)
		}
	}

	if err := exec.Command("sh", "-n", "-c", s).Run(); err != nil {
		t.Fatalf("script invalido: %v", err)
	}
}
