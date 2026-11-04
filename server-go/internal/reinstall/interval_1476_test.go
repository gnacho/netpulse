package reinstall_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/reinstall"
)

// #1476: el intervalo del agente es un ajuste por router y el script lo
// escribe en la config UCI del paquete, que es el conffile que sobrevive a
// un sysupgrade. Con 0 se borra la clave y el agente vuelve a su default.
func TestScriptSyncsAgentInterval(t *testing.T) {
	token := strings.Repeat("a1", 32)

	s := reinstall.ScriptWithTargets(
		"test-router", token, "https://192.168.1.226:3000", "sha256//AAAA",
		map[string]string{"arm64": "cafebabe"},
		reinstall.WanTargets{Interval: 90},
	)
	if !strings.Contains(s, `uci -q set netpulse-agent.main.interval="90"`) {
		t.Error("el script no escribe el intervalo configurado")
	}
	if err := exec.Command("sh", "-n", "-c", s).Run(); err != nil {
		t.Fatalf("script inválido con intervalo: %v", err)
	}

	zero := reinstall.ScriptWithTargets(
		"test-router", token, "https://192.168.1.226:3000", "sha256//AAAA",
		map[string]string{"arm64": "cafebabe"},
		reinstall.WanTargets{},
	)
	if !strings.Contains(zero, "uci -q delete netpulse-agent.main.interval") {
		t.Error("con intervalo 0 el script debe borrar la clave")
	}
	if strings.Contains(zero, `uci -q set netpulse-agent.main.interval=""`) {
		t.Error("no debe escribir un intervalo vacío")
	}
}
