// config.go — rutas /api/config/* (paridad src/routes/config.js, SPEC §2.17):
// CRUD de routers (tabla `routers`), clave SSH propia del servidor,
// descubrimiento de la LAN y credenciales AdGuard GL.iNet (kv; la contraseña
// NUNCA se devuelve). Tras cada mutación se sincroniza el adapter
// (SetRouters) sin reiniciar.
package httpapi

import (
	"database/sql"
	"net/http"
	"regexp"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/discover"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sshkey"
)

var hostRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// registerConfigRoutes registra las rutas /api/config/* en el mux.
func (s *server) registerConfigRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config/sshkey", s.handleGetSSHKey)
	mux.HandleFunc("GET /api/config/discover", s.handleDiscover)
	mux.HandleFunc("GET /api/config/routers", s.handleListConfigRouters)
	mux.HandleFunc("POST /api/config/routers", s.handleAddConfigRouter)
	mux.HandleFunc("DELETE /api/config/routers/{id}", s.handleDeleteConfigRouter)
	mux.Handle("GET /api/config/adguard", auth.RequireAdmin(http.HandlerFunc(s.handleGetAdguardConfig)))
	mux.Handle("PUT /api/config/adguard", auth.RequireAdmin(http.HandlerFunc(s.handlePutAdguardConfig)))
}

// syncRouters replica sync() de config.js: adapter.setRouters(listRouters(db)).
func (s *server) syncRouters() {
	if s.adapter != nil {
		s.adapter.SetRouters(routerstore.ListRouters(s.db.DB))
	}
}

// GET /api/config/sshkey — clave pública propia para autorizar en routers.
func (s *server) handleGetSSHKey(w http.ResponseWriter, r *http.Request) {
	key := sshkey.GetPublicKey(s.cfg.SSHKeyPath)
	if key == nil {
		writeError(w, http.StatusInternalServerError, "no_key")
		return
	}
	writeJSON(w, http.StatusOK, key)
}

// GET /api/config/discover?force=1 — escaneo de la LAN (cacheado 60 s).
func (s *server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	result := discover.Routers(s.db.DB, s.cfg.SSHKeyPath, r.URL.Query().Get("force") == "1")
	writeJSON(w, http.StatusOK, result)
}

// GET /api/config/routers — lista configurada (no el estado sondeado).
func (s *server) handleListConfigRouters(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"routers": routerstore.ListRouters(s.db.DB)})
}

type routerInput struct {
	Name    *string `json:"name"`
	Host    *string `json:"host"`
	Type    string  `json:"type"`
	Gateway bool    `json:"gateway"`
}

// validateHost replica hostSchema (trim, 1..253, regex). Devuelve el valor
// trimmeado o "" + mensaje de error.
func validateHost(host string) (string, string) {
	h := strings.TrimSpace(host)
	if len(h) < 1 {
		return "", "String must contain at least 1 character(s)"
	}
	if len(h) > 253 {
		return "", "String must contain at most 253 character(s)"
	}
	if !hostRe.MatchString(h) {
		return "", "host debe ser una IP o hostname válido"
	}
	return h, ""
}

// POST /api/config/routers — añadir router manualmente desde Ajustes.
func (s *server) handleAddConfigRouter(w http.ResponseWriter, r *http.Request) {
	var in routerInput
	if !readJSONBody(r, &in) {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	// Orden de validación del schema zod: name, host, type
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if len(name) < 1 {
			writeError(w, http.StatusBadRequest, "invalid_input", "String must contain at least 1 character(s)")
			return
		}
		if len(name) > 60 {
			writeError(w, http.StatusBadRequest, "invalid_input", "String must contain at most 60 character(s)")
			return
		}
	}
	if in.Host == nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Required")
		return
	}
	host, msg := validateHost(*in.Host)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_input", msg)
		return
	}
	typ := in.Type
	if typ == "" {
		typ = "openwrt"
	}
	if typ != "glinet" && typ != "openwrt" {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid enum value. Expected 'glinet' | 'openwrt'")
		return
	}
	for _, rt := range routerstore.ListRouters(s.db.DB) {
		if rt.Host == host {
			writeError(w, http.StatusConflict, "duplicate_host", "Ya hay un router con "+host)
			return
		}
	}
	created, err := routerstore.AddRouter(s.db.DB, routerstore.AddInput{
		Name: name, Host: host, Type: typ, IsGateway: in.Gateway,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	s.syncRouters()
	writeJSON(w, http.StatusCreated, map[string]any{"router": created})
}

// DELETE /api/config/routers/:id — 204 o 404.
func (s *server) handleDeleteConfigRouter(w http.ResponseWriter, r *http.Request) {
	if !routerstore.RemoveRouter(s.db.DB, r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	s.syncRouters()
	w.WriteHeader(http.StatusNoContent)
}

// --- AdGuard Home (GL.iNet) — solo admin; la contraseña NO se devuelve ---

func kvGet(db *sql.DB, key string) string {
	var v string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&v); err != nil {
		return ""
	}
	return v
}

// GET /api/config/adguard — {host, user ('root' por defecto), passSet}.
func (s *server) handleGetAdguardConfig(w http.ResponseWriter, r *http.Request) {
	user := kvGet(s.db.DB, "adguard_user")
	if user == "" {
		user = "root"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"host":    kvGet(s.db.DB, "adguard_host"),
		"user":    user,
		"passSet": kvGet(s.db.DB, "adguard_pass") != "",
	})
}

type adguardInput struct {
	Host     *string `json:"host"`
	User     string  `json:"user"`
	Password string  `json:"password"`
}

// PUT /api/config/adguard — upsert en kv (password solo si viene). 204.
func (s *server) handlePutAdguardConfig(w http.ResponseWriter, r *http.Request) {
	var in adguardInput
	if !readJSONBody(r, &in) {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if in.Host == nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Required")
		return
	}
	host, msg := validateHost(*in.Host)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_input", msg)
		return
	}
	user := strings.TrimSpace(in.User)
	if user == "" {
		user = "root"
	}
	if len(user) > 64 {
		writeError(w, http.StatusBadRequest, "invalid_input", "String must contain at most 64 character(s)")
		return
	}
	if len(in.Password) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_input", "String must contain at most 128 character(s)")
		return
	}
	upsert := "INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
	if _, err := s.db.Exec(upsert, "adguard_host", host); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if _, err := s.db.Exec(upsert, "adguard_user", user); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if in.Password != "" {
		if _, err := s.db.Exec(upsert, "adguard_pass", in.Password); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
