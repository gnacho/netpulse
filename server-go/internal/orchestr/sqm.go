// sqm.go — módulo de orquestación "QoS / SQM" (Fase 17.4).
//
// Instala sqm-scripts (apk/opkg según el router) y configura Smart Queue
// Management (SQM) en la interfaz elegida: sección UCI `queue` con download,
// upload, qdisc y script, y arranca el servicio `sqm`. Reutiliza los Kinds de
// 17.1 (apk_install / install) y el patrón de índices @queue[n] del 17.2.
package orchestr

import (
	"fmt"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
)

// sqmDefaults: valores por defecto razonables para OpenWrt cuando el desired
// no los trae (mismos que la UI LuCI de sqm-scripts).
const (
	sqmDefaultQdisc    = "cake"
	sqmDefaultScript   = "piece_of_cake.qos"
	sqmDefaultLink     = "ethernet"
	sqmDefaultOverhead = "44"
)

// SqmDesired es el estado deseado del módulo QoS.
type SqmDesired struct {
	Enabled bool `json:"enabled"`
	// Interface: interfaz WAN/LAN a gestionar (p. ej. "eth1"). Obligatoria.
	Interface string `json:"interface"`
	// Download / Upload: ancho de banda en kbit/s (p. ej. "85000").
	Download string `json:"download"`
	Upload   string `json:"upload"`
	// Qdisc / Script: disciplinas y script de SQM (defaults LuCI).
	Qdisc     string `json:"qdisc"`
	Script    string `json:"script"`
	Linklayer string `json:"linklayer"`
	Overhead  string `json:"overhead"`
	// AllowNonGateway: gateway-only por defecto (patrón #120).
	AllowNonGateway bool `json:"allowNonGateway,omitempty"`
}

// SqmSection es una sección `queue` del config sqm.
type SqmSection struct {
	// Idx: referencia UCI "@queue[n]" (o nombre si la sección es nombrada).
	Idx string
	// Interface: valor de option interface.
	Interface string
	// Enabled: option enabled == '1'.
	Enabled bool
}

// SqmScenario describe el estado del SQM en el router.
type SqmScenario struct {
	// Manager: "apk" | "opkg" (gestor de paquetes del router).
	Manager string
	// Installed: /etc/init.d/sqm existe (sqm-scripts instalado).
	Installed bool
	// Sections: secciones queue existentes en uci.
	Sections []SqmSection
}

// probeSqmCmd detecta gestor, instalación y secciones sqm.
const probeSqmCmd = `echo '===PKG_MGR==='
[ -x /usr/bin/apk ] && echo apk || echo opkg
echo '===INSTALLED==='
[ -x /etc/init.d/sqm ] && echo yes || echo no
echo '===SQM_UCI==='
uci show sqm 2>/dev/null
echo '===END==='`

// DetectSqm ejecuta el probe y devuelve el escenario.
func DetectSqm(run CommandRunner, host string) (SqmScenario, error) {
	out, err := run.Run(host, probeSqmCmd, 8*time.Second)
	if err != nil {
		return SqmScenario{}, err
	}
	return parseSqm(out), nil
}

// parseSqm extrae el escenario del output del probe. Función pura.
//
// Formato de `uci show sqm`:
//
//	sqm.@queue[0]=queue
//	sqm.@queue[0].interface='eth1'
//	sqm.@queue[0].enabled='0'
//	sqm.@queue[0].download='85000'
//	...
func parseSqm(out string) SqmScenario {
	sc := SqmScenario{}
	sections := splitSections(out)
	sc.Manager = strings.TrimSpace(firstNonEmpty(sections["PKG_MGR"]))
	sc.Installed = strings.TrimSpace(firstNonEmpty(sections["INSTALLED"])) == "yes"
	uci := strings.Join(sections["SQM_UCI"], "\n")

	// Secciones: líneas "sqm.@queue[n]=queue" o "sqm.<nombre>=queue".
	for _, line := range strings.Split(uci, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "sqm.") || !strings.HasSuffix(line, "=queue") {
			continue
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(line, "sqm."), "=queue")
		sec := SqmSection{Idx: raw}
		// Leer option interface y enabled de la sección.
		if v := sqmOptionValue(uci, raw, "interface"); v != "" {
			sec.Interface = v
		}
		if v := sqmOptionValue(uci, raw, "enabled"); v == "1" {
			sec.Enabled = true
		}
		sc.Sections = append(sc.Sections, sec)
	}
	return sc
}

// sqmOptionValue lee el valor de una option de una sección en el output de
// `uci show sqm` ("sqm.@queue[0].interface='eth1'" → "eth1").
func sqmOptionValue(uci, section, option string) string {
	prefix := "sqm." + section + "." + option + "="
	for _, line := range strings.Split(uci, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimPrefix(line, prefix), "'")
		}
	}
	return ""
}

