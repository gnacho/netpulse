// Package executor — ejecutor sandboxeado de operaciones de escritura en el
// router (Fase 10). El servidor envía una lista de Ops allowlistedas vía SSE;
// el ejecutor las valida, hace snapshot, aplica en staging, commit, healthcheck
// y rollback automático si falla.
//
// Allowlist estricta: cada Kind tiene un patrón regex por arg. Args que no
// casan → rechazo inmediato. Shell libre PROHIBIDO.
package executor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Op es una operación allowlistedada que el ejecutor puede aplicar.
type Op struct {
	Kind string            `json:"kind"` // uci_set|uci_delete|uci_add_list|uci_commit|service|install|wg_check
	Args map[string]string `json:"args"`
	Desc string            `json:"desc"` // descripción humana para el diff
}

// ApplyResult es el resultado de ejecutar un plan.
type ApplyResult struct {
	Status     string `json:"status"` // applied|failed|rolled_back
	Op         string `json:"op,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Snapshot   string `json:"snapshot,omitempty"` // configs afectados
}

// Runner ejecuta comandos en el router. Interfaz para inyectar fakes en tests.
type Runner interface {
	Run(name string, args ...string) (stdout string, exitCode int)
}

// shellRunner usa exec.Command en producción.
type shellRunner struct{}

func (shellRunner) Run(name string, args ...string) (string, int) {
	out, err := exec.Command(name, args...).Output()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return string(out), code
}

// patrones de validación por campo (sin shell metachars).
var (
	reConfig = regexp.MustCompile(`^[a-z_]+$`)
	// section: nombre de sección (wifi-iface, interface, eth1) o referencia
	// `@tipo[idx]` / `@tipo[-1]`. Los dígitos cubren interfaces UCI reales
	// (eth1, wlan0, br-lan). `[-1]` referencia la ÚLTIMA sección del tipo —
	// usado tras un uci_add para setear los campos de la sección recién
	// creada (patrón OpenWrt estándar).
	reSection = regexp.MustCompile(`^(@[a-z0-9_-]+(\[\d+\]|\[-1\])|[a-z0-9_-]+)$`)
	reOption  = regexp.MustCompile(`^[a-z0-9_]+$`)
	// value: alfanumérico + puntos, dos-puntos, barras, hashes, guiones.
	// Cubre IPs (192.168.1.1), DNS (1.1.1.1#3001), rutas, MACs, puertos.
	reValue      = regexp.MustCompile(`^[a-zA-Z0-9_.:/#,-]+$`)
	reService    = regexp.MustCompile(`^[a-z_-]+$`)
	reServiceAct = regexp.MustCompile(`^(restart|reload|enable|disable|start|stop)$`)
	rePackage    = regexp.MustCompile(`^[a-z0-9_-]+$`)
	// Rutas de fichero permitidas para write_file/download/extract/chmod.
	// "/root" excluido a propósito (no hace falta para AdGuard y reduce la
	// superficie: .ssh/id_rsa etc. no deben ser escribibles).
	// Además del regex, Validate aplica filepath.Clean + rechazo de ".." a los
	// args marcados como path en opSpec (defense in depth: ".." casa el charset).
	reFilePath = regexp.MustCompile(`^/(etc|tmp|usr/bin|usr/lib|var/etc)/[A-Za-z0-9_./-]+$`)
	reDirPath  = regexp.MustCompile(`^/(etc|tmp|usr/bin|usr/lib|opt|var/etc)/[A-Za-z0-9_./-]+$`)
	// Allowlist de dominios de descarga: SOLO releases oficiales de AdGuard.
	// Cualquier otra URL (incluida un attacker que controle el plan) se rechaza.
	reDownloadURL = regexp.MustCompile(`^https://github\.com/AdguardTeam/AdGuardHome/releases/download/v[0-9]+\.[0-9]+\.[0-9]+/AdGuardHome_linux_[a-z0-9]+\.tar\.gz$`)
	reMode        = regexp.MustCompile(`^[0-7]{3,4}$`)
	// base64 estándar (sin newlines). La validación final la hace base64.Decode.
	reBase64 = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)

	// tcp_check: host (IPv4 o hostname simple) + puerto (1-65535).
	reHost = regexp.MustCompile(`^([0-9]{1,3}[.]){3}[0-9]{1,3}$|^[a-zA-Z0-9.-]{1,253}$`)
	// rePort valida 1-65535 sin ceros a la izquierda.
	rePort = regexp.MustCompile(`^([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5])$`)
)

