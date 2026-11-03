package reinstall_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/reinstall"
)

// #1476: OpenWrt 25.12 trae apk y no opkg, asi que el paquete del servidor
// tiene que instalarse tambien por ahi. El .apk lo firma el CI y el servidor
// solo lo sirve con format=apk; si falta, el script cae al camino de ficheros
// sueltos como antes.
func TestScriptInstallsPackageWithAPK(t *testing.T) {
	s := reinstall.ScriptWithTargets(
		"test-router",
		strings.Repeat("a1", 32),
		"https://192.168.1.226:3000",
		"sha256//AAAA",
		map[string]string{"arm64": "cafebabe"},
		reinstall.WanTargets{},
	)

	for _, want := range []string{
		`elif command -v apk >/dev/null 2>&1; then`,
		`pkg_fetch "$PKG_URL&format=apk" /tmp/netpulse-agent.apk`,
		`apk add --allow-untrusted /tmp/netpulse-agent.apk`,
		`rm -f /tmp/netpulse-agent.apk`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("el script no contiene %q", want)
		}
	}

	// El bloque opkg sigue existiendo y va ANTES que el de apk: en un router
	// con ambos gestores manda el .ipk, que el servidor compone al vuelo y no
	// depende de que el operador haya copiado el asset.
	opkg := strings.Index(s, `command -v opkg`)
	apk := strings.Index(s, `elif command -v apk`)
	if opkg < 0 || apk < 0 {
		t.Fatalf("faltan los bloques de gestor de paquetes (opkg=%d apk=%d)", opkg, apk)
	}
	if opkg > apk {
		t.Error("el bloque opkg debe ir antes que el de apk")
	}

	if out, err := exec.Command("sh", "-n", "-c", s).CombinedOutput(); err != nil {
		t.Fatalf("script invalido para sh -n: %v\n%s", err, out)
	}
}

// La funcion pkg_fetch centraliza la descarga autenticada con curl o wget, de
// modo que ambos formatos comparten el mismo manejo de TLS y cabeceras.
func TestScriptPackageFetchIsShared(t *testing.T) {
	s := reinstall.ScriptWithTargets(
		"test-router",
		strings.Repeat("a1", 32),
		"https://192.168.1.226:3000",
		"",
		map[string]string{"arm64": "cafebabe"},
		reinstall.WanTargets{},
	)
	if !strings.Contains(s, `PKG_URL="$SERVER/api/agents/$SLUG/package?arch=$GOARCH"`) {
		t.Error("falta la base URL del paquete")
	}
	if strings.Count(s, `pkg_fetch "$PKG_URL&format=`) != 2 {
		t.Error("los dos formatos deben descargarse por pkg_fetch")
	}
	if !strings.Contains(s, `-H "Authorization: Bearer $TOKEN"`) {
		t.Error("la descarga debe autenticarse con el token del agente")
	}
}
