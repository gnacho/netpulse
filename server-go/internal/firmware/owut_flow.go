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

// owutInstallCmd detecta el gestor de paquetes del router e instala owut con
// los repositorios PROPIOS del router (los firmware vanilla lo empaquetan).
// En releases sin paquete owut (23.05-) el comando falla con el error del
// gestor; en firmware de fabricante (GL.iNet) el flujo va bloqueado antes
// (cada vendor gestiona sus actualizaciones en su propio panel).
const owutInstallCmd = `if command -v apk >/dev/null 2>&1; then ` +
	`apk update >/dev/null 2>&1; ` +
	`apk add owut; ` +
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

// Platform es la foto de plataforma de un router: si tiene owut y si lleva
// firmware de fabricante. Vendor != "" significa firmware compilado por el
// vendor con su propia gestión de actualizaciones (GL.iNet): NetPulse NO
// ofrece upgrades ASU ahí, el vendor ya resuelve los suyos en su panel.
type Platform struct {
	Owut   bool
	Vendor string // "" | "gl-inet"
}

// platformDetectCmd: una sola ida SSH resuelve ambas señales. La marca de
// vendor son los repositorios propios (fw.gl-inet.com en apk u opkg): el
// /etc/openwrt_release de los GL op25 es idéntico al vanilla y NO sirve.
// OJO: busybox grep sale con 2 si algún fichero listado no existe (aunque
// haya matches), así que el match se decide por salida (grep -q .), no por
// exit code del primer grep.
const platformDetectCmd = `command -v owut >/dev/null 2>&1 && printf '__owut__\n'; ` +
	`grep -hs fw.gl-inet.com /etc/apk/repositories /etc/apk/repositories.d/* /etc/opkg/distfeeds.conf 2>/dev/null | grep -q . && printf '__gl__\n'; true`

// DetectPlatform sondea owut y vendor del router (tolerante: runner/host
// vacíos → Platform vacía).
func DetectPlatform(runner Runner, host string) Platform {
	if runner == nil || host == "" {
		return Platform{}
	}
	out, _ := runner.Run(host, platformDetectCmd, owutDetectTimeout)
	return ParsePlatform(out)
}

// ParsePlatform interpreta las marcas de platformDetectCmd (función pura).
func ParsePlatform(out string) Platform {
	p := Platform{}
	if strings.Contains(out, "__owut__") {
		p.Owut = true
	}
	if strings.Contains(out, "__gl__") {
		p.Vendor = "gl-inet"
	}
	return p
}

// stackPreserveCmd garantiza de forma idempotente que los ficheros del
// stack propio presente en el router (netgrip y/o agente NetPulse) estén en
// /etc/sysupgrade.conf: ASU no puede incluir paquetes sin feed en la imagen,
// pero los ficheros listados SOBREVIVEN al flash (el servicio arranca en el
// primer boot; el registro apk se recupera con una reinstalación posterior).
const stackPreserveCmd = `P=/etc/sysupgrade.conf; touch $P; ensure() { grep -qxF "$1" $P || echo "$1" >> $P; }; ` +
	`if [ -f /usr/sbin/netgrip ]; then ensure /usr/sbin/netgrip; ensure /etc/init.d/netgrip; ` +
	`ensure /usr/libexec/netgrip-restore-rules; ensure /etc/netgrip/; ` +
	`for f in /etc/rc.d/*netgrip*; do [ -e "$f" ] && ensure "$f"; done; fi; ` +
	`if [ -f /usr/sbin/netpulse-agent ]; then ensure /usr/sbin/netpulse-agent; ensure /etc/netpulse-agent.env; ` +
	`ensure /usr/sbin/netpulse-watchdog; ensure /etc/init.d/netpulse-agent; ` +
	`for f in /etc/rc.d/*netpulse-agent*; do [ -e "$f" ] && ensure "$f"; done; fi; true`

// EnsureStackPreserved ejecuta la preservación en el router (tolerante: un
// runner nil es no-op; devuelve el error de transporte si falla y el caller
// decide si continúa).
func EnsureStackPreserved(runner Runner, host string) error {
	if runner == nil || host == "" {
		return nil
	}
	_, err := runner.Run(host, stackPreserveCmd, owutDetectTimeout)
	return err
}

// OpenWrtVersionRe: versión destino válida para -V ("25.12.5", "24.10.2").
// Evita que textos libres guardados como target disparen upgrades sin sentido.
var OpenWrtVersionRe = regexp.MustCompile(`^\d+\.\d+(\.\d+)?(-[a-zA-Z0-9.]+)?$`)

// ValidOpenWrtVersion informa de si target parece una versión OpenWrt.
func ValidOpenWrtVersion(v string) bool {
	return OpenWrtVersionRe.MatchString(strings.TrimSpace(v))
}

// shQuote entrecomilla un valor para shell POSIX (single-quote escaping).
func shQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}
