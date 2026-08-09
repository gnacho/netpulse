// guestwifi.go — módulo de orquestación "WiFi guest aislada" (Fase 17.2).
//
// Crea (o elimina) una red WiFi guest aislada en un router OpenWrt: una
// wifi-iface en modo AP con isolate, una network estática en subred propia
// (192.168.8.0/24) y una zona de firewall que solo permite salir a WAN.
// Todo se hace con Ops allowlistedas del executor (uci_add/uci_set/...).
package orchestr

import (
	"fmt"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
)

// guestSSIDDefault es el SSID por defecto de la red guest.
const guestSSIDDefault = "NetPulse-Guest"

// guestIP / guestMask: subred propia de la guest (no choca con la LAN /24).
const (
	guestIP   = "192.168.8.1"
	guestMask = "255.255.255.0"
)

// GuestWiFiDesired es el estado deseado del módulo.
type GuestWiFiDesired struct {
	Enabled  bool   `json:"enabled"`
	SSID     string `json:"ssid"`
	Password string `json:"password"`
	// Band: "2g" | "5g" | "auto". Vacío/auto → el primer radio disponible.
	Band string `json:"band"`
	// AllowNonGateway: permitir en un router no-gateway (mismo patrón que
	// AdGuard, #120). Default solo gateway.
	AllowNonGateway bool `json:"allowNonGateway,omitempty"`
}

// GuestWiFiScenario es lo que detecta el probe en el router.
type GuestWiFiScenario struct {
	// Radio2G / Radio5G: nombre del radio (radio0, radio1) por banda. Vacío si
	// el router no tiene esa banda.
	Radio2G string
	Radio5G string
	// GuestPresent: ya existe una wifi-iface con el SSID guest.
	GuestPresent bool
	// GuestIfaceIdx: referencia a la wifi-iface guest existente (para disable).
	GuestIfaceIdx string
	// GuestZoneIdx / GuestFwdIdx: secciones firewall guest existentes.
	GuestZoneIdx string
	GuestFwdIdx  string
}

// probeGuestWiFiCmd lee el estado de wireless/network/firewall. Una sola sesión.
const probeGuestWiFiCmd = `echo '===WIRELESS==='
uci show wireless
echo '===NETWORK==='
uci show network
echo '===FIREWALL==='
uci show firewall
echo '===END==='`

// DetectGuestWiFi ejecuta el probe y devuelve el escenario.
func DetectGuestWiFi(run CommandRunner, host string) (GuestWiFiScenario, error) {
	out, err := run.Run(host, probeGuestWiFiCmd, 8*time.Second)
	if err != nil {
		return GuestWiFiScenario{}, err
	}
	return parseGuestWiFi(out), nil
}

// parseGuestWiFi extrae radios, wifi-ifaces y secciones guest del output.
// Función pura (testeable sin SSH).
func parseGuestWiFi(out string) GuestWiFiScenario {
	sc := GuestWiFiScenario{}
	sections := splitSections(out)
	wireless := strings.Join(sections["WIRELESS"], "\n")
	network := strings.Join(sections["NETWORK"], "\n")
	firewall := strings.Join(sections["FIREWALL"], "\n")

	// Radios y su banda + wifi-ifaces en orden de aparición.
	radioBand := map[string]string{}
	var wifiIfaces []string
	for _, line := range strings.Split(wireless, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "wireless.") {
			rest := strings.TrimPrefix(line, "wireless.")
			if strings.Contains(rest, "=wifi-device") {
				name := strings.TrimSuffix(rest, "=wifi-device")
				radioBand[name] = ""
				continue
			}
			if strings.Contains(rest, "=wifi-iface") {
				name := strings.TrimSuffix(rest, "=wifi-iface")
				wifiIfaces = append(wifiIfaces, name)
				continue
			}
			// banda: wireless.radioX.band='2g'
			if dot := strings.Index(rest, "."); dot > 0 {
				sec, field := rest[:dot], rest[dot+1:]
				if _, ok := radioBand[sec]; ok && strings.HasPrefix(field, "band=") {
					radioBand[sec] = strings.Trim(strings.TrimPrefix(field, "band="), "'")
				}
			}
		}
	}
	for name, band := range radioBand {
		switch band {
		case "2g":
			if sc.Radio2G == "" {
				sc.Radio2G = name
			}
		case "5g":
			if sc.Radio5G == "" {
				sc.Radio5G = name
			}
		}
	}

	// ¿Existe ya una wifi-iface guest (por ssid)?
	for i, name := range wifiIfaces {
		for _, line := range strings.Split(wireless, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "wireless."+name+".ssid=") {
				ssid := strings.Trim(strings.TrimPrefix(line, "wireless."+name+".ssid="), "'")
				if ssid == guestSSIDDefault {
					sc.GuestPresent = true
					sc.GuestIfaceIdx = fmt.Sprintf("@wifi-iface[%d]", i)
				}
			}
		}
	}

	// Firewall: zone name=guest y forwarding src=guest.
	sc.GuestZoneIdx = guestFirewallIndex(firewall, "zone", "guest")
	sc.GuestFwdIdx = guestForwardingIndex(firewall)
	_ = network // (la network guest se borra por nombre "guest", sin índice)

	return sc
}