// opSpec define los args requeridos y su validación para cada Kind.
type opSpec struct {
	required map[string]*regexp.Regexp
	build    func(args map[string]string) (cmd string, cmdArgs []string)
	// exec, si está presente, reemplaza a build. Para Kinds que no son un
	// simple exec.Command (p. ej. write_file escribe el fichero en Go).
	exec func(run Runner, args map[string]string) int
	// pathArgs: args que son rutas de fichero/directorio. Validate les aplica
	// un check anti-traversal además del regex (filepath.Clean + sin "..").
	pathArgs []string
	configs  func(args map[string]string) []string // UCI configs afectados (para snapshot)
}

var allowlist = map[string]opSpec{
	"uci_set": {
		required: map[string]*regexp.Regexp{"config": reConfig, "section": reSection, "option": reOption, "value": reValue},
		build: func(a map[string]string) (string, []string) {
			return "uci", []string{"set", fmt.Sprintf("%s.%s.%s=%s", a["config"], a["section"], a["option"], a["value"])}
		},
		configs: func(a map[string]string) []string { return []string{a["config"]} },
	},
	"uci_delete": {
		required: map[string]*regexp.Regexp{"config": reConfig, "section": reSection, "option": reOption},
		build: func(a map[string]string) (string, []string) {
			return "uci", []string{"delete", fmt.Sprintf("%s.%s.%s", a["config"], a["section"], a["option"])}
		},
		configs: func(a map[string]string) []string { return []string{a["config"]} },
	},
	"uci_add_list": {
		required: map[string]*regexp.Regexp{"config": reConfig, "section": reSection, "option": reOption, "value": reValue},
		build: func(a map[string]string) (string, []string) {
			return "uci", []string{"add_list", fmt.Sprintf("%s.%s.%s=%s", a["config"], a["section"], a["option"], a["value"])}
		},
		configs: func(a map[string]string) []string { return []string{a["config"]} },
	},
	"uci_commit": {
		required: map[string]*regexp.Regexp{"config": reConfig},
		build: func(a map[string]string) (string, []string) {
			return "uci", []string{"commit", a["config"]}
		},
		configs: func(a map[string]string) []string { return []string{a["config"]} },
	},
	// uci_add: crea una sección nueva. `uci add <config> <type>` imprime el
	// nombre; luego uci_set sobre @<type>[-1] setea sus campos. Necesario para
	// crear wifi-iface / network / firewall zone en módulos como WiFi guest.
	// type: nombre de sección (wifi-iface, interface, zone, forwarding).
	"uci_add": {
		required: map[string]*regexp.Regexp{"config": reConfig, "type": reSection},
		build: func(a map[string]string) (string, []string) {
			return "uci", []string{"add", a["config"], a["type"]}
		},
		configs: func(a map[string]string) []string { return []string{a["config"]} },
	},
	// uci_delete_section: borra una sección completa (`uci delete <cfg>.<sec>`),
	// a diferencia de uci_delete que quita una option. Útil para revertir un
	// módulo que creó secciones (guest network: quitar la wifi-iface, la
	// interface, la zone y el forwarding creados al habilitar).
	"uci_delete_section": {
		required: map[string]*regexp.Regexp{"config": reConfig, "section": reSection},
		build: func(a map[string]string) (string, []string) {
			return "uci", []string{"delete", a["config"] + "." + a["section"]}
		},
		configs: func(a map[string]string) []string { return []string{a["config"]} },
	},
	// uci_set_named: crea una SECCIÓN NOMBRADA de un tipo: `uci set
	// <config>.<section>=<type>`. Diferencia con uci_add (sección anónima):
	// las network interfaces se identifican por su nombre de sección
	// (network.guest=interface), mientras que wifi-iface/zone/forwarding
	// pueden ser anónimas (referenciadas por índice @tipo[i]).
	"uci_set_named": {
		required: map[string]*regexp.Regexp{"config": reConfig, "section": reSection, "type": reSection},
		build: func(a map[string]string) (string, []string) {
			return "uci", []string{"set", a["config"] + "." + a["section"] + "=" + a["type"]}
		},
		configs: func(a map[string]string) []string { return []string{a["config"]} },
	},
	"service": {
		required: map[string]*regexp.Regexp{"name": reService, "action": reServiceAct},
		build: func(a map[string]string) (string, []string) {
			return "/etc/init.d/" + a["name"], []string{a["action"]}
		},
		configs: func(a map[string]string) []string { return nil },
	},
	"install": {
		required: map[string]*regexp.Regexp{"package": rePackage},
		build: func(a map[string]string) (string, []string) {
			return "opkg", []string{"install", a["package"]}
		},
		configs: func(a map[string]string) []string { return nil },
	},
	// apk_install: OpenWrt 24+ usa apk en vez de opkg.
	"apk_install": {
		required: map[string]*regexp.Regexp{"package": rePackage},
		build: func(a map[string]string) (string, []string) {
			return "apk", []string{"add", a["package"]}
		},
		configs: func(a map[string]string) []string { return nil },
	},
	// download: uclient-fetch (presente en OpenWrt por defecto) con allowlist
	// estricta de URL (solo releases oficiales de AdGuard).  El dest se valida
	// con reFilePath. No shell libre: la URL va como arg, no interpolada.
	"download": {
		required: map[string]*regexp.Regexp{"url": reDownloadURL, "dest": reFilePath},
		pathArgs: []string{"dest"},
		build: func(a map[string]string) (string, []string) {
			return "uclient-fetch", []string{a["url"], "-O", a["dest"]}
		},
		configs: func(a map[string]string) []string { return nil },
	},
	// write_file: escribe contenido base64 a path. Se hace en Go (no shell)
	// para evitar inyección vía base64 -d | sh. Path sanitizado + re-chequeo.
	// Crea el directorio padre si no existe (MkdirAll, modo 0755) — necesario
	// para /etc/AdGuardHome/AdGuardHome.yaml donde el dir no existe aún.
	"write_file": {
		required: map[string]*regexp.Regexp{"path": reFilePath, "content_b64": reBase64},
		pathArgs: []string{"path"},
		exec: func(_ Runner, a map[string]string) int {
			path := a["path"]
			clean := filepath.Clean(path)
			if clean != path || !reFilePath.MatchString(clean) || strings.Contains(clean, "..") {
				return 1
			}
			data, err := base64.StdEncoding.DecodeString(a["content_b64"])
			if err != nil {
				return 1
			}
			if dir := filepath.Dir(clean); dir != "" && dir != "/" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return 1
				}
			}
			if err := os.WriteFile(clean, data, 0644); err != nil {
				return 1
			}
			return 0
		},
		configs: func(a map[string]string) []string { return nil },
	},
	// mv: mueve un fichero. src y dest en path allowlist. Necesario para el
	// escenario "binary" de AdGuard (extraer tarball y mover el binario a
	// /usr/bin/AdGuardHome).
	"mv": {
		required: map[string]*regexp.Regexp{"src": reFilePath, "dest": reFilePath},
		pathArgs: []string{"src", "dest"},
		build: func(a map[string]string) (string, []string) {
			return "mv", []string{a["src"], a["dest"]}
		},
		configs: func(a map[string]string) []string { return nil },
	},
	// extract_tarball: tar -xzf SRC -C DEST.
	"extract_tarball": {
		required: map[string]*regexp.Regexp{"src": reFilePath, "dest": reDirPath},
		pathArgs: []string{"src", "dest"},
		build: func(a map[string]string) (string, []string) {
			return "tar", []string{"-xzf", a["src"], "-C", a["dest"]}
		},
		configs: func(a map[string]string) []string { return nil },
	},
	// chmod: modo octal (3-4 dígitos) + path.
	"chmod": {
		required: map[string]*regexp.Regexp{"mode": reMode, "path": reFilePath},
		pathArgs: []string{"path"},
		build: func(a map[string]string) (string, []string) {
			return "chmod", []string{a["mode"], a["path"]}
		},
		configs: func(a map[string]string) []string { return nil },
	},
	// tcp_check: abre una conexión TCP al host:port. Éxito (exit 0) si conecta
	// dentro del budget; fallo (exit 1) si no. Es un healthcheck real de
	// servicio (el ping del executor solo confirma red). Usado por módulos que
	// abren puerto (AdGuard :3000, Tailscale, OpenVPN...): si el servicio no
	// levanta, esta op falla → el executor revierte staged.
	// Se hace en Go (no shell) para no depender de nc, que no siempre está en
	// OpenWrt stock. Reintenta durante tcpCheckBudget (10 s por defecto) cada
	// tcpCheckRetry: un servicio puede tardar unos segundos en abrir puerto
	// tras `service start` (AdGuard, Tailscale...).
	"tcp_check": {
		required: map[string]*regexp.Regexp{"host": reHost, "port": rePort},
		exec: func(_ Runner, a map[string]string) int {
			addr := net.JoinHostPort(a["host"], a["port"])
			start := time.Now()
			for time.Since(start) < tcpCheckBudget {
				conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
				if err == nil {
					_ = conn.Close()
					return 0
				}
				time.Sleep(tcpCheckRetry)
			}
			return 1
		},
		configs: func(a map[string]string) []string { return nil },
	},
	// wg_check: healthcheck del módulo WireGuard (10.3). Ejecuta
	// `wg show <interface>`; exit 0 si el túnel está arriba en el kernel,
	// exit 1 si no existe o está caído. Al final del plan (tras el reload de
	// red), una fallo aquí significa que el apply aisló el túnel → el executor
	// restaura los snapshots (rollback real, ver Apply).
	"wg_check": {
		required: map[string]*regexp.Regexp{"interface": reSection},
		build: func(a map[string]string) (string, []string) {
			return "wg", []string{"show", a["interface"]}
		},
		configs: func(a map[string]string) []string { return nil },
	},
	// dawn_check: healthcheck del módulo DAWN. Ejecuta
	// `ubus call dawn get_network` y verifica que devuelva JSON no vacío
	// con vecinos. Si falla, el executor revierte los snapshots UCI.
	"dawn_check": {
		exec: func(run Runner, a map[string]string) int {
			out, code := run.Run("ubus", "call", "dawn", "get_network")
			if code != 0 {
				return code
			}
			out = strings.TrimSpace(out)
			if out == "" || out == "{}" {
				return 1
			}
			var v map[string]any
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				return 1
			}
			if len(v) == 0 {
				return 1
			}
			return 0
		},
		configs: func(a map[string]string) []string { return nil },
	},
	// wifi_reload: aplica cambios en /etc/config/wireless sin reboot.
	"wifi_reload": {
		build: func(a map[string]string) (string, []string) {
			return "/sbin/wifi", []string{"reload"}
		},
		configs: func(a map[string]string) []string { return nil },
	},
}

