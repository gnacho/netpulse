// middleware.go — requireAuth / requireAdmin (paridad auth.js:258-278).
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSessionID
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
// /api/health, /api/auth/login y /api/ingest/agent (este último lleva auth
// propia Bearer por token de equipo, SPEC-AGENTE-PILOTO §1 — la sesión es de
// humanos). Fallo → 401 {error:'unauthorized'}.
func RequireAuth(d *db.DB, secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !strings.HasPrefix(path, "/api/") || path == "/api/health" || path == "/api/auth/login" ||
			path == "/api/ingest/agent" {
			next.ServeHTTP(w, r)
			return
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
