// auth.go — POST /api/auth/login · POST /api/auth/logout · GET /api/auth/me
// (paridad routes/auth.js).
package httpapi

import (
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/auth"
)

// handleLogin: 429 rate_limited (antes de validar nada) → 400 invalid_body →
// 401 invalid_credentials → 204 + Set-Cookie (rotación de sesión).
func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if limited, retryAfterSec := auth.LoginRateLimited(s.db, r); limited {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":         "rate_limited",
			"retryAfterSec": retryAfterSec,
		})
		return
	}
	var body struct {
		Username *string `json:"username"`
		Password *string `json:"password"`
	}
	if !readJSONBody(r, &body) || body.Username == nil || body.Password == nil ||
		len(*body.Username) < 1 || len(*body.Username) > 64 || len(*body.Password) < 1 {
		writeError(w, http.StatusBadRequest, "invalid_body",
			`Se esperaba { "username": string, "password": string }`)
		return
	}
	result := auth.HandleLogin(s.db, s.secret, r, *body.Username, *body.Password)
	if result == nil {
		auth.RegisterLoginFail(s.db, r)
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	auth.LoginOk(s.db, r)
	signed := auth.SignSessionID(s.secret, result.SessionID)
	w.Header().Set("Set-Cookie", auth.BuildSessionCookie(s.cfg, r, signed, auth.SessionTTLMS/1000))
	w.WriteHeader(http.StatusNoContent)
}

// handleLogout: destruye la sesión y borra la cookie (204; SIN Secure).
func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	auth.HandleLogout(s.db, s.secret, r)
	w.Header().Set("Set-Cookie", auth.ClearSessionCookie)
	w.WriteHeader(http.StatusNoContent)
}

// handleMe: {user, role, language, displayName, mode} (language/displayName
// releídos de users; SPEC-65 D65-5: displayName "" = no puesto).
func (s *server) handleMe(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		language := "auto"
		var lang *string
		if err := s.db.QueryRow("SELECT language FROM users WHERE id = ?", user.ID).Scan(&lang); err == nil && lang != nil {
			language = *lang
		}
		displayName := ""
		var dn *string
		if err := s.db.QueryRow("SELECT display_name FROM users WHERE id = ?", user.ID).Scan(&dn); err == nil && dn != nil {
			displayName = *dn
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"user":        user.Username,
			"role":        user.Role,
			"language":    language,
			"displayName": displayName,
			"mode":        mode,
		})
	}
}