// tcpCheckBudget / tcpCheckRetry controlan los reintentos del Kind tcp_check.
// Son vars de paquete (no const) para poder acortarlos en tests.
var (
	tcpCheckBudget = 10 * time.Second
	tcpCheckRetry  = 500 * time.Millisecond
)

// Executor aplica Ops allowlistedas con snapshot + healthcheck + rollback.
type Executor struct {
	run       Runner
	now       func() time.Time
	gwTarget  string // IP del gateway para healthcheck (APs)
	wanTarget string // IP WAN para healthcheck (gateway)
}

// New crea un ejecutor. gwTarget/wanTarget se usan para el healthcheck
// post-apply (al menos uno debe estar configurado).
func New(gwTarget, wanTarget string) *Executor {
	return &Executor{run: shellRunner{}, now: time.Now, gwTarget: gwTarget, wanTarget: wanTarget}
}

// SetRunner inyecta un runner fake (tests).
func (e *Executor) SetRunner(r Runner) { e.run = r }

// SetClock inyecta un reloj (tests).
func (e *Executor) SetClock(f func() time.Time) { e.now = f }

// Validate comprueba que una Op pasa el allowlist sin ejecutarla.
func Validate(op Op) error {
	spec, ok := allowlist[op.Kind]
	if !ok {
		return fmt.Errorf("kind %q no allowlistedado", op.Kind)
	}
	for arg, re := range spec.required {
		val, present := op.Args[arg]
		if !present || val == "" {
			return fmt.Errorf("arg %q requerido para %s", arg, op.Kind)
		}
		if !re.MatchString(val) {
			return fmt.Errorf("arg %q=%q no válido para %s (no casa con %s)", arg, val, op.Kind, re)
		}
	}
	// Defense in depth: args marcados como path no pueden contener ".." ni
	// resolverse a algo distinto tras filepath.Clean (p. ej. "/etc/../x").
	for _, arg := range spec.pathArgs {
		val := op.Args[arg]
		if strings.Contains(val, "..") || filepath.Clean(val) != val {
			return fmt.Errorf("arg %q=%q contiene traversal (..) para %s", arg, val, op.Kind)
		}
	}
	return nil
}

