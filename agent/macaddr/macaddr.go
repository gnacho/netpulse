// Package macaddr normaliza direcciones MAC a la forma canónica de NetPulse
// (AA:BB:CC:DD:EE:FF). El agente y el servidor la usan desde aquí para que
// las dos partes no diverjan.
package macaddr

import "strings"

// Normalize devuelve la MAC en forma canónica (AA:BB:CC:DD:EE:FF) o "" si
// raw no es una MAC ethernet de 6 octetos. Acepta separadores ':' o '-' (el
// mismo en las cinco posiciones) y 12 dígitos hex sin separador; ignora
// espacios alrededor y mayúsculas/minúsculas.
func Normalize(raw string) string {
	s := strings.TrimSpace(raw)
	var b [17]byte
	switch len(s) {
	case 17:
		sep := s[2]
		if sep != ':' && sep != '-' {
			return ""
		}
		for i := 0; i < 17; i++ {
			c := s[i]
			if i%3 == 2 {
				if c != sep {
					return ""
				}
				b[i] = ':'
				continue
			}
			u, ok := upperHex(c)
			if !ok {
				return ""
			}
			b[i] = u
		}
	case 12:
		for i := 0; i < 12; i++ {
			u, ok := upperHex(s[i])
			if !ok {
				return ""
			}
			b[i/2*3+i%2] = u
			if i%2 == 1 && i < 11 {
				b[i/2*3+2] = ':'
			}
		}
	default:
		return ""
	}
	return string(b[:])
}

// upperHex devuelve el dígito hex en mayúscula y si c era un dígito hex.
func upperHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9', c >= 'A' && c <= 'F':
		return c, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 'A', true
	}
	return 0, false
}
