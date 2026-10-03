package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/apitoken"
	"github.com/gnacho/netpulse/server-go/internal/auth"
)

// Handler devuelve el handler HTTP de /mcp: rate limit por IP → auth Bearer
// por API token → servidor MCP streamable-HTTP. La cadena va en este orden
// para que el brute-force de tokens también quede limitado.
//
// SOLO acepta API tokens (internal/apitoken): nunca la cookie de sesión, para
// no mezclar superficies CSRF con clientes AI (spec fase 3.5). Cualquier scope
// válido (read/write/admin) basta: la primera tanda de tools es solo lectura.
func (s *Server) Handler(tokens *apitoken.Store) http.Handler {
	// Sin token store el endpoint NO se sirve sin auth: fail-closed (el MCP
	// nunca debe quedar abierto; main solo lo construye con tokenStore, pero
	// la defensa va aquí, en la última capa antes del handler).
	if tokens == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"mcp_auth_unavailable"}`))
		})
	}
	return rateLimit(s.ratePerMin, bearerOnly(tokens, http.Handler(s.http)))
}

// bearerOnly exige Authorization: Bearer <api token> válido. Sin token, con
// token mal formado o inválido → 401 {error: unauthorized}.
func bearerOnly(tokens *apitoken.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" || tokens.Validate(raw) == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if !strings.HasPrefix(v, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(v, "Bearer ")
}

// rlBucket es la ventana deslizante de un cliente (por IP).
type rlBucket struct {
	window time.Time
	count  int
}

// rateLimit limita /mcp por IP (ventana fija de 1 minuto). Sin tokens ni
// estado externo: brute-force de tokens queda acotado a ratePerMin intentos/min.
func rateLimit(perMin int, next http.Handler) http.Handler {
	var mu sync.Mutex
	buckets := map[string]*rlBucket{}
	window := time.Minute
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		now := time.Now()
		mu.Lock()
		b := buckets[ip]
		if b == nil || now.Sub(b.window) >= window {
			b = &rlBucket{window: now}
			buckets[ip] = b
		}
		b.count++
		full := b.count > perMin
		mu.Unlock()
		if full {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate_limited"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP prefiere X-Forwarded-For solo si el proceso confía en proxy
// (misma regla global que internal/auth: sin TRUST_PROXY el header no es
// fiable y se usa RemoteAddr).
func clientIP(r *http.Request) string {
	if auth.TrustProxy() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}