// Apply ejecuta una lista de Ops con snapshot, commit y healthcheck.
// Devuelve applied si todo OK, rolled_back si el healthcheck falla, o
// failed si alguna Op falla antes del commit.
func (e *Executor) Apply(ops []Op) ApplyResult {
	start := e.now()

	// 1. Validar todas las Ops antes de tocar nada.
	for _, op := range ops {
		if err := Validate(op); err != nil {
			return ApplyResult{Status: "failed", Op: op.Desc, Error: err.Error(), DurationMs: ms(e.now(), start)}
		}
	}

	// 2. Snapshot de los configs UCI afectados.
	affected := affectedConfigs(ops)
	snapshots := map[string]string{}
	for _, cfg := range affected {
		out, code := e.run.Run("uci", "export", cfg)
		if code == 0 && out != "" {
			snapshots[cfg] = out
		}
	}

	// 3. Ejecutar Ops (staged, sin commit aún).
	for _, op := range ops {
		spec := allowlist[op.Kind]
		var code int
		if spec.exec != nil {
			code = spec.exec(e.run, op.Args)
		} else {
			cmd, cmdArgs := spec.build(op.Args)
			_, code = e.run.Run(cmd, cmdArgs...)
		}
		if code != 0 {
			if op.Kind == "wg_check" || op.Kind == "dawn_check" {
				// Healthcheck del módulo: rollback real restaurando snapshots UCI
				// y relanzando los servicios afectados.
				for cfg, snap := range snapshots {
					e.run.Run("sh", "-c", fmt.Sprintf("echo '%s' | uci import", snap))
					e.run.Run("uci", "commit", cfg)
				}
				if op.Kind == "wg_check" {
					e.run.Run("/etc/init.d/network", "reload")
				}
				if op.Kind == "dawn_check" {
					e.run.Run("/etc/init.d/dawn", "restart")
					e.run.Run("/sbin/wifi", "reload")
				}
				errKey := "wg_check_failed"
				if op.Kind == "dawn_check" {
					errKey = "dawn_check_failed"
				}
				return ApplyResult{Status: "rolled_back", Op: op.Desc, Error: errKey, Snapshot: strings.Join(affected, ","), DurationMs: ms(e.now(), start)}
			}
			// Revert staged changes y salir.
			e.revertStaged(affected)
			return ApplyResult{Status: "failed", Op: op.Desc, Error: fmt.Sprintf("%s exit %d", op.Kind, code), DurationMs: ms(e.now(), start)}
		}
	}

	// 4. Commit de cada config afectado.
	for _, cfg := range affected {
		e.run.Run("uci", "commit", cfg)
	}

	// 5. Healthcheck.
	if !e.healthcheck() {
		// ROLLBACK: restaurar snapshots.
		for cfg, snap := range snapshots {
			e.run.Run("sh", "-c", fmt.Sprintf("echo '%s' | uci import", snap))
			e.run.Run("uci", "commit", cfg)
		}
		return ApplyResult{Status: "rolled_back", Error: "healthcheck_failed", Snapshot: strings.Join(affected, ","), DurationMs: ms(e.now(), start)}
	}

	return ApplyResult{Status: "applied", Snapshot: strings.Join(affected, ","), DurationMs: ms(e.now(), start)}
}

