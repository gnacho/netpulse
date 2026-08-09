// modules.go — módulos de orquestación (Fase 10 + Fase 17.1). Cada módulo
// convierte un estado deseado + un escenario detectado en una lista de Ops
// allowlistedas.
//
// Fase 17.1: AdGuardOps usa el AdGuardScenario (probe SSH del servidor) para
// elegir entre 4 métodos deterministas: apk | opkg | none (binario ya
// presente) | binary (download oficial de GitHub). Aborta con
// ErrManagedByFirmware si el router trae un fork de fabricante (GL.iNet).
package orchestr

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/gnacho/netpulse/agent/executor"
)

// adguardVersion es la versión del binario oficial a descargar en el escenario
// "binary". Actualizar cuando se quiera ofrecer una release más reciente.
const adguardVersion = "v0.107.62"

// ErrManagedByFirmware indica que el router trae un fork de AdGuard del
// fabricante (p. ej. GL.iNet gl-sdk4-adguardhome). El handler lo traduce a
// HTTP 422 para que el usuario lo vea en el dry-run antes de aplicar nada.
var ErrManagedByFirmware = errors.New("managed_by_firmware")

// AdGuardDesired es el estado deseado para el módulo AdGuard Home.
type AdGuardDesired struct {
	Enabled     bool   `json:"enabled"`
	Port        string `json:"port"`        // puerto HTTP de la UI de AdGuard (default "3000")
	UpstreamDNS string `json:"upstreamDns"` // DNS upstream (default "1.1.1.1")
}

// AdGuardOps genera las Ops para activar/desactivar AdGuard Home según el
// escenario detectado por el probe. Devuelve ErrManagedByFirmware si el router
// está gestionado por firmware de fabricante (no se toca).
func AdGuardOps(desired AdGuardDesired, sc AdGuardScenario) ([]executor.Op, error) {
	if sc.ManagedByFirmware {
		return nil, ErrManagedByFirmware
	}
	if !desired.Enabled {
		return adGuardDisableOps(), nil
	}
	port := desired.Port
	if port == "" {
		port = "3000"
	}
	dns := desired.UpstreamDNS
	if dns == "" {
		dns = "1.1.1.1"
	}
	switch sc.InstallMethod() {
	case "none":
		// Binario oficial ya presente: solo configurar DNS + arrancar.
		return adGuardConfigOps(port, dns), nil
	case "apk":
		install := executor.Op{Kind: "apk_install", Args: map[string]string{"package": "adguard-home"}, Desc: "Install AdGuard Home (apk)"}
		return append([]executor.Op{install}, adGuardConfigOps(port, dns)...), nil
	case "opkg":
		install := executor.Op{Kind: "install", Args: map[string]string{"package": "adguard-home"}, Desc: "Install AdGuard Home (opkg)"}
		return append([]executor.Op{install}, adGuardConfigOps(port, dns)...), nil
	default: // "binary": download oficial de GitHub.
		return adGuardBinaryOps(sc, port, dns), nil
	}
}

// adGuardConfigOps: asume AdGuard ya instalado (opkg/apk/binario presente).
// Solo configura el DNS forwarding de dnsmasq y arranca el servicio procd.
func adGuardConfigOps(port, dns string) []executor.Op {
	return []executor.Op{
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "no_resolv", "value": "1"}, Desc: "Don't use /etc/resolv.conf"},
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "127.0.0.1#" + port}, Desc: "Forward DNS to AdGuard on port " + port},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}, Desc: "Commit DHCP changes"},
		{Kind: "service", Args: map[string]string{"name": "adguardhome", "action": "enable"}, Desc: "Enable AdGuard Home on boot"},
		{Kind: "service", Args: map[string]string{"name": "adguardhome", "action": "start"}, Desc: "Start AdGuard Home"},
		{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": "restart"}, Desc: "Restart dnsmasq to apply DNS forwarding"},
	}
}

// adGuardDisableOps: restaura el DNS de dnsmasq y para/deshabilita AdGuard.
// No desinstala el paquete ni borra el binario (idempotente para re-activar).
func adGuardDisableOps() []executor.Op {
	return []executor.Op{
		{Kind: "uci_delete", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server"}, Desc: "Remove AdGuard DNS forwarding"},
		{Kind: "uci_delete", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "no_resolv"}, Desc: "Restore /etc/resolv.conf usage"},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}, Desc: "Commit DHCP changes"},
		{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": "restart"}, Desc: "Restart dnsmasq"},
		{Kind: "service", Args: map[string]string{"name": "adguardhome", "action": "disable"}, Desc: "Disable AdGuard Home on boot"},
		{Kind: "service", Args: map[string]string{"name": "adguardhome", "action": "stop"}, Desc: "Stop AdGuard Home"},
	}
}

