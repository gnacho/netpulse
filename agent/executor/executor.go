// Package executor — ejecutor sandboxeado de operaciones de escritura en el
// router (Fase 10). El servidor envía una lista de Ops allowlistedas vía SSE;
// el ejecutor las valida, hace snapshot, aplica en staging, commit, healthcheck
// y rollback automático si falla.
//
// Allowlist estricta: cada Kind tiene un patrón regex por arg. Args que no
// casan → rechazo inmediato. Shell libre PROHIBIDO.
package executor

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Op es una operación allowlistedada que el ejecutor puede aplicar.
type Op struct {
	Kind string            `json:"kind"` // uci_set|uci_delete|uci_add_list|uci_commit|service|install
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
	reConfig  = regexp.MustCompile(`^[a-z_]+$`)
	reSection = regexp.MustCompile(`^(@[a-z_]+\[\d+\]|[a-z_]+)$`)
	reOption  = regexp.MustCompile(`^[a-z_]+$`)
	// value: alfanumérico + puntos, dos-puntos, barras, hashes, guiones.
	// Cubre IPs (192.168.1.1), DNS (1.1.1.1#3001), rutas, MACs, puertos.
	reValue      = regexp.MustCompile(`^[a-zA-Z0-9_.:/#,-]+$`)
	reService    = regexp.MustCompile(`^[a-z_-]+$`)
	reServiceAct = regexp.MustCompile(`^(restart|reload|enable|disable)$`)
	rePackage    = regexp.MustCompile(`^[a-z0-9_-]+$`)
)

// opSpec define los args requeridos y su validación para cada Kind.
type opSpec struct {
	required map[string]*regexp.Regexp
	build    func(args map[string]string) (cmd string, cmdArgs []string)
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
}

// Executor aplica Ops allowlistedas con snapshot + healthcheck + rollback.
type Executor struct {
	run        Runner
	now        func() time.Time
	gwTarget   string // IP del gateway para healthcheck (APs)
	wanTarget  string // IP WAN para healthcheck (gateway)
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
		cmd, cmdArgs := spec.build(op.Args)
		_, code := e.run.Run(cmd, cmdArgs...)
		if code != 0 {
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