func (e *Executor) revertStaged(configs []string) {
	for _, cfg := range configs {
		e.run.Run("uci", "revert", cfg)
	}
}

func (e *Executor) healthcheck() bool {
	target := e.wanTarget
	if target == "" {
		target = e.gwTarget
	}
	if target == "" {
		return true // sin target configurado: pasar (mejor falso negativo que bloquear)
	}
	_, code := e.run.Run("ping", "-c", "1", "-W", "3", target)
	return code == 0
}

func affectedConfigs(ops []Op) []string {
	seen := map[string]bool{}
	var out []string
	for _, op := range ops {
		spec, ok := allowlist[op.Kind]
		if !ok {
			continue
		}
		for _, cfg := range spec.configs(op.Args) {
			if cfg != "" && !seen[cfg] {
				seen[cfg] = true
				out = append(out, cfg)
			}
		}
	}
	return out
}

func ms(now, start time.Time) int64 {
	return now.Sub(start).Milliseconds()
}

// MarshalOps serializa una lista de Ops a JSON (para enviar vía SSE).
func MarshalOps(ops []Op) (string, error) {
	b, err := json.Marshal(ops)
	return string(b), err
}

// UnmarshalOps deserializa Ops desde JSON (en el agente, al recibir el SSE).
func UnmarshalOps(data string) ([]Op, error) {
	var ops []Op
	err := json.Unmarshal([]byte(data), &ops)
	return ops, err
}