// SqmOps genera las Ops para activar/desactivar SQM.
func SqmOps(desired SqmDesired, sc SqmScenario) []executor.Op {
	if !desired.Enabled {
		return sqmDisableOps(desired, sc)
	}
	iface := desired.Interface
	if iface == "" {
		if len(sc.Sections) > 0 {
			iface = sc.Sections[0].Interface
		}
	}
	if iface == "" {
		iface = "eth1" // fallback razonable (la mayoría de routers OpenWrt)
	}
	// Resolver la sección a gestionar: la que ya tenga esa interfaz, o la
	// primera existente, o crear una nueva (@queue[-1]).
	idx := sqmSectionFor(sc, iface)
	var ops []executor.Op
	if !sc.Installed {
		if sc.Manager == "apk" {
			ops = append(ops, executor.Op{Kind: "apk_install", Args: map[string]string{"package": "sqm-scripts"}, Desc: "Install sqm-scripts (apk)"})
		} else {
			ops = append(ops, executor.Op{Kind: "install", Args: map[string]string{"package": "sqm-scripts"}, Desc: "Install sqm-scripts (opkg)"})
		}
	}
	if idx == "" {
		ops = append(ops, executor.Op{Kind: "uci_add", Args: map[string]string{"config": "sqm", "type": "queue"}, Desc: "Create sqm queue section"})
		idx = "@queue[-1]"
	}
	ops = append(ops,
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "sqm", "section": idx, "option": "enabled", "value": "1"}, Desc: "Enable SQM"},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "sqm", "section": idx, "option": "interface", "value": iface}, Desc: "Set SQM interface " + iface},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "sqm", "section": idx, "option": "download", "value": sqmOrDefault(desired.Download, "100000")}, Desc: "Set download bandwidth"},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "sqm", "section": idx, "option": "upload", "value": sqmOrDefault(desired.Upload, "20000")}, Desc: "Set upload bandwidth"},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "sqm", "section": idx, "option": "qdisc", "value": sqmOrDefault(desired.Qdisc, sqmDefaultQdisc)}, Desc: "Set SQM qdisc"},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "sqm", "section": idx, "option": "script", "value": sqmOrDefault(desired.Script, sqmDefaultScript)}, Desc: "Set SQM script"},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "sqm", "section": idx, "option": "linklayer", "value": sqmOrDefault(desired.Linklayer, sqmDefaultLink)}, Desc: "Set SQM link layer"},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "sqm", "section": idx, "option": "overhead", "value": sqmOrDefault(desired.Overhead, sqmDefaultOverhead)}, Desc: "Set SQM overhead"},
		executor.Op{Kind: "uci_commit", Args: map[string]string{"config": "sqm"}, Desc: "Commit sqm config"},
		executor.Op{Kind: "service", Args: map[string]string{"name": "sqm", "action": "enable"}, Desc: "Enable sqm on boot"},
		executor.Op{Kind: "service", Args: map[string]string{"name": "sqm", "action": "start"}, Desc: "Start sqm service"},
	)
	return ops
}

// sqmDisableOps desactiva SQM sin desinstalar el paquete.
func sqmDisableOps(desired SqmDesired, sc SqmScenario) []executor.Op {
	iface := desired.Interface
	if iface == "" && len(sc.Sections) > 0 {
		iface = sc.Sections[0].Interface
	}
	idx := sqmSectionFor(sc, iface)
	if idx == "" {
		// No hay sección que desactivar.
		return []executor.Op{}
	}
	return []executor.Op{
		{Kind: "uci_set", Args: map[string]string{"config": "sqm", "section": idx, "option": "enabled", "value": "0"}, Desc: "Disable SQM"},
		{Kind: "uci_commit", Args: map[string]string{"config": "sqm"}, Desc: "Commit sqm config"},
		{Kind: "service", Args: map[string]string{"name": "sqm", "action": "stop"}, Desc: "Stop sqm service"},
		{Kind: "service", Args: map[string]string{"name": "sqm", "action": "disable"}, Desc: "Disable sqm on boot"},
	}
}

// sqmSectionFor devuelve la referencia UCI de la sección a gestionar: la que
// ya tiene la interfaz deseada, o la primera existente ("" si no hay ninguna).
func sqmSectionFor(sc SqmScenario, iface string) string {
	if iface != "" {
		for _, s := range sc.Sections {
			if s.Interface == iface {
				return s.Idx
			}
		}
	}
	if len(sc.Sections) > 0 {
		return sc.Sections[0].Idx
	}
	return ""
}

// sqmOrDefault devuelve val si no está vacío, si no fallback.
func sqmOrDefault(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

// validateSqmOps valida cada op contra el executor (usado en tests).
func validateSqmOps(ops []executor.Op) error {
	for _, op := range ops {
		if err := executor.Validate(op); err != nil {
			return fmt.Errorf("%s: %w", op.Desc, err)
		}
	}
	return nil
}
