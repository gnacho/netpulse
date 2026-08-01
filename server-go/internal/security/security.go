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
	{"Strict-Transport-Security", "max-age=31536000; includeSubDomains"},
	{"Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'"},
}

// Middleware aplica los headers a todas las respuestas (HSTS también por
// HTTP plano — comportamiento del JS, preservado).
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range Headers {
			w.Header().Set(h.K, h.V)
		}
		next.ServeHTTP(w, r)
	})
}
