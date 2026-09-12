// owut_flow.go - ciclo owut completo (#761): lista de versiones disponibles,
// instalación del paquete y comando de upgrade desatendido.
//
// `owut upgrade` es no interactivo por diseño (verificado sobre el fuente
// ucode de owut 2026.07): aborta solo si los checks fallan, hace warn sin
// preguntar cuando hay default packages perdidos, y si no hay cambios termina
// con exit 0 sin tocar nada (idempotencia nativa). Sin -V reconstruye la
// versión instalada con los paquetes al día; con -V VERSION salta de versión
// conservando los paquetes instalados (attended sysupgrade vía ASU).
package firmware

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	owutVersionsTimeout = 60 * time.Second
	owutInstallTimeout  = 240 * time.Second
	// OwutUpgradeTimeout: build ASU + descarga + verificación + flash. El
	// build remoto puede tardar minutos; el flash final mata dropbear y la
	// sesión SSH muere con el router reiniciándose (resultado esperado).
	OwutUpgradeTimeout = 15 * time.Minute
)

// OwutVersion es una versión destino que ASU puede construir para el router.
type OwutVersion struct {
	Version string `json:"version"`
	Branch  string `json:"branch"`
	Latest  bool   `json:"latest,omitempty"`
	Newer   bool   `json:"newer,omitempty"`
}

var (
	owutBranchRe = regexp.MustCompile(`^\s{2}(\S+)\s+release branch\s*$`)
	owutVerRe    = regexp.MustCompile(`^\s{4}(\S+)(\s+\(latest\))?\s*$`)
)

// ListOwutVersions parsea `owut versions` y devuelve las versiones estables
// (se excluyen -rc* y *-SNAPSHOT) con la marca Latest y Newer respecto a la
// versión instalada actual (vacía = todas sin comparar).
func ListOwutVersions(runner Runner, host, current string) ([]OwutVersion, error) {
	if runner == nil || host == "" {
		return nil, fmt.Errorf("firmware: runner o host vacío")
	}
	out, err := runner.Run(host, "owut versions 2>&1", owutVersionsTimeout)
	if err != nil {
		return nil, err
	}
	return ParseOwutVersions(out, current), nil
}

// ParseOwutVersions interpreta la salida de `owut versions` (función pura,
// testeada): ramas con dos espacios y versiones con cuatro.
func ParseOwutVersions(out, current string) []OwutVersion {
	var versions []OwutVersion
	branch := ""
	for _, line := range strings.Split(out, "\n") {
		if m := owutBranchRe.FindStringSubmatch(line); m != nil {
			branch = m[1]
			continue
		}
		if m := owutVerRe.FindStringSubmatch(line); m != nil {
			v := m[1]
			if !stableVersion(v) {
				continue
			}
			versions = append(versions, OwutVersion{
				Version: v,
				Branch:  branch,
				Latest:  m[2] != "",
				Newer:   current != "" && VersionCmp(v, current) > 0,
			})
		}
	}
	return versions
}

// stableVersion filtra release candidates y snapshots del desplegable
// (incluido el "SNAPSHOT" literal con que cierra cada rama del server ASU).
func stableVersion(v string) bool {
	if v == "SNAPSHOT" || !strings.ContainsFunc(v, isDigitRune) {
		return false
	}
	return !strings.Contains(v, "-rc") && !strings.HasSuffix(v, "-SNAPSHOT")
}

func isDigitRune(r rune) bool { return r >= '0' && r <= '9' }

