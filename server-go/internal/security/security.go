// Package security — headers de seguridad obligatorios (middleware global).
// Literales de src/security.js:6-19 (SPEC §10.1), en TODAS las respuestas.
package security

import "net/http"

// Headers es la lista literal de headers (orden de security.js).
var Headers = []struct{ K, V string }{
	{"X-Content-Type-Options", "nosniff"},
	{"X-Frame-Options", "DENY"},
	{"Referrer-Policy", "strict-origin-when-cross-origin"},
	{"Permissions-Policy", "geolocation=(), microphone=(), camera=()"},
	// style-src lleva 'unsafe-inline' a propósito (#485): Radix (posicionado
	// popper) y framer-motion (transform/opacity) escriben atributos style
	// CON VALORES DINÁMICOS (píxeles calculados), imposibles de hashear con
	// 'unsafe-hashes'. Sin 'unsafe-inline' cada apertura de diálogo loguea
	// una violación CSP y las transiciones no corren. El riesgo de CSS
	// inline es muy inferior al de script inline (la app no renderiza HTML
	// controlado por el usuario), y script-src sigue sin 'unsafe-inline'.
	{"Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'"},
}

// HSTS is the Strict-Transport-Security value for a server whose every
// port speaks TLS.
//
// includeSubDomains is left out: nothing lives under the server's own name,
// and the flag would pin every host below it to HTTPS for a year, which
// makes turning HTTPS off again much harder to undo.
const HSTS = "max-age=31536000"

// Middleware applies the headers to every response, and the HSTS value hsts
// returns for it, if any.
//
// HSTS is per host, not per port (RFC 6797 section 8.3): a policy received on
// one port makes the browser rewrite http://host:ANY to https://host:ANY. So
// it may only be sent where no port of the host still serves plain HTTP that
// people need - the caller decides that, not this package.
func Middleware(hsts func(*http.Request) string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range Headers {
			w.Header().Set(h.K, h.V)
		}
		if hsts != nil {
			if v := hsts(r); v != "" {
				w.Header().Set("Strict-Transport-Security", v)
			}
		}
		next.ServeHTTP(w, r)
	})
}
