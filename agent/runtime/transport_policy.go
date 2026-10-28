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
		return fmt.Errorf("URL de servidor invalida: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("URL de servidor sin esquema o host: %q", server)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if allowInsecure {
			return nil
		}
		return fmt.Errorf("comandos privilegiados rechazados sobre HTTP; usa HTTPS o NETPULSE_ALLOW_INSECURE_COMMANDS=1")
	default:
		return fmt.Errorf("esquema %q no soportado para comandos privilegiados", u.Scheme)
	}
}
