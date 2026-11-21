package adapters

import (
	"regexp"
	"strings"
)

var (
	generatedPlatformNameRE  = regexp.MustCompile(`^(?:android|iphone|ipad|tablet|phone|device)[-_]([a-z0-9]{6,})$`)
	generatedTechnicalNameRE = regexp.MustCompile(`^(?:shelly|esphome|esp|tasmota|plug)[a-z0-9]*[-_][0-9a-f]{6,12}$`)
)

// isGeneratedDeviceName reports whether a name is a generated placeholder,
// not a useful device name chosen by the device owner. Keep the patterns
// narrow: an ordinary model, version or location must never be demoted.
func isGeneratedDeviceName(value string) bool {
	name := strings.ToLower(strings.TrimSpace(value))
	if name == "" {
		return false
	}
	if generatedTechnicalNameRE.MatchString(name) {
		return true
	}
	match := generatedPlatformNameRE.FindStringSubmatch(name)
	return len(match) == 2 && strings.ContainsAny(match[1], "0123456789")
}
