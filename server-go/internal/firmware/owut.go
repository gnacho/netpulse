// owut.go - detección y check de owut (attended sysupgrade) en un router (#695).
//
// Fase 1: solo detección + `owut check` para revisión, SIN download ni install
// (eso llega en una fase posterior con confirmación explícita). owut requiere el
// paquete ucode-mod-uclient, disponible solo en OpenWrt 24.10+; en 23.05 y
// anteriores no existe (el equivalente es auc), así que la detección es
// imprescindible y el flujo sysupgrade actual sigue siendo el fallback.
//
// `owut check` consulta el sysupgrade server oficial (sysupgrade.openwrt.org),
// así que puede tardar y puede fallar por causas externas (paquete sin build,
// rate limit, caída). El resultado se expone tal cual, sin bloquear el resto.
package firmware

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Techos de tiempo de las sondas owut por SSH. El check golpea la infra ASU
// remota, de ahí el timeout generoso; la detección es local y barata. Los fija
// este paquete (igual que uciShowTimeout/uciApplyTimeout); no son configurables.
const (
	owutDetectTimeout = 10 * time.Second
	owutCheckTimeout  = 60 * time.Second
)

// owutDetectCmd comprueba si el binario owut está en el PATH del router.
const owutDetectCmd = "command -v owut"

// owutCheckCmd envuelve `owut check` fusionando stderr en stdout y añadiendo
// una línea final con el exit code. SSHPool.Run descarta el stdout cuando el
// comando sale con código distinto de cero (session.Output), así que sin este
// wrapper un check fallido perdería su salida. La línea `__owut_exit__=N` se
// parsea y se retira del rawOutput antes de exponerlo.
const owutCheckCmd = "owut check 2>&1; printf '\\n__owut_exit__=%d\\n' $?"

// owutExitRe extrae el exit code embebido por el wrapper de owutCheckCmd.
var owutExitRe = regexp.MustCompile(`__owut_exit__=(\d+)`)

// Runner ejecuta comandos en un router por SSH. Lo satisfacen httpapi.SSHRunner
// y *adapters.SSHPool; en tests se inyecta un fake (mismo contrato que el
// SSHRunner fakeable de httpapi).
type Runner interface {
	Run(host, cmd string, timeout time.Duration) (string, error)
}

// OwutStatus es el resultado de detectar owut y, si está disponible, de
// ejecutar `owut check` en el router. Se expone tal cual en la API para que la
// UI lo muestre para revisión (Fase 1: sin acción de descarga/instalación).
type OwutStatus struct {
	// OwutAvailable: el router tiene el binario owut en su PATH.
	OwutAvailable bool `json:"owutAvailable"`
	// CheckRan: se llegó a ejecutar `owut check` y devolvió salida (aunque el
	// check en sí informara de errores de la infra ASU).
	CheckRan bool `json:"checkRan"`
	// UpgradeAvailable: hay una versión más reciente (paquetes out-of-date o
	// downgrades) a la que actualizar.
	UpgradeAvailable bool `json:"upgradeAvailable"`
	// Buildable: la infraestructura ASU puede construir la imagen (owut salió
	// con código 0, es decir, sin paquetes missing ni build failures).
	Buildable bool `json:"buildable"`
	// RawOutput: salida completa de `owut check` (sin la línea de exit code).
	RawOutput string `json:"rawOutput,omitempty"`
	// Error: fallo de transporte (router inalcanzable, timeout SSH), no un
	// fallo del check en sí.
	Error string `json:"error,omitempty"`
}

// CheckOwut detecta owut en el router y, si existe, ejecuta `owut check`.
// Tolerante: un runner nil o un fallo de SSH devuelve un OwutStatus con los
// flags a false y, si procede, el Error, sin romper el flujo del caller.
func CheckOwut(runner Runner, host string) OwutStatus {
	if runner == nil || host == "" {
		return OwutStatus{}
	}
	if !detectOwut(runner, host) {
		return OwutStatus{OwutAvailable: false}
	}
	status := OwutStatus{OwutAvailable: true}
	out, err := runner.Run(host, owutCheckCmd, owutCheckTimeout)
	if err != nil {
		// Fallo de transporte (SSH caído/timeout), no un resultado del check.
		status.Error = err.Error()
		return status
	}
	exitCode, raw := extractExitCode(out)
	status.CheckRan = raw != ""
	status.RawOutput = raw
	status.UpgradeAvailable, status.Buildable = parseOwutCheck(raw, exitCode)
	return status
}

// detectOwut comprueba si el router tiene owut ejecutable en su PATH.
func detectOwut(runner Runner, host string) bool {
	out, err := runner.Run(host, owutDetectCmd, owutDetectTimeout)
	return err == nil && strings.TrimSpace(out) != ""
}

// extractExitCode separa la línea `__owut_exit__=N` del resto de la salida.
// Devuelve el exit code (0 por defecto si no se encuentra) y el texto limpio.
func extractExitCode(out string) (int, string) {
	m := owutExitRe.FindStringSubmatch(out)
	if m == nil {
		return 0, strings.TrimRight(out, "\n")
	}
	code, err := strconv.Atoi(m[1])
	if err != nil {
		code = 0
	}
	// Elimina la línea del marcador (y el \n previo que le precede).
	raw := owutExitRe.ReplaceAllString(out, "")
	return code, strings.TrimRight(raw, "\n")
}

// parseOwutCheck interpreta la salida de `owut check` a dos señales claras:
// si hay upgrade disponible y si ASU puede construir la imagen. El mapeo está
// tomado de la fuente oficial de owut (check_updates + el resumen final):
//   - buildable: exit code 0 (owut solo pone EXIT_OK con check_pkg_builds()
//     ok y sin paquetes missing).
//   - upgradeAvailable: la salida reporta cambios ("N packages are out-of-date"
//     o "N packages were downgraded") o el resumen "safe to proceed".
func parseOwutCheck(out string, exitCode int) (upgradeAvailable, buildable bool) {
	buildable = exitCode == 0
	upgradeAvailable = strings.Contains(out, "packages are out-of-date") ||
		strings.Contains(out, "packages were downgraded") ||
		strings.Contains(out, "safe to proceed")
	return upgradeAvailable, buildable
}
