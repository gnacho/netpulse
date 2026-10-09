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
	// Videoportero antes que camara: "doorbell"/"timbre" son cámaras para
	// el resto de reglas, pero el usuario distingue el timbre con cámara
	// (#1327). Aspirador antes que iot: los nombres de robots aspiradores
	// son muy reconocibles y el cubo genérico los esconde.
	{"videoportero", []string{"videoportero", "doorbell", "timbre", "portero", "videocitofono", "citofono", "chime"}},
	// Consola antes que tv/iot: "switch" a secas es switch de red, pero
	// "switch OLED" no aparece en hostnames reales; Nintendo suele anunciarse
	// como "nintendo-switch".
	{"consola", []string{"playstation", "ps4", "ps5", "xbox", "nintendo", "steamdeck", "steam-deck", "consola"}},
	// Camara antes que tv: "cctv-..." contiene "tv-" y caeria en TV (#1327).
	{"camara", []string{"camara", "camera", "cctv", "nvr", "reolink", "ezviz", "tapo-c", "-cam", "cam-"}},
	{"tv", []string{"appletv", "apple-tv", "bravia", "webos", "firetv", "fire-tv", "chromecast", "mibox", "mi-box", "shield", "television", "smart-tv", "smarttv", "-tv", "tv-"}},
	{"aspirador", []string{"roomba", "irobot", "roborock", "aspirador", "vacuum", "deebot", "ecovacs", "dreame", "qrevo", "s8-max", "s8-pro", "k10"}},
	// Caldera: calefaccion/boiler/radiador eran 'desconocido' o caian mal;
	// #1340 les da tipo propio. ADITIVO: "caldera" se QUEDA en iot (regla
	// antigua), no se roba: una caldera ya clasificada no cambia de icono.
	{"caldera", []string{"calefaccion", "calefacción", "boiler", "heater", "heating", "radiador"}},
	{"altavoz", []string{"sonos", "heos", "homepod", "echo-", "altavoz", "speaker", "soundbar", "marantz", "denon", "home-mini", "nest-mini", "nest-audio", "amplificador", "receiver"}},
	{"tablet", []string{"ipad", "tablet", "kindle", "kobo"}},
	{"movil", []string{"iphone", "android", "pixel", "galaxy-s", "galaxy-a", "redmi-note", "oneplus", "xiaomi-1", "mi-1", "phone", "movil"}},
	{"portatil", []string{"macbook", "laptop", "thinkpad", "ideapad", "portatil", "notebook", "surface", "hp-laptop", "lenovo-yoga"}},
	// Placa: SBC no cubiertas antes (orange/nano/banana/rock pi). ADITIVO:
	// raspberry/rpi/pihole se QUEDAN en servidor (regla antigua): un RPi ya
	// clasificado no cambia de icono. "-sbc"/"sbc-" con guion para no casar
	// dentro de otra palabra.
	{"placa", []string{"orangepi", "orange-pi", "nanopi", "nano-pi", "bananapi", "banana-pi", "rockpi", "rock-pi", "-sbc", "sbc-"}},
	{"servidor", []string{"proxmox", "pve", "jellyfin", "transmission", "helios", "homeassistant", "home-assistant", "haos", "servidor", "server", "nas", "citadel", "omv", "truenas", "pihole", "pi-hole", "adguard", "raspberry", "rpi", "docker", "keynest", "deltos", "nido", "netpulse"}},
	{"ordenador", []string{"imac", "mac-mini", "macstudio", "mac-studio", "desktop", "sobremesa", "workstation", "nuc", "ser9", "pc-", "-pc", "tower", "minipc", "mini-pc"}},
	// Clima: nombre explicito del aire acondicionado (#1340). ADITIVO:
	// "aire"/"ac-" se QUEDAN en iot (regla antigua): un dispositivo ya
	// clasificado no cambia de icono. Los tokens cortos van con guion
	// ("-split"/"split-") igual que "-tv"/"tv-": "split" a secas comeria
	// "band-splitter" o similar.
	{"clima", []string{"aire-acondicionado", "air-conditioner", "aireacondicionado", "minisplit", "-split", "split-", "clima"}},
	{"switch", []string{"gs308", "gs305", "tl-sg", "switch"}},
	{"iot", []string{"robot", "tasmota", "sonoff", "shelly", "esphome", "tuya", "smartlife", "meross", "gosund", "switchbot", "aqara", "lumi", "zigbee", "zhirui", "osram", "ikea", "tradfri", "hue", "wled", "athom", "cargador", "wallbox", "feyree", "tedee", "cerradura", "enchufe", "plug", "bombilla", "downlight", "persiana", "curtain", "riego", "sprinkler", "termo", "termostato", "caldera", "aire", "ac-", "slzb", "impresora", "printer", "epson", "brother", "canon",
		// Xiaomi vende móviles Y electrodomésticos bajo el mismo OUI, así
		// que el fabricante no decide: el nombre sí.
		"purificator", "purificador", "purifier", "humidifier", "humidificador"}},
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
	// Cámaras primero: un fabricante de videovigilancia es más específico
	// que "iot", y hay un tipo propio para ello.
	if oui.IsCameraVendor(m) {
		return "camara"
	}
	if oui.IsIoTVendor(m) {
		return "iot"
	}
	return "desconocido"
}

// ValidDeviceTypes es el conjunto cerrado de tipos que GuessDeviceType puede
// devolver (y las claves devices.types.* del frontend). El override manual de
// tipo (#797) valida contra esta lista.
var ValidDeviceTypes = map[string]bool{
	"consola": true, "tv": true, "camara": true, "altavoz": true,
	"tablet": true, "movil": true, "portatil": true, "servidor": true,
	"ordenador": true, "switch": true, "iot": true, "desconocido": true,
	"aspirador": true, "videoportero": true,
	"clima": true, "caldera": true, "placa": true,
}

// guessFromMdns (#338): classify a device from its mDNS service types when
// hostname/DHCP/LLDP classification yielded "desconocido". The mapping is
// intentionally conservative: only well-known service types that strongly
// indicate a device category.
func guessFromMdns(services []string) string {
	for _, svc := range services {
		s := strings.ToLower(svc)
		switch {
		case strings.Contains(s, "_airplay") || strings.Contains(s, "_raop"):
			return "altavoz"
		case strings.Contains(s, "_googlecast") || strings.Contains(s, "_googlezone"):
			return "altavoz"
		case strings.Contains(s, "_appletv") || strings.Contains(s, "_mediaremote"):
			return "tv"
		case strings.Contains(s, "_ipp") || strings.Contains(s, "_printer") || strings.Contains(s, "_pdl-datastream"):
			return "iot" // printer
		case strings.Contains(s, "_hap") || strings.Contains(s, "_homekit"):
			return "iot"
		case strings.Contains(s, "_smb") || strings.Contains(s, "_afpovertcp") || strings.Contains(s, "_nfs"):
			return "servidor"
		case strings.Contains(s, "_ssh") || strings.Contains(s, "_http") || strings.Contains(s, "_https"):
			// Too generic — many devices expose SSH/HTTP. Don't classify.
			continue
		}
	}
	return "desconocido"
}