// adGuardBinaryOps: escenario "binary" (download oficial de GitHub + init procd).
// Plan (13 ops): download → extract → mv binario → chmod → write config →
// write init.d → chmod init.d → enable + start service → DNS forward + commit
// + dnsmasq restart.
//
// La estructura del tarball oficial es `AdGuardHome/AdGuardHome` (binario) +
// README/LICENSE/homepage; por eso se extrae a /tmp y se mueve solo el binario.
func adGuardBinaryOps(sc AdGuardScenario, port, dns string) []executor.Op {
	suffix := sc.AdguardSuffix
	if suffix == "" {
		suffix = "arm64" // fallback razonable (la mayoría de routers OpenWrt modernos)
	}
	url := fmt.Sprintf("https://github.com/AdguardTeam/AdGuardHome/releases/download/%s/AdGuardHome_linux_%s.tar.gz", adguardVersion, suffix)
	config := fmt.Sprintf(adGuardConfigTemplate, port, dns)
	return []executor.Op{
		{Kind: "download", Args: map[string]string{"url": url, "dest": "/tmp/AdGuardHome.tar.gz"}, Desc: "Download AdGuard Home " + adguardVersion + " (" + suffix + ")"},
		{Kind: "extract_tarball", Args: map[string]string{"src": "/tmp/AdGuardHome.tar.gz", "dest": "/tmp/agh-inst"}, Desc: "Extract tarball to /tmp/agh-inst"},
		{Kind: "mv", Args: map[string]string{"src": "/tmp/agh-inst/AdGuardHome/AdGuardHome", "dest": "/usr/bin/AdGuardHome"}, Desc: "Install AdGuardHome binary to /usr/bin"},
		{Kind: "chmod", Args: map[string]string{"mode": "0755", "path": "/usr/bin/AdGuardHome"}, Desc: "Make binary executable"},
		{Kind: "write_file", Args: map[string]string{"path": "/etc/AdGuardHome/AdGuardHome.yaml", "content_b64": base64.StdEncoding.EncodeToString([]byte(config))}, Desc: "Write AdGuardHome config"},
		{Kind: "write_file", Args: map[string]string{"path": "/etc/init.d/adguardhome", "content_b64": base64.StdEncoding.EncodeToString([]byte(adGuardInitScript))}, Desc: "Write procd init script"},
		{Kind: "chmod", Args: map[string]string{"mode": "0755", "path": "/etc/init.d/adguardhome"}, Desc: "Make init script executable"},
		{Kind: "service", Args: map[string]string{"name": "adguardhome", "action": "enable"}, Desc: "Enable AdGuard Home on boot"},
		{Kind: "service", Args: map[string]string{"name": "adguardhome", "action": "start"}, Desc: "Start AdGuard Home"},
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "no_resolv", "value": "1"}, Desc: "Don't use /etc/resolv.conf"},
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "127.0.0.1#" + port}, Desc: "Forward DNS to AdGuard on port " + port},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}, Desc: "Commit DHCP changes"},
		{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": "restart"}, Desc: "Restart dnsmasq to apply DNS forwarding"},
	}
}

// adGuardConfigTemplate: config YAML mínimo de AdGuard Home para primer
// arranque sin wizard. %s = bind_port (HTTP admin UI), %s = upstream DNS.
// DNS escucha en 0.0.0.0:53 (dnsmasq ya no resuelve, forwardea a AdGuard).
const adGuardConfigTemplate = `bind_port: %s
users: []
dns:
  bind_hosts:
    - 0.0.0.0
  port: 53
  upstream_dns:
    - %s
  protection_enabled: true
  filtering_enabled: true
`

// adGuardInitScript: servicio procd de OpenWrt para AdGuard Home. respawn
// automático si cae; logs a stdout/stderr (logread).
const adGuardInitScript = `#!/bin/sh /etc/rc.common
START=99
USE_PROCD=1
start_service() {
    procd_open_instance
    procd_set_param command /usr/bin/AdGuardHome -c /etc/AdGuardHome/AdGuardHome.yaml
    procd_set_param respawn
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}
`
