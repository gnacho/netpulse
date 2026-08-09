// adguard_probe.go — Fase 17.1: detección del estado de AdGuard Home en un
// router vía SSH, para decidir el escenario de instalación antes de generar
// el plan. El servidor ejecuta UN solo comando combinado y parsea el output.
//
// Cubre los 3 escenarios auto-detectados (solo OpenWrt stock):
//  1. managed_by_firmware (fork GL.iNet) → ABORT.
//  2. opkg/apk en feeds → install por gestor de paquetes.
//  3. no en feeds → download del binario oficial de GitHub.
//
// La detección vive en el SERVIDOR (no en el agente) para que el abort llegue
// al usuario en el dry-run (POST /api/plans) ANTES de aplicar nada.
package orchestr

import (
	"strings"
	"time"
)

// CommandRunner ejecuta un comando en un router por host. adapters.SSHPool en
// producción; fake en tests. Mismo contrato que httpapi.SSHRunner.
type CommandRunner interface {
	Run(host, cmd string, timeout time.Duration) (string, error)
}

// AdGuardScenario describe el estado detectado de AdGuard en un router.
type AdGuardScenario struct {
	// ManagedByFirmware: el router trae un fork de AdGuard del fabricante
	// (p. ej. GL.iNet gl-sdk4-adguardhome). NUNCA se toca: su UI manda.
	ManagedByFirmware bool
	// GLinetPackages: paquetes gl-sdk4-*adguardhome hallados (para diagnóstico).
	GLinetPackages []string
	// Arch: salida de `uname -m` (aarch64, x86_64, armv7l, ...).
	Arch string
	// AdguardSuffix: sufijo del tarball oficial de GitHub (arm64, amd64, ...).
	// Vacío si la arch no está soportada por AdGuard.
	AdguardSuffix string
	// OpkgAvailable: `adguard-home` listado en `opkg list` (feeds).
	OpkgAvailable bool
	// ApkAvailable: `adguard-home` aparece en `apk search` (OpenWrt 24+).
	ApkAvailable bool
	// BinaryPresent: /usr/bin/AdGuardHome es ejecutable (instalación oficial
	// previa nuestra; distinguir del fork GL.iNet vía ManagedByFirmware).
	BinaryPresent bool
}

// probeCmd es el comando combinado de detección. Una sola sesión SSH.
// Diseño: marcadores ===NAME=== para parseo robusto; grep tolerante a
// mayúsculas; `2>/dev/null` para que paquetes/herramientas ausentes no rompan.
const probeCmd = `echo '===OPKG_INST==='
opkg list-installed 2>/dev/null | grep -i adguard
echo '===APK_INST==='
apk list --installed 2>/dev/null | grep -i adguard
echo '===ARCH==='
uname -m
echo '===OPKG_AVAIL==='
opkg list 2>/dev/null | grep -E '^adguard-home'
echo '===APK_AVAIL==='
apk search adguard-home 2>/dev/null
echo '===BINARY==='
test -x /usr/bin/AdGuardHome && echo present || echo absent
echo '===END==='`

// DetectAdGuard ejecuta el comando combinado vía SSH y parsea el estado.
// Timeout holgado (8s): `opkg list` puede tardar en routers con muchos paquetes.
func DetectAdGuard(run CommandRunner, host string) (AdGuardScenario, error) {
	out, err := run.Run(host, probeCmd, 8*time.Second)
	if err != nil {
		return AdGuardScenario{}, err
	}
	return parseAdGuardProbe(out), nil
}

// InstallMethod resume el escenario en una etiqueta:
// "abort" (managed_by_firmware) | "none" (binario oficial ya presente) |
// "apk" | "opkg" | "binary" (download oficial).
// Orden de precedencia: abort > none > apk > opkg > binary.
func (s AdGuardScenario) InstallMethod() string {
	if s.ManagedByFirmware {
		return "abort"
	}
	if s.BinaryPresent {
		return "none"
	}
	if s.ApkAvailable {
		return "apk"
	}
	if s.OpkgAvailable {
		return "opkg"
	}
	return "binary"
}

// parseAdGuardProbe extrae el AdGuardScenario del output del probeCmd.
// Función pura (testeable sin SSH).
func parseAdGuardProbe(out string) AdGuardScenario {
	sc := AdGuardScenario{}
	sections := splitSections(out)

	// Fork de fabricante: paquetes gl-sdk4-*adguardhome (en opkg o apk).
	for _, line := range append(sections["OPKG_INST"], sections["APK_INST"]...) {
		l := strings.ToLower(line)
		if strings.Contains(l, "gl-sdk4-adguardhome") || strings.Contains(l, "gl-sdk4-ui-adguardhome") {
			sc.GLinetPackages = append(sc.GLinetPackages, strings.TrimSpace(line))
		}
	}
	sc.ManagedByFirmware = len(sc.GLinetPackages) > 0

	if arch := firstNonEmpty(sections["ARCH"]); arch != "" {
		sc.Arch = arch
		sc.AdguardSuffix = archToSuffix(arch)
	}
	sc.OpkgAvailable = len(sections["OPKG_AVAIL"]) > 0
	sc.ApkAvailable = len(sections["APK_AVAIL"]) > 0
	if bin := firstNonEmpty(sections["BINARY"]); strings.Contains(strings.ToLower(bin), "present") {
		sc.BinaryPresent = true
	}
	return sc
}

// splitSections parte el output por marcadores ===NAME=== en un map nombre→líneas.
// Las líneas entre marcadores (no vacías) se asignan a la sección vigente.
func splitSections(out string) map[string][]string {
	res := map[string][]string{}
	var cur string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := sectionMarker(line); m != "" {
			cur = m
			if _, ok := res[cur]; !ok {
				res[cur] = []string{}
			}
			continue
		}
		if cur == "" {
			continue
		}
		if s := strings.TrimSpace(line); s != "" {
			res[cur] = append(res[cur], line)
		}
	}
	return res
}

// sectionMarker devuelve el nombre si la línea es "===NAME===" (NAME != END),
// o "" si no es un marcador.
func sectionMarker(line string) string {
	const m = "==="
	if len(line) < len(m)*2+1 || !strings.HasPrefix(line, m) || !strings.HasSuffix(line, m) {
		return ""
	}
	name := strings.Trim(line, "=")
	if name == "END" || name == "" {
		return ""
	}
	return name
}

func firstNonEmpty(lines []string) string {
	for _, l := range lines {
		if s := strings.TrimSpace(l); s != "" {
			return s
		}
	}
	return ""
}

// archToSuffix mapea `uname -m` al sufijo del tarball oficial de AdGuard Home
// en GitHub releases (AdGuardHome_linux_<suffix>.tar.gz).
func archToSuffix(arch string) string {
	switch strings.ToLower(arch) {
	case "aarch64", "arm64":
		return "arm64"
	case "x86_64", "amd64":
		return "amd64"
	case "armv7l", "armv7":
		return "armv7"
	case "armv6l":
		return "armv6"
	case "armv5tel", "armv5":
		return "armv5"
	case "i386", "i486", "i586", "i686", "386":
		return "386"
	case "mips":
		return "mips"
	case "mipsle":
		return "mipsle"
	case "mips64":
		return "mips64"
	case "mips64le":
		return "mips64le"
	case "ppc64le":
		return "ppc64le"
	default:
		return ""
	}
}
