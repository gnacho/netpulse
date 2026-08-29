// imports_test.go (#369): garantía arquitectural de que el runtime compartido
// NUNCA depende del paquete de cedida. El runtime lo consumen el standalone
// y el agente embebido de NetGrip; si llegara a importar internal/yield, el
// sabor embebido podría ceder el paso a sí mismo. Además, al ser internal,
// NetGrip (otro módulo) no puede importarlo: esta prueba cierra la puerta
// que quedaría dentro de nuestro propio módulo.
package runtime_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRuntimeDoesNotImportYield(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Skipf("go list no disponible: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if strings.Contains(string(out), "internal/yield") {
		t.Fatal("agent/runtime no debe depender de internal/yield: el agente embebido de NetGrip usa este runtime y NUNCA debe ceder")
	}
}
