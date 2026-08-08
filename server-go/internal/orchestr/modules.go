// modules.go — módulos de orquestación (Fase 10). Cada módulo convierte un
// estado deseado en una lista de Ops allowlistedas.
//
// Por ahora los módulos generan Ops estáticamente (declarar lo que se quiere).
// En el futuro, el diff será dinámico: leer el estado actual del router vía
// SSH y generar solo las Ops necesarias para llegar al estado deseado.
package orchestr

import (
	"encoding/json"

	"github.com/gnacho/netpulse/agent/executor"
)

// AdGuardDesired es el estado deseado para el módulo AdGuard Home.
type AdGuardDesired struct {
	Enabled     bool   `json:"enabled"`
	Port        string `json:"port"`         // puerto de escucha de AdGuard (default "3000")
	UpstreamDNS string `json:"upstreamDns"`  // DNS upstream (default "1.1.1.1")
}

// AdGuardOps genera las Ops para activar/desactivar AdGuard Home en un router.
// Activar: instala el paquete, reenvía DNS de dnsmasq hacia AdGuard y reinicia.
// Desactivar: restaura el DNS upstream de dnsmasq y para AdGuard.
func AdGuardOps(desired AdGuardDesired) []executor.Op {
	if !desired.Enabled {
		return []executor.Op{
			{Kind: "uci_delete", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server"}, Desc: "Remove AdGuard DNS forwarding"},
			{Kind: "uci_delete", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "no_resolv"}, Desc: "Restore /etc/resolv.conf usage"},
			{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}, Desc: "Commit DHCP changes"},
			{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": "restart"}, Desc: "Restart dnsmasq"},
			{Kind: "service", Args: map[string]string{"name": "adguard-home", "action": "disable"}, Desc: "Disable AdGuard Home"},
			{Kind: "service", Args: map[string]string{"name": "adguard-home", "action": "stop"}, Desc: "Stop AdGuard Home"},
		}
	}

	port := desired.Port
	if port == "" {
		port = "3000"
	}
	dns := desired.UpstreamDNS
	if dns == "" {
		dns = "1.1.1.1"
	}

	return []executor.Op{
		{Kind: "install", Args: map[string]string{"package": "adguard-home"}, Desc: "Install AdGuard Home"},
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "no_resolv", "value": "1"}, Desc: "Don't use /etc/resolv.conf"},
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "127.0.0.1#" + port}, Desc: "Forward DNS to AdGuard on port " + port},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}, Desc: "Commit DHCP changes"},
		{Kind: "service", Args: map[string]string{"name": "adguard-home", "action": "enable"}, Desc: "Enable AdGuard Home on boot"},
		{Kind: "service", Args: map[string]string{"name": "adguard-home", "action": "restart"}, Desc: "Start AdGuard Home"},
		{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": "restart"}, Desc: "Restart dnsmasq to apply DNS forwarding"},
	}
}

// ModuleDiff despacha al módulo correcto según el tipo de recurso.
// Devuelve la lista de Ops y el desired serializado.
func ModuleDiff(resource string, desired json.RawMessage) ([]executor.Op, json.RawMessage, error) {
	switch resource {
	case "adguard":
		var d AdGuardDesired
		if err := json.Unmarshal(desired, &d); err != nil {
			return nil, desired, err
		}
		return AdGuardOps(d), desired, nil
	default:
		return nil, desired, &unknownModuleError{resource}
	}
}

type unknownModuleError struct{ resource string }

func (e *unknownModuleError) Error() string { return "módulo desconocido: " + e.resource }
