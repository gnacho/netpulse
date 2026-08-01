// users.go — CRUD de usuarios (paridad routes/users.js).
//
//	PUT  /api/users/me/language   (cualquier rol — fuera del gate admin)
//	GET  /api/users               (admin)
//	POST /api/users               (admin) → 201 · 400 · 409
//	PUT  /api/users/:id/password  (admin) → 204 (invalida sus sesiones)
//	PUT  /api/users/:id/role?role= (admin) → rol nuevo en QUERY STRING (quirk)
//	DELETE /api/users/:id         (admin) → 204 (no self, no último admin)
package httpapi

import (
	"database/sql"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// parseUserID replica Number(:id): no numérico → NaN → no encontrado (404).
func parseUserID(s string) (float64, bool) {
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

type userTarget struct {
	ID       int64
	Username string
	Role     string
}

func (s *server) findUser(id float64) *userTarget {
	var t userTarget
	err := s.db.QueryRow("SELECT id, username, role FROM users WHERE id = ?", id).
		Scan(&t.ID, &t.Username, &t.Role)
	if err != nil {
		return nil
	}
	return &t
}

func (s *server) adminCount() int {
	var n int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&n)
	return n
}

// handleMyLanguage: {language: auto|es|en} → 204 (fuera del gate admin).
func (s *server) handleMyLanguage(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFromContext(r.Context())
	var body struct {
		Language string `json:"language"`
	}
	if !readJSONBody(r, &body) ||
		(body.Language != "auto" && body.Language != "es" && body.Language != "en") {
		writeError(w, http.StatusBadRequest, "invalid_input", "language debe ser auto|es|en")
		return
	}
	_, _ = s.db.Exec("UPDATE users SET language = ? WHERE id = ?", body.Language, me.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handleListUsers: {users:[{id, username, role, language, created_at}]} por username.
func (s *server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query("SELECT id, username, role, language, created_at FROM users ORDER BY username")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	defer rows.Close()
	users := []map[string]any{}
	for rows.Next() {
		var (
			id        int64
			username  string
			role      string
			language  sql.NullString
			createdAt int64
		)
		if err := rows.Scan(&id, &username, &role, &language, &createdAt); err != nil {
			continue
		}
		var lang any
		if language.Valid {
			lang = language.String
		}
		users = append(users, map[string]any{
			"id": id, "username": username, "role": role, "language": lang, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// handleCreateUser: alta (admin). bcrypt cost 10.
func (s *server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username *string `json:"username"`
		Password *string `json:"password"`
		Role     *string `json:"role"`
		Language *string `json:"language"`
	}
	if !readJSONBody(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid_input", "Required")
		return
	}
	// Validación (orden zod: username → password → role → language)
	if body.Username == nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Required")
		return
	}
	username := strings.TrimSpace(*body.Username)
	if len(username) < 2 {
		writeError(w, http.StatusBadRequest, "invalid_input", "String must contain at least 2 character(s)")
		return
	}
	if len(username) > 32 {
		writeError(w, http.StatusBadRequest, "invalid_input", "String must contain at most 32 character(s)")
		return
	}
	if !usernameRe.MatchString(username) {
		writeError(w, http.StatusBadRequest, "invalid_input", "usuario debe ser alfanumérico (.-_ permitidos)")
		return
	}
	if body.Password == nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Required")
		return
	}
	password := *body.Password
	if len(password) < 6 {
		writeError(w, http.StatusBadRequest, "invalid_input", "String must contain at least 6 character(s)")
		return
	}
	if len(password) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_input", "String must contain at most 128 character(s)")
		return
	}
	role := "user"
	if body.Role != nil {
		if *body.Role != "admin" && *body.Role != "user" {
			writeError(w, http.StatusBadRequest, "invalid_input",
				"Invalid enum value. Expected 'admin' | 'user'")
			return
		}
		role = *body.Role
	}
	language := "auto"
	if body.Language != nil {
		if *body.Language != "auto" && *body.Language != "es" && *body.Language != "en" {
			writeError(w, http.StatusBadRequest, "invalid_input",
				"Invalid enum value. Expected 'auto' | 'es' | 'en'")
			return
		}
		language = *body.Language
	}

	var exists int
	_ = s.db.QueryRow("SELECT 1 FROM users WHERE username = ?", username).Scan(&exists)
	if exists == 1 {
		writeError(w, http.StatusConflict, "duplicate_user", "Ya existe el usuario "+username)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	res, err := s.db.Exec(
		"INSERT INTO users (username, pass_hash, role, language, created_at) VALUES (?, ?, ?, ?, ?)",
		username, hash, role, language, db.NowMS(),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{
		"user": map[string]any{"id": id, "username": username, "role": role, "language": language},
	})
}

// handleSetPassword: {password 6..128} → 204 + destroyUserSessions.
func (s *server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUserID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	target := s.findUser(id)
	if target == nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var body struct {
		Password *string `json:"password"`
	}
	if !readJSONBody(r, &body) || body.Password == nil || len(*body.Password) < 6 || len(*body.Password) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_input", "password mínimo 6 caracteres")
		return
	}
	hash, err := auth.HashPassword(*body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	_, _ = s.db.Exec("UPDATE users SET pass_hash = ? WHERE id = ?", hash, target.ID)
	auth.DestroyUserSessions(s.db, target.ID) // fuerza re-login
	w.WriteHeader(http.StatusNoContent)
}

// handleSetRole: rol nuevo en QUERY STRING (quirk del contrato) → 204.
func (s *server) handleSetRole(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUserID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	me := auth.UserFromContext(r.Context())
	target := s.findUser(id)
	if target == nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	role := r.URL.Query().Get("role")
	if role != "admin" && role != "user" {
		writeError(w, http.StatusBadRequest, "invalid_input", "role debe ser admin|user")
		return
	}
	if target.ID == me.ID {
		writeError(w, http.StatusBadRequest, "cannot_change_self", "No puedes cambiar tu propio rol")
		return
	}
	if target.Role == "admin" && role == "user" && s.adminCount() <= 1 {
		writeError(w, http.StatusBadRequest, "last_admin", "Debe quedar al menos un admin")
		return
	}
	_, _ = s.db.Exec("UPDATE users SET role = ? WHERE id = ?", role, target.ID)
	auth.DestroyUserSessions(s.db, target.ID) // fuerza re-login con el nuevo rol
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteUser: 204 (no self, no último admin); borra sesiones y la fila.
func (s *server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUserID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	me := auth.UserFromContext(r.Context())
	target := s.findUser(id)
	if target == nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if target.ID == me.ID {
		writeError(w, http.StatusBadRequest, "cannot_delete_self", "No puedes borrar tu propio usuario")
		return
	}
	if target.Role == "admin" && s.adminCount() <= 1 {
		writeError(w, http.StatusBadRequest, "last_admin", "Debe quedar al menos un admin")
		return
	}
	auth.DestroyUserSessions(s.db, target.ID)
	_, _ = s.db.Exec("DELETE FROM users WHERE id = ?", target.ID)
	w.WriteHeader(http.StatusNoContent)
}
