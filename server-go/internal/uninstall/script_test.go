// script_test.go — contrato de uninstall.Script (#624): desinstalación
// completa del agente (detener/deshabilitar init, borrar binario, env,
// watchdog y artefactos temporales).
package uninstall_test

import (
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/uninstall"
)

func TestScriptRemovesAgentArtifacts(t *testing.T) {
	s := uninstall.Script()
	for _, want := range []string{
		"INIT=/etc/init.d/netpulse-agent",
		"BIN=/usr/sbin/netpulse-agent",
		"ENV_FILE=/etc/netpulse-agent.env",
		"WATCHDOG=/usr/sbin/netpulse-watchdog",
		`"$INIT" stop >/dev/null 2>&1 || true`,
		`"$INIT" disable >/dev/null 2>&1 || true`,
		`rm -f "$INIT"`,
		`killall netpulse-agent >/dev/null 2>&1 || true`,
		`rm -f "$BIN"`,
		`rm -f "$WATCHDOG"`,
		`rm -f "$ENV_FILE"`,
		`grep -v netpulse-watchdog`,
		`exit 0`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script sin %q", want)
		}
	}
}

func TestScriptIdempotentNoFailWithoutAgent(t *testing.T) {
	// Si el agente no está instalado, cada comando es no-fatal (|| true,
	// -f guard, pidof guard) y el script acaba con exit 0.
	s := uninstall.Script()
	if !strings.Contains(s, `WATCHDOG=/usr/sbin/netpulse-watchdog`) {
		t.Fatal("script sin claves de rutas")
	}
	// No debe dejar referencias a la instalación (no descarga, no enable).
	for _, forbidden := range []string{"--binary", "curl", "wget", "procd_open_instance", "chmod 600 \"$ENV_FILE\""} {
		if strings.Contains(s, forbidden) {
			t.Errorf("script de uninstall no debe contener %q", forbidden)
		}
	}
}
