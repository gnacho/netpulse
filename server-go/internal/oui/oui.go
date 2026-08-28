// Package oui resolves the first 3 bytes of a MAC address (the IEEE OUI) to
// a manufacturer name using an embedded prefix map derived from the Wireshark
// manuf database (which itself is generated from IEEE registration data).
// It also exposes IoT vendor classification for device-type guessing.
package oui

import (
	_ "embed"
	"strings"
	"sync"
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
}

// IsIoTVendor reports whether a manufacturer name (as returned by Lookup)
// matches a known IoT vendor.
func IsIoTVendor(manufacturer string) bool {
	m := strings.ToLower(strings.TrimSpace(manufacturer))
	for _, s := range iotVendors {
		if m != "" && strings.Contains(m, s) {
			return true
		}
	}
	return false
}
