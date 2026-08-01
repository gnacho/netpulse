package adapters

import "strings"

// ---------------------------------------------------------------------------
// Clasificación de tipo de dispositivo live (2-Ago-2026). El FDB/DHCP no dice
// QUÉ es cada cliente: se estima por patrones de hostname (y en el futuro por
// OUI). Solo afecta a presentación (icono/grupo); ante la duda, "desconocido"
// — nunca afirmar un tipo sin evidencia razonable.
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

// GuessDeviceType estima el DeviceType a partir del hostname (DHCP) y, si se
// conoce, del fabricante OUI. Vacío/desconocido → "desconocido".
func GuessDeviceType(hostname, manufacturer string) string {
	h := strings.ToLower(strings.TrimSpace(hostname))
	m := strings.ToLower(strings.TrimSpace(manufacturer))
	// Hostnames basura frecuentes (MAC como nombre, "unknown-…", "android-…"
	// genérico de DHCP) solo aportan si casan con una regla clara.
	for _, r := range typeRules {
		for _, s := range r.subs {
			if h != "" && strings.Contains(h, s) {
				return r.typ
			}
		}
	}
	// Fabricante como refuerzo cuando el hostname no dice nada.
	ouiIoT := []string{"espressif", "tuya", "shenzhen", "sonoff", "shelly", "tasmota", "meross", "gosund", "lumi", "aqara", "tuya smart"}
	for _, s := range ouiIoT {
		if m != "" && strings.Contains(m, s) {
			return "iot"
		}
	}
	return "desconocido"
}
