// ddns.go — módulo de orquestación "DDNS" (Fase 17.3).
//
// Instala ddns-scripts (apk/opkg según el router) y configura el servicio de
// DNS dinámico (service_name, dominio, usuario/contraseña) en /etc/config/ddns,
// luego arranca el servicio ddns. Reutiliza los Kinds de 17.1 (apk_install /
// install para opkg) y el patrón de secciones del 17.2.
package orchestr

import (
	"fmt"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
)

// ddnsSection es la sección UCI del servicio DDNS que gestiona este módulo.
// El probe detecta si ya existe una sección activa; si no, se usa esta.
const ddnsSection = "myddns"

// DdnsDesired es el estado deseado del módulo DDNS.
type DdnsDesired struct {
	Enabled     bool   `json:"enabled"`
	ServiceName string `json:"serviceName"` // proveedor (duckdns.org, dyndns.org...)
	Domain      string `json:"domain"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	// AllowNonGateway: gateway-only por defecto (patrón #120).
	AllowNonGateway bool `json:"allowNonGateway,omitempty"`
}

// DdnsScenario describe el estado del DDNS en el router.
type DdnsScenario struct {
	// Manager: "apk" | "opkg" (gestor de paquetes del router).
	Manager string
	// Installed: /etc/init.d/ddns existe (ddns-scripts instalado).
	Installed bool
	// ActiveSection: hay una sección ddns con enabled='1'.
	ActiveSection bool
	// SectionExists: existe la sección ddnsSection en uci.
	SectionExists bool
}

// probeDdnsCmd detecta gestor, instalación y sección activa.
const probeDdnsCmd = `echo '===PKG_MGR==='
[ -x /usr/bin/apk ] && echo apk || echo opkg
echo '===INSTALLED==='
[ -x /etc/init.d/ddns ] && echo yes || echo no
echo '===DDNS_UCI==='
uci show ddns 2>/dev/null
echo '===END==='`

// DetectDdns ejecuta el probe y devuelve el escenario.
func DetectDdns(run CommandRunner, host string) (DdnsScenario, error) {
	out, err := run.Run(host, probeDdnsCmd, 8*time.Second)
	if err != nil {
		return DdnsScenario{}, err
	}
	return parseDdns(out), nil
}

// parseDdns extrae el escenario del output del probe. Función pura.
func parseDdns(out string) DdnsScenario {
	sc := DdnsScenario{}
	sections := splitSections(out)
	sc.Manager = strings.TrimSpace(firstNonEmpty(sections["PKG_MGR"]))
	sc.Installed = strings.TrimSpace(firstNonEmpty(sections["INSTALLED"])) == "yes"
	uci := strings.Join(sections["DDNS_UCI"], "\n")
	sc.SectionExists = strings.Contains(uci, "ddns."+ddnsSection+"=service")
	if strings.Contains(uci, "ddns."+ddnsSection+".enabled='1'") {
		sc.ActiveSection = true
	}
	return sc
}

// DdnsOps genera las Ops para activar/desactivar DDNS.
func DdnsOps(desired DdnsDesired, sc DdnsScenario) []executor.Op {
	if !desired.Enabled {
		return ddnsDisableOps(sc)
	}
	// Si no hay sección, crearla (uci_set_named → sección nombrada).
	var ops []executor.Op
	if !sc.Installed {
		if sc.Manager == "apk" {
			ops = append(ops, executor.Op{Kind: "apk_install", Args: map[string]string{"package": "ddns-scripts"}, Desc: "Install ddns-scripts (apk)"})
		} else {
			ops = append(ops, executor.Op{Kind: "install", Args: map[string]string{"package": "ddns-scripts"}, Desc: "Install ddns-scripts (opkg)"})
		}
	}
	// Configurar la sección service.
	if !sc.SectionExists {
		ops = append(ops, executor.Op{Kind: "uci_set_named", Args: map[string]string{"config": "ddns", "section": ddnsSection, "type": "service"}, Desc: "Create ddns service section"})
	}
	svcName := desired.ServiceName
	if svcName == "" {
		svcName = "duckdns.org"
	}
	ops = append(ops,
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "ddns", "section": ddnsSection, "option": "enabled", "value": "1"}, Desc: "Enable ddns service"},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "ddns", "section": ddnsSection, "option": "service_name", "value": svcName}, Desc: "Set ddns provider"},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "ddns", "section": ddnsSection, "option": "domain", "value": desired.Domain}, Desc: "Set ddns domain"},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "ddns", "section": ddnsSection, "option": "username", "value": desired.Username}, Desc: "Set ddns username"},
		executor.Op{Kind: "uci_set", Args: map[string]string{"config": "ddns", "section": ddnsSection, "option": "password", "value": desired.Password}, Desc: "Set ddns password"},
		executor.Op{Kind: "uci_commit", Args: map[string]string{"config": "ddns"}, Desc: "Commit ddns config"},
		executor.Op{Kind: "service", Args: map[string]string{"name": "ddns", "action": "enable"}, Desc: "Enable ddns on boot"},
		executor.Op{Kind: "service", Args: map[string]string{"name": "ddns", "action": "start"}, Desc: "Start ddns service"},
	)
	return ops
}

// ddnsDisableOps desactiva el servicio sin desinstalar el paquete.
func ddnsDisableOps(sc DdnsScenario) []executor.Op {
	if !sc.SectionExists && !sc.Installed {
		// No hay nada que desactivar.
		return []executor.Op{}
	}
	return []executor.Op{
		{Kind: "uci_set", Args: map[string]string{"config": "ddns", "section": ddnsSection, "option": "enabled", "value": "0"}, Desc: "Disable ddns service"},
		{Kind: "uci_commit", Args: map[string]string{"config": "ddns"}, Desc: "Commit ddns config"},
		{Kind: "service", Args: map[string]string{"name": "ddns", "action": "stop"}, Desc: "Stop ddns service"},
		{Kind: "service", Args: map[string]string{"name": "ddns", "action": "disable"}, Desc: "Disable ddns on boot"},
	}
}

// validateDdnsOps valida cada op contra el executor (usado en tests).
func validateDdnsOps(ops []executor.Op) error {
	for _, op := range ops {
		if err := executor.Validate(op); err != nil {
			return fmt.Errorf("%s: %w", op.Desc, err)
		}
	}
	return nil
}
