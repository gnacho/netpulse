package adapters

import (
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/oui"
)

// ---------------------------------------------------------------------------
// Clasificación de tipo de dispositivo live (2-Ago-2026). El FDB/DHCP no dice
// QUÉ es cada cliente: se estima con reglas deterministas por patrones de
// hostname, huella DHCP (vendor class, client-id), capacidades LLDP y OUI.
// Solo afecta a presentación (icono/grupo); ante la duda, "desconocido":
// nunca afirmar un tipo sin evidencia razonable.
// ---------------------------------------------------------------------------

// typeRule: substrings (minúsculas) del hostname → tipo. El orden importa:
// la primera regla que casa gana (de más específica a más genérica).
var typeRules = []struct {
	typ  string
	subs []string
}{
	// Consola antes que tv/iot: "switch" a secas es switch de red, pero
	// "switch OLED" no aparece en hostnames reales; Nintendo suele anunciarse
	// como "nintendo-switch".
	{"consola", []string{"playstation", "ps4", "ps5", "xbox", "nintendo", "steamdeck", "steam-deck", "consola"}},
	{"tv", []string{"appletv", "apple-tv", "bravia", "webos", "firetv", "fire-tv", "chromecast", "mibox", "mi-box", "shield", "television", "smart-tv", "smarttv", "-tv", "tv-"}},
	{"camara", []string{"camara", "camera", "doorbell", "timbre", "ezviz", "tapo-c", "-cam", "cam-"}},
	{"altavoz", []string{"sonos", "heos", "homepod", "echo-", "altavoz", "speaker", "soundbar", "marantz", "denon", "home-mini", "nest-mini", "nest-audio", "amplificador", "receiver"}},
	{"tablet", []string{"ipad", "tablet", "kindle", "kobo"}},
	{"movil", []string{"iphone", "android", "pixel", "galaxy-s", "galaxy-a", "redmi-note", "oneplus", "xiaomi-1", "mi-1", "phone", "movil"}},
	{"portatil", []string{"macbook", "laptop", "thinkpad", "ideapad", "portatil", "notebook", "surface", "hp-laptop", "lenovo-yoga"}},
	{"servidor", []string{"proxmox", "pve", "jellyfin", "transmission", "helios", "homeassistant", "home-assistant", "haos", "servidor", "server", "nas", "citadel", "omv", "truenas", "pihole", "pi-hole", "adguard", "raspberry", "rpi", "docker", "keynest", "deltos", "nido", "netpulse"}},
	{"ordenador", []string{"imac", "mac-mini", "macstudio", "mac-studio", "desktop", "sobremesa", "workstation", "nuc", "ser9", "pc-", "-pc", "tower", "minipc", "mini-pc"}},
	{"switch", []string{"gs308", "gs305", "tl-sg", "switch"}},
	{"iot", []string{"roomba", "irobot", "roborock", "aspirador", "robot", "tasmota", "sonoff", "shelly", "esphome", "tuya", "smartlife", "meross", "gosund", "switchbot", "aqara", "lumi", "zigbee", "zhirui", "osram", "ikea", "tradfri", "hue", "wled", "athom", "cargador", "wallbox", "feyree", "tedee", "cerradura", "enchufe", "plug", "bombilla", "downlight", "persiana", "curtain", "riego", "sprinkler", "termo", "termostato", "caldera", "aire", "ac-", "slzb", "impresora", "printer", "epson", "brother", "canon"}},
}

// GuessDeviceType estima el DeviceType con reglas deterministas: patrones de
// hostname primero, luego huella DHCP (vendor class, client-id), capacidades
// LLDP y fabricante OUI. Vacío/desconocido → "desconocido".
func GuessDeviceType(hostname, manufacturer, dhcpVendorClass, dhcpClientID, lldpCaps string) string {
	h := strings.ToLower(strings.TrimSpace(hostname))
	m := strings.ToLower(strings.TrimSpace(manufacturer))
	v := strings.ToLower(strings.TrimSpace(dhcpVendorClass))
	c := strings.ToLower(strings.TrimSpace(dhcpClientID))
	caps := strings.ToLower(strings.TrimSpace(lldpCaps))
	// Hostnames basura frecuentes (MAC como nombre, "unknown-…", "android-…"
	// genérico de DHCP) solo aportan si casan con una regla clara.
	for _, r := range typeRules {
		for _, s := range r.subs {
			if h != "" && strings.Contains(h, s) {
				return r.typ
			}
		}
	}
	// Huella DHCP (option 60 vendor class): específica de SO/plataforma.
	if v != "" {
		if strings.Contains(v, "android") {
			return "movil"
		}
		if strings.Contains(v, "msft") || strings.Contains(v, "windows") {
			return "ordenador"
		}
	}
	// Client-id DHCP: a veces lleva el nombre del equipo.
	if c != "" && strings.Contains(c, "raspberry") {
		return "servidor"
	}
	// Capacidades LLDP: bridge a secas = switch de red; wlan = punto de acceso.
	if caps != "" {
		bridge := strings.Contains(caps, "bridge")
		router := strings.Contains(caps, "router")
		wlan := strings.Contains(caps, "wlan")
		station := strings.Contains(caps, "station")
		if bridge && !router && !wlan && !station {
			return "switch"
		}
		if wlan && !station {
			return "switch"
		}
	}
	// Fabricante como refuerzo cuando el hostname no dice nada (OUI DB).
	if oui.IsIoTVendor(m) {
		return "iot"
	}
	return "desconocido"
}
