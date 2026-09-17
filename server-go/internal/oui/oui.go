// Package oui resolves the first 3 bytes of a MAC address (the IEEE OUI) to
// a manufacturer name using an embedded prefix map derived from the Wireshark
// manuf database (which itself is generated from IEEE registration data).
// It also exposes IoT vendor classification for device-type guessing.
package oui

import (
	_ "embed"
	"strings"
	"sync"
	"unicode"
)

// The embedded table uses the compact format "aabbcc\tVendor Name" (one OUI
// per line, lowercase hex prefix, tab-separated vendor). Regenerate it from
// the IEEE/Wireshark manuf data when vendor assignments change.
//
//go:embed data/oui.txt
var data string

var (
	loadOnce sync.Once
	table    map[string]string
)

func load() {
	loadOnce.Do(func() {
		table = make(map[string]string, 40000)
		for _, line := range strings.Split(data, "\n") {
			prefix, vendor, ok := strings.Cut(line, "\t")
			if !ok || prefix == "" || vendor == "" {
				continue
			}
			table[prefix] = vendor
		}
	})
}

// Lookup returns the manufacturer name for a MAC address (any separator or
// case is accepted), or "" when the OUI is not in the embedded database.
func Lookup(mac string) string {
	load()
	prefix := normalize(mac)
	if len(prefix) < 6 {
		return ""
	}
	return table[prefix[:6]]
}

// normalize lowercases a MAC and strips separators (':', '-', '.'), keeping
// only hex digits.
func normalize(mac string) string {
	var b strings.Builder
	b.Grow(len(mac))
	for _, c := range strings.ToLower(mac) {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			b.WriteByte(byte(c))
		}
	}
	return b.String()
}

// iotVendors lists manufacturer substrings (lowercase) that identify IoT
// vendors in the OUI database (sockets, bulbs, locks, vacuums, sensors...).
var iotVendors = []string{
	"espressif",
	"tuya",
	"itead",
	"sonoff",
	"shelly",
	"meross",
	"gosund",
	"lumi",
	"aqara",
	"heimgard",
	"roborock",
	"dreame",
	"ecovacs",
	"signify",
	"ledvance",
	"ikea",
	"tradfri",
	"feeyree",
	"nuki",
	"tedee",
	"wyze",
	"broadlink",
	"qingping",
	"shenzhen",
	"tasmota",
	// Electrodomésticos y clima conectados: no son "el IoT de siempre" (un
	// enchufe, una bombilla), pero para la topología son lo mismo — un
	// cacharro de la casa que habla por wifi.
	"wiz",     // bombillas WiZ (Signify, OUI propio)
	"gree",    // aire acondicionado
	"hausger", // BSH Hausgeräte: horno/lavavajillas Bosch y Siemens
}

// cameraVendors: fabricantes cuyo catálogo es videovigilancia. Un OUI suyo
// es una cámara o un grabador, no un cacharro genérico: hay un tipo propio
// (con su icono) y decir "iot" sería perder información que sí tenemos.
var cameraVendors = []string{
	"reolink",
	"hikvision",
	"dahua",
	"ezviz",
	"amcrest",
	"foscam",
	"annke",
}

// IsCameraVendor reports whether a manufacturer only makes surveillance gear.
func IsCameraVendor(manufacturer string) bool {
	return vendorMatches(manufacturer, cameraVendors)
}

// vendorMatches: substring search, except for tokens under 6 characters,
// which have to appear as a whole word. A short token is a liability as a
// substring -- "gree" would swallow Greenwave and Greenliant, "wiz" would
// swallow WIZnet -- and a wrong vendor match hands the device a confident,
// wrong icon. Longer tokens stay substrings so "Dreame Technology" and
// "Shelly Europe Ltd" match whatever suffix the OUI registry carries.
func vendorMatches(manufacturer string, list []string) bool {
	m := strings.ToLower(strings.TrimSpace(manufacturer))
	if m == "" {
		return false
	}
	var words []string
	for _, s := range list {
		if len(s) >= 6 {
			if strings.Contains(m, s) {
				return true
			}
			continue
		}
		if words == nil {
			words = strings.FieldsFunc(m, func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsDigit(r)
			})
		}
		for _, w := range words {
			if w == s {
				return true
			}
		}
	}
	return false
}

// IsIoTVendor reports whether a manufacturer name (as returned by Lookup)
// matches a known IoT vendor.
func IsIoTVendor(manufacturer string) bool {
	return vendorMatches(manufacturer, iotVendors)
}
