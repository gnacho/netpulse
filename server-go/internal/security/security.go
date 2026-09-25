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

// HSTS is the Strict-Transport-Security value sent on secure responses.
//
// It is sent only when the request itself was secure. Over plain HTTP the
// header is ignored by browsers (RFC 6797 section 8.1) and only misstates what
// the server offers. includeSubDomains is left out: nothing lives under the
// server's own name, and the flag would pin every host below it to HTTPS for
// a year, which makes turning HTTPS off again much harder to undo.
const HSTS = "max-age=31536000"

// Middleware applies the headers to every response, and HSTS to those that
// secure reports as secure (a TLS connection, or a TLS-terminating proxy the
// server was told to trust).
func Middleware(secure func(*http.Request) bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range Headers {
			w.Header().Set(h.K, h.V)
		}
		if secure != nil && secure(r) {
			w.Header().Set("Strict-Transport-Security", HSTS)
		}
		next.ServeHTTP(w, r)
	})
}
