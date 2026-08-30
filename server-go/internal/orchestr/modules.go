// modules.go — módulos de orquestación (Fase 10 + Fase 17.1). Cada módulo
// convierte un estado deseado + un escenario detectado en una lista de Ops
// allowlistedas.
//
// Módulos registrados (dispatch en httpapi/orchestr.go computeModuleDiff):
//   - adguard   → modules.go + adguard_probe.go (Fase 17.1)
//   - guestwifi → guestwifi.go (Fase 17.2)
//   - ddns      → ddns.go (Fase 17.3)
//   - sqm       → sqm.go (Fase 17.4)
//   - wireguard → wireguard.go (Fase 10.3)
//   - dawn      → dawn.go (Fase 17.9)
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

// adGuardDNSPort es el puerto donde AdGuard Home escucha el DNS (dns.port).
// Es fijo y separado del puerto de la UI (bind_port = AdGuardDesired.Port) para
// que dnsmasq reenvíe al resolutor y no a la UI web (issue #269). Elegido fuera
// del puerto 53 para no colisionar con dnsmasq, que sigue sirviendo la LAN en :53.
const adGuardDNSPort = "5353"

// ErrManagedByFirmware indica que el router trae un fork de AdGuard del
// fabricante (p. ej. GL.iNet gl-sdk4-adguardhome). El handler lo traduce a
// HTTP 422 para que el usuario lo vea en el dry-run antes de aplicar nada.
var ErrManagedByFirmware = errors.New("managed_by_firmware")

// AdGuardDesired es el estado deseado para el módulo AdGuard Home.
type AdGuardDesired struct {
	Enabled     bool   `json:"enabled"`
	Port        string `json:"port"`        // puerto HTTP de la UI de AdGuard (bind_port; default "3000")
	UpstreamDNS string `json:"upstreamDns"` // DNS upstream (default "1.1.1.1")
	// AllowNonGateway: permitir AdGuard en un router que no es el gateway.
	// El servidor lo exige para no rechazar con 422 adguard_gateway_only
	// (issue #120). El frontend lo expone como toggle "advanced".
	AllowNonGateway bool `json:"allowNonGateway,omitempty"`
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
// Configura el DNS forwarding de dnsmasq hacia el puerto DNS de AdGuard (fijo,
// adGuardDNSPort) y arranca el servicio procd. tcp_check al puerto de la UI
// (bind_port) verifica que el servicio levantó de verdad (si no, el executor
// revierte staged). El puerto port es SOLO de la UI, no del resolver (issue #269).
func adGuardConfigOps(port, dns string) []executor.Op {
	_ = dns // el upstream lo fija la config de AdGuard en write_file, no dnsmasq
	return []executor.Op{
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "no_resolv", "value": "1"}, Desc: "Don't use /etc/resolv.conf"},
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "127.0.0.1#" + adGuardDNSPort}, Desc: "Forward DNS to AdGuard on port " + adGuardDNSPort},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}, Desc: "Commit DHCP changes"},
		{Kind: "service", Args: map[string]string{"name": "adguardhome", "action": "enable"}, Desc: "Enable AdGuard Home on boot"},
		{Kind: "service", Args: map[string]string{"name": "adguardhome", "action": "start"}, Desc: "Start AdGuard Home"},
		{Kind: "tcp_check", Args: map[string]string{"host": "127.0.0.1", "port": port}, Desc: "Check AdGuard Home UI is listening"},
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
// Plan (14 ops): download → extract → mv binario → chmod → write config →
// write init.d → chmod init.d → enable + start service → tcp_check UI →
// DNS forward + commit + dnsmasq restart.
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
		{Kind: "tcp_check", Args: map[string]string{"host": "127.0.0.1", "port": port}, Desc: "Check AdGuard Home UI is listening"},
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "no_resolv", "value": "1"}, Desc: "Don't use /etc/resolv.conf"},
		{Kind: "uci_set", Args: map[string]string{"config": "dhcp", "section": "@dnsmasq[0]", "option": "server", "value": "127.0.0.1#" + adGuardDNSPort}, Desc: "Forward DNS to AdGuard on port " + adGuardDNSPort},
		{Kind: "uci_commit", Args: map[string]string{"config": "dhcp"}, Desc: "Commit DHCP changes"},
		{Kind: "service", Args: map[string]string{"name": "dnsmasq", "action": "restart"}, Desc: "Restart dnsmasq to apply DNS forwarding"},
	}
}

// adGuardConfigTemplate: config YAML mínimo de AdGuard Home para primer
// arranque sin wizard. %s = bind_port (HTTP admin UI), %s = upstream DNS.
// El DNS escucha en 0.0.0.0:<adGuardDNSPort> (fijo, separado de la UI);
// dnsmasq sigue en :53 sirviendo la LAN y reenvía a AdGuard en ese puerto.
const adGuardConfigTemplate = `bind_port: %s
users: []
dns:
  bind_hosts:
    - 0.0.0.0
  port: ` + adGuardDNSPort + `
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