// VersionCmp compara dos versiones OpenWrt ("25.12.5") numéricamente por
// componentes; devuelve -1/0/1. Componentes no numéricos valen 0.
func VersionCmp(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// MajorJump informa de si pasar de current a target cambia de rama mayor
// ("23.05.5" -> "24.10.0" es salto mayor; "23.05.5" -> "23.05.7" no).
func MajorJump(current, target string) bool {
	if current == "" || target == "" {
		return false
	}
	cs := strings.Split(current, ".")
	ts := strings.Split(target, ".")
	if len(cs) == 0 || len(ts) == 0 {
		return false
	}
	return cs[0] != ts[0]
}

// owutInstallCmd detecta el gestor de paquetes del router e instala owut.
// El reintento con el repo oficial de OpenWrt cubre los firmware de vendor
// (p. ej. GL.iNet op25) cuyos mirrors propios no empaquetan owut: se usa la
// versión del propio router (/etc/openwrt_release) y el arch de apk. En
// releases sin paquete owut (23.05 y anteriores) el comando falla con el
// error del gestor.
const owutInstallCmd = `if command -v apk >/dev/null 2>&1; then ` +
	`apk update >/dev/null 2>&1; ` +
	`apk add owut || apk add --repository "https://downloads.openwrt.org/releases/$(. /etc/openwrt_release 2>/dev/null; echo ${DISTRIB_RELEASE%%-*})/packages/$(apk --print-arch)/packages/packages.adb" owut; ` +
	`elif command -v opkg >/dev/null 2>&1; then opkg update && opkg install owut; ` +
	`else printf '__no_pm__\n'; fi 2>&1; printf '\n__owut_exit__=%d\n' $?`

// InstallOwut instala el paquete owut en el router con el gestor disponible
// (con reintento vía repo oficial para firmware de vendor). Devuelve la
// salida del gestor; error si no hay gestor, falla la instalación o falla el
// transporte SSH.
func InstallOwut(runner Runner, host string) (string, error) {
	if runner == nil || host == "" {
		return "", fmt.Errorf("firmware: runner o host vacío")
	}
	out, err := runner.Run(host, owutInstallCmd, owutInstallTimeout)
	if err != nil {
		return out, err
	}
	exit, raw := extractExitCode(out)
	if strings.Contains(raw, "__no_pm__") {
		return raw, fmt.Errorf("firmware: el router no tiene apk ni opkg")
	}
	if exit != 0 {
		tail := raw
		if len(tail) > 300 {
			tail = "..." + tail[len(tail)-300:]
		}
		return raw, fmt.Errorf("instalación de owut falló (exit %d): %s", exit, strings.TrimSpace(tail))
	}
	return raw, nil
}

// OwutUpgradeCmd construye el comando de upgrade desatendido. Con target
// distinto de la versión actual añade -V TARGET (salto de versión); si son
// iguales (o current desconocida) reconstruye la versión instalada con los
// paquetes al día. removePkgs excluye paquetes locales que no están en los
// feeds oficiales ( owut "-r"): sin ellos ASU se niega a construir la imagen.
func OwutUpgradeCmd(target, current string, removePkgs []string) string {
	cmd := "owut upgrade -q"
	if target != "" && current != "" && target != current {
		cmd += " -V " + shQuote(target)
	}
	if pkgs := sanitizeRemovePkgs(removePkgs); pkgs != "" {
		cmd += " -r " + shQuote(pkgs)
	}
	return cmd
}

// pkgNameRe: nombre de paquete apk/opkg sano (defensa contra inyección en
// el comando SSH; el shQuote ya protege, esto limita el abuso).
var pkgNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]*$`)

// sanitizeRemovePkgs filtra y une la lista de exclusiones (máx 20).
func sanitizeRemovePkgs(list []string) string {
	var keep []string
	for _, p := range list {
		p = strings.TrimSpace(p)
		if p != "" && pkgNameRe.MatchString(p) && len(keep) < 20 {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, " ")
}

// RunOwutUpgrade ejecuta el comando de upgrade con el wrapper de exit code y
// devuelve (exit, salida, error de transporte). Un error de transporte al
// final del flujo es NORMAL: el sysupgrade reinicia el router y la sesión SSH
// muere sin llegar a escribir la línea de exit (el caller lo interpreta).
func RunOwutUpgrade(runner Runner, host, cmd string) (int, string, error) {
	wrapped := cmd + ` 2>&1; printf '\n__owut_exit__=%d\n' $?`
	out, err := runner.Run(host, wrapped, OwutUpgradeTimeout)
	exit, raw := extractExitCode(out)
	return exit, raw, err
}

// OwutInstalled informa de si el router tiene owut en su PATH (detección
// local y barata; la usa el loop de recurrencia y el endpoint de versiones).
func OwutInstalled(runner Runner, host string) bool {
	return detectOwut(runner, host)
}

// shQuote entrecomilla un valor para shell POSIX (single-quote escaping).
func shQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}
