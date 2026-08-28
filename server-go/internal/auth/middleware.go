// middleware.go — requireAuth / requireAdmin (paridad auth.js:258-278).
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSessionID
	ctxAPIToken
)

// UserFromContext devuelve el usuario autenticado (nil si no hay).
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxUser).(*User)
	return u
}

// SessionIDFromContext devuelve el sessionId autenticado ("" si no hay).
func SessionIDFromContext(ctx context.Context) string {
	s, _ := ctx.Value(ctxSessionID).(string)
	return s
}

// TokenValidator valida un bearer token y devuelve el usuario y scope.
// Devuelve nil si el token no es válido. Implementado por apitoken.Store.
type TokenValidator interface {
	ValidateBearer(raw string) (*User, string)
}

// APITokenFromContext devuelve el scope del token API usado ("" si no es token).
func APITokenFromContext(ctx context.Context) string {
	s, _ := ctx.Value(ctxAPIToken).(string)
	return s
}

// WriteError escribe el envelope de error {error, message?} con el status dado.
func WriteError(w http.ResponseWriter, status int, code string, message ...string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": code}
	if len(message) > 0 {
		body["message"] = message[0]
	}
	// json.Marshal (no Encoder): el cuerpo debe ser literal como Hono, SIN
	// el '\n' final que añade Encode.
	data, _ := json.Marshal(body)
	_, _ = w.Write(data)
}

// RequireAuth es middleware global: todo /api/* exige sesión salvo
// /api/health, /api/auth/login, /api/ingest/agent (auth Bearer por token
// de equipo), /api/agents/pair (pairing token de un solo uso, Fase 9 R3),
// /api/agents/{slug}/binary (auth Bearer por token de agente,
// Fase 6.2), /api/agents/{slug}/stream (SSE bidireccional, Fase 7.3),
// /api/agents/{slug}/apply-result, /api/agents/{slug}/upgrade-result y
// /api/agents/{slug}/upgrade-progress (reportes del agente, auth Bearer).
// Acepta Bearer tokens de API (#330) como alternativa a la cookie de sesión.
// Fallo → 401 {error:'unauthorized'}.
func RequireAuth(d *db.DB, secret string, tv TokenValidator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !strings.HasPrefix(path, "/api/") || path == "/api/health" || path == "/api/auth/login" ||
			path == "/api/ingest/agent" || path == "/api/agents/pair" ||
			(strings.HasPrefix(path, "/api/agents/") && (strings.HasSuffix(path, "/binary") || strings.HasSuffix(path, "/stream") || strings.HasSuffix(path, "/apply-result") || strings.HasSuffix(path, "/upgrade-result") || strings.HasSuffix(path, "/upgrade-progress"))) {
			next.ServeHTTP(w, r)
			return
		}
		if tv != nil {
			if raw := bearerToken(r); raw != "" {
				if user, scope := tv.ValidateBearer(raw); user != nil {
					ctx := context.WithValue(r.Context(), ctxUser, user)
					ctx = context.WithValue(ctx, ctxAPIToken, scope)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}
		sess := SessionUserFromRequest(d, secret, r)
		if sess == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, sess.User)
		ctx = context.WithValue(ctx, ctxSessionID, sess.SessionID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(v, "Bearer ")
}

// RequireAdmin exige role === 'admin' (tras RequireAuth) → 403 {error:'forbidden'}.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || user.Role != "admin" {
			WriteError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSameOrigin es la defensa CSRF (issue #213). SameSite=Lax cubre el
// envío de cookies cross-site de fetch/XHR, pero no hay defensa en
// profundidad si la app se sirve desde otro origen o un navegador sin
// SameSite. En mutaciones (POST/PUT/DELETE) se valida el header Origin contra
// el host efectivo de la petición (X-Forwarded-Host si TRUST_PROXY, si no
// r.Host). Un Origin ausente (cliente no-navegador: agente, curl, CLI) se
// deja pasar; si el navegador envía Sec-Fetch-Site: cross-site sin Origin, se
// rechaza igualmente. Un Origin que no casa → 403 {error:'cross_origin'}.
func RequireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			if !originAllowed(r) {
				WriteError(w, http.StatusForbidden, "cross_origin")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed valida el header Origin (si viene) contra el host efectivo.
func originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		if sf := r.Header.Get("Sec-Fetch-Site"); sf != "" {
			return sf != "cross-site"
		}
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// Origin "null" (iframe sandbox / redirect) no tiene host → no casa.
	if u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, effectiveHost(r))
}

// effectiveHost devuelve el host al que el cliente cree estar llamando:
// X-Forwarded-Host (si TRUST_PROXY) o r.Host. Sin TRUST_PROXY, un header
// falseado no puede eludir la comprobación.
func effectiveHost(r *http.Request) string {
	if trustProxy {
		if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
			if first := strings.TrimSpace(strings.Split(xfh, ",")[0]); first != "" {
				return first
			}
		}
	}
	return r.Host
}