// guestFirewallIndex busca la sección @type[i] cuyo name coincide.
func guestFirewallIndex(firewall, typ, nameWanted string) string {
	prefix := "firewall.@" + typ + "["
	for _, line := range strings.Split(firewall, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) || !strings.Contains(line, ".name='") {
			continue
		}
		rest := strings.TrimPrefix(line, "firewall.@")
		idx := ""
		for _, r := range rest {
			if r == ']' {
				break
			}
			if r >= '0' && r <= '9' {
				idx += string(r)
			}
		}
		name := strings.Trim(strings.TrimPrefix(line, "firewall.@"+typ+"["+idx+"].name="), "'")
		if name == nameWanted {
			return "@" + typ + "[" + idx + "]"
		}
	}
	return ""
}

// guestForwardingIndex busca el forwarding con src=guest.
func guestForwardingIndex(firewall string) string {
	prefix := "firewall.@forwarding["
	for _, line := range strings.Split(firewall, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if strings.Contains(line, "src='guest'") {
			rest := strings.TrimPrefix(line, "firewall.@")
			idx := ""
			for _, r := range rest {
				if r == ']' {
					break
				}
				if r >= '0' && r <= '9' {
					idx += string(r)
				}
			}
			return "@forwarding[" + idx + "]"
		}
	}
	return ""
}

