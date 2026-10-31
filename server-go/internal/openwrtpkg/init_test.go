// Package openwrtpkg guarda los checks de empaquetado OpenWrt. Es un
// paquete solo-de-tests: valida los ficheros reales de deploy/openwrt
// (init scripts, uci-defaults y los scripts que generan el .ipk/.apk)
// contra las reglas de compatibilidad de OpenWrt 21.02 documentadas en la
// issue #1701, para que ninguna regresión vuelva a llegar a un .ipk roto.
package openwrtpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const repoRel = "../../.."

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRel, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

func countOccurrences(s, needle string) int {
	return strings.Count(s, needle)
}

// codeLines devuelve el script sin líneas de comentario ni líneas vacías:
// los checks son sobre código ejecutable, no sobre los comentarios que lo
// explican.
func codeLines(t *testing.T, src string) string {
	t.Helper()
	var out []string
	for _, ln := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// TestServerInitOpenWrt21Compat (#1701): el init del servidor on-box debe
// cumplir las reglas de procd de OpenWrt 21.02 (GL.iNet GL-MT6000 del
// reporte): una sola llamada env, stdout/stderr booleanos, arranque tardío
// y guardia de montaje para no crear la base de datos en flash.
func TestServerInitOpenWrt21Compat(t *testing.T) {
	const rel = "deploy/openwrt/netpulse-server/files/netpulse-server.init"
	src := readRepoFile(t, rel)
	code := codeLines(t, src)

	if !strings.Contains(code, "START=99") {
		t.Error("START debe ser 99 para dejar montar los USB antes del arranque")
	}
	if got := countOccurrences(code, "procd_set_param env"); got != 1 {
		t.Errorf("procd_set_param env aparece %d veces, want 1 (en 21.02 cada llamada extra pisa la anterior)", got)
	}
	envLine := ""
	for _, ln := range strings.Split(code, "\n") {
		if strings.Contains(ln, "procd_set_param env") {
			envLine = ln
		}
	}
	for _, want := range []string{"NETPULSE_ONBOX=1", "DATA_DIR=", "PORT="} {
		if !strings.Contains(envLine, want) {
			t.Errorf("la unica llamada env debe incluir %s: %q", want, envLine)
		}
	}
	if strings.Contains(code, "procd_set_param stdout stderr") {
		t.Error(`"procd_set_param stdout stderr" no es valido en 21.02; usa stdout 1 / stderr 1`)
	}
	if !strings.Contains(code, "procd_set_param stdout 1") || !strings.Contains(code, "procd_set_param stderr 1") {
		t.Error("falta procd_set_param stdout 1 / stderr 1 (logs en logread)")
	}

	// Guardia de montaje: la comprobacion de /proc/mounts y el fallo temprano
	// deben aparecer ANTES del mkdir, o mkdir crearia el dir en flash.
	guard := strings.Index(src, "/proc/mounts")
	mkdir := strings.Index(src, `mkdir -p "$data_dir"`)
	if guard < 0 {
		t.Error("falta la guardia de montaje (/proc/mounts)")
	}
	if mkdir < 0 {
		t.Error("falta mkdir -p del data_dir")
	}
	if guard >= 0 && mkdir >= 0 && guard > mkdir {
		t.Error("la guardia de montaje debe ejecutarse antes de mkdir -p")
	}
	if !strings.Contains(src, "logger -t netpulse") {
		t.Error("el rechazo de arranque debe quedar en syslog (logger -t netpulse)")
	}
}

// TestServerPortDefaultsAgree (#1701): init, uci-defaults y config embarcado
// deben anunciar el mismo puerto por defecto (3100: el 3000 colisiona con la
// UI de AdGuard Home del firmware GL.iNet).
func TestServerPortDefaultsAgree(t *testing.T) {
	files := map[string]string{
		"deploy/openwrt/netpulse-server/files/netpulse-server.init":     "DEFAULT_PORT=3100",
		"deploy/openwrt/netpulse-server/files/netpulse-server.defaults": "netpulse.server.port=3100",
		"deploy/openwrt/netpulse-server/files/netpulse-server.config":   "option port '3100'",
	}
	for rel, want := range files {
		if src := readRepoFile(t, rel); !strings.Contains(src, want) {
			t.Errorf("%s: falta %q (puerto por defecto 3100)", rel, want)
		}
	}
}

// TestAgentInitOpenWrt21Compat (#1701): el init del agente comparte las
// reglas de procd de 21.02.
func TestAgentInitOpenWrt21Compat(t *testing.T) {
	const rel = "deploy/openwrt/netpulse-agent/files/netpulse-agent.init"
	code := codeLines(t, readRepoFile(t, rel))

	if strings.Contains(code, "procd_set_param stdout stderr") {
		t.Error(`"procd_set_param stdout stderr" no es valido en 21.02; usa stdout 1 / stderr 1`)
	}
	if !strings.Contains(code, "procd_set_param stdout 1") || !strings.Contains(code, "procd_set_param stderr 1") {
		t.Error("falta procd_set_param stdout 1 / stderr 1 (logs en logread)")
	}
	if got := countOccurrences(code, "procd_set_param env"); got > 1 {
		t.Errorf("procd_set_param env aparece %d veces, maximo 1 (en 21.02 cada llamada extra pisa la anterior)", got)
	}
}

// TestPackageScriptsStageExecutableInit (#1701): los scripts de empaquetado
// ipk deben instalar init y uci-defaults con modo 0755 (ipkg-build NO
// corrige permisos: con 0644 el enable/start falla con Permission denied) y
// verificar los modos dentro del .ipk resultante.
func TestPackageScriptsStageExecutableInit(t *testing.T) {
	type want struct {
		install string
		verify  string
	}
	cases := map[string]want{
		"deploy/openwrt/package-server.sh": {
			install: "install -m 0755 \"$REPO_ROOT/deploy/openwrt/netpulse-server/files/netpulse-server.init\"",
			verify:  "etc/init.d/netpulse=755",
		},
		"deploy/openwrt/package.sh": {
			install: "install -m 0755 \"$REPO_ROOT/deploy/openwrt/netpulse-agent/files/netpulse-agent.init\"",
			verify:  "etc/init.d/netpulse-agent=755",
		},
	}
	for rel, w := range cases {
		src := readRepoFile(t, rel)
		if !strings.Contains(src, w.install) {
			t.Errorf("%s: el init debe empaquetarse con install -m 0755", rel)
		}
		if !strings.Contains(src, w.verify) {
			t.Errorf("%s: falta la verificación verify_ipk_modes de %s", rel, w.verify)
		}
		if !strings.Contains(src, "verify_ipk_modes") {
			t.Errorf("%s: falta la llamada a verify_ipk_modes", rel)
		}
	}

	// El helper compartido existe en sdk.sh (sourceado por ambos scripts).
	sdk := readRepoFile(t, "deploy/openwrt/sdk.sh")
	if !strings.Contains(sdk, "verify_ipk_modes() {") {
		t.Error("deploy/openwrt/sdk.sh: falta la función verify_ipk_modes")
	}
}
