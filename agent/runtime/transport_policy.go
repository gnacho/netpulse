package runtime

import (
	"fmt"
	"net/url"
	"strings"
)

// privilegedEventAllowed decide si el agente puede aceptar comandos que
// acaban en root (apply, upgrade, firmware_upgrade, config_backup). HTTPS es
// el unico transporte valido. HTTP solo se permite durante la transicion con
// NETPULSE_ALLOW_INSECURE_COMMANDS=1.
func privilegedEventAllowed(server string, allowInsecure bool) error {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("server URL without scheme or host: %q", server)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if allowInsecure {
			return nil
		}
		return fmt.Errorf("privileged commands rejected over HTTP; use HTTPS or NETPULSE_ALLOW_INSECURE_COMMANDS=1")
	default:
		return fmt.Errorf("scheme %q unsupported for privileged commands", u.Scheme)
	}
}