// GuestWiFiOps genera las Ops para activar/desactivar la guest.
func GuestWiFiOps(desired GuestWiFiDesired, sc GuestWiFiScenario) []executor.Op {
	if !desired.Enabled {
		return guestWiFiDisableOps(sc)
	}
	ssid := desired.SSID
	if ssid == "" {
		ssid = guestSSIDDefault
	}
	radio := sc.Radio2G
	switch {
	case desired.Band == "5g" && sc.Radio5G != "":
		radio = sc.Radio5G
	case desired.Band == "2g" && sc.Radio2G != "":
		radio = sc.Radio2G
	case sc.Radio2G == "" && sc.Radio5G != "":
		radio = sc.Radio5G
	}
	password := desired.Password
	if password == "" {
		password = "changeme123"
	}
	ops := []executor.Op{
		{Kind: "uci_add", Args: map[string]string{"config": "wireless", "type": "wifi-iface"}, Desc: "Create guest wifi-iface"},
		{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "@wifi-iface[-1]", "option": "device", "value": radio}, Desc: "Bind guest iface to radio"},
		{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "@wifi-iface[-1]", "option": "mode", "value": "ap"}, Desc: "Guest iface mode AP"},
		{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "@wifi-iface[-1]", "option": "ssid", "value": ssid}, Desc: "Set guest SSID"},
		{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "@wifi-iface[-1]", "option": "encryption", "value": "psk2"}, Desc: "Guest WPA2 encryption"},
		{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "@wifi-iface[-1]", "option": "key", "value": password}, Desc: "Set guest password"},
		{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "@wifi-iface[-1]", "option": "network", "value": "guest"}, Desc: "Bind guest iface to guest network"},
		{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "@wifi-iface[-1]", "option": "isolate", "value": "1"}, Desc: "Isolate guest clients"},
		{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "Commit wireless"},
		{Kind: "uci_set_named", Args: map[string]string{"config": "network", "section": "guest", "type": "interface"}, Desc: "Create guest network interface"},
		{Kind: "uci_set", Args: map[string]string{"config": "network", "section": "guest", "option": "proto", "value": "static"}, Desc: "Guest static IP"},
		{Kind: "uci_set", Args: map[string]string{"config": "network", "section": "guest", "option": "ipaddr", "value": guestIP}, Desc: "Guest gateway IP"},
		{Kind: "uci_set", Args: map[string]string{"config": "network", "section": "guest", "option": "netmask", "value": guestMask}, Desc: "Guest netmask"},
		{Kind: "uci_commit", Args: map[string]string{"config": "network"}, Desc: "Commit network"},
		{Kind: "uci_add", Args: map[string]string{"config": "firewall", "type": "zone"}, Desc: "Create guest firewall zone"},
		{Kind: "uci_set", Args: map[string]string{"config": "firewall", "section": "@zone[-1]", "option": "name", "value": "guest"}, Desc: "Name guest zone"},
		{Kind: "uci_set", Args: map[string]string{"config": "firewall", "section": "@zone[-1]", "option": "network", "value": "guest"}, Desc: "Zone network guest"},
		{Kind: "uci_set", Args: map[string]string{"config": "firewall", "section": "@zone[-1]", "option": "input", "value": "REJECT"}, Desc: "Zone input REJECT"},
		{Kind: "uci_set", Args: map[string]string{"config": "firewall", "section": "@zone[-1]", "option": "output", "value": "ACCEPT"}, Desc: "Zone output ACCEPT"},
		{Kind: "uci_set", Args: map[string]string{"config": "firewall", "section": "@zone[-1]", "option": "forward", "value": "REJECT"}, Desc: "Zone forward REJECT"},
		{Kind: "uci_add", Args: map[string]string{"config": "firewall", "type": "forwarding"}, Desc: "Create guest forwarding"},
		{Kind: "uci_set", Args: map[string]string{"config": "firewall", "section": "@forwarding[-1]", "option": "src", "value": "guest"}, Desc: "Forwarding src guest"},
		{Kind: "uci_set", Args: map[string]string{"config": "firewall", "section": "@forwarding[-1]", "option": "dest", "value": "wan"}, Desc: "Forwarding dest wan"},
		{Kind: "uci_commit", Args: map[string]string{"config": "firewall"}, Desc: "Commit firewall"},
		{Kind: "service", Args: map[string]string{"name": "network", "action": "reload"}, Desc: "Reload network (applies wifi)"},
		{Kind: "service", Args: map[string]string{"name": "firewall", "action": "reload"}, Desc: "Reload firewall"},
	}
	return ops
}

// guestWiFiDisableOps revierte el enable: borra las secciones creadas.
func guestWiFiDisableOps(sc GuestWiFiScenario) []executor.Op {
	ops := []executor.Op{}
	if sc.GuestIfaceIdx != "" {
		ops = append(ops, executor.Op{Kind: "uci_delete_section", Args: map[string]string{"config": "wireless", "section": sc.GuestIfaceIdx}, Desc: "Remove guest wifi-iface"})
	}
	ops = append(ops, executor.Op{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "Commit wireless"})
	ops = append(ops, executor.Op{Kind: "uci_delete_section", Args: map[string]string{"config": "network", "section": "guest"}, Desc: "Remove guest network"})
	ops = append(ops, executor.Op{Kind: "uci_commit", Args: map[string]string{"config": "network"}, Desc: "Commit network"})
	if sc.GuestZoneIdx != "" {
		ops = append(ops, executor.Op{Kind: "uci_delete_section", Args: map[string]string{"config": "firewall", "section": sc.GuestZoneIdx}, Desc: "Remove guest zone"})
	}
	if sc.GuestFwdIdx != "" {
		ops = append(ops, executor.Op{Kind: "uci_delete_section", Args: map[string]string{"config": "firewall", "section": sc.GuestFwdIdx}, Desc: "Remove guest forwarding"})
	}
	ops = append(ops, executor.Op{Kind: "uci_commit", Args: map[string]string{"config": "firewall"}, Desc: "Commit firewall"})
	ops = append(ops, executor.Op{Kind: "service", Args: map[string]string{"name": "network", "action": "reload"}, Desc: "Reload network"})
	ops = append(ops, executor.Op{Kind: "service", Args: map[string]string{"name": "firewall", "action": "reload"}, Desc: "Reload firewall"})
	return ops
}

// validateGuestWiFiOps valida cada op contra el executor (usado en tests).
func validateGuestWiFiOps(ops []executor.Op) error {
	for _, op := range ops {
		if err := executor.Validate(op); err != nil {
			return fmt.Errorf("%s: %w", op.Desc, err)
		}
	}
	return nil
}
