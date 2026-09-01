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
	"strconv"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/discover"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sshkey"
)

var (
	hostRe    = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	hostPortRe = regexp.MustCompile(`^([a-zA-Z0-9._-]+)(:[0-9]{1,5})?$`)
)

// registerConfigRoutes registra las rutas /api/config/* en el mux.
// Las mutaciones (añadir/borrar routers) y las que exponen credenciales o
// escanean la red (sshkey, discover) exigen rol admin (auditoría v2.4.0 §2,
// issue #7); la lista de routers es de lectura y queda tras RequireAuth.
func (s *server) registerConfigRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/config/sshkey", auth.RequireAdmin(http.HandlerFunc(s.handleGetSSHKey)))
	mux.Handle("POST /api/config/sshkey/rotate", auth.RequireAdmin(http.HandlerFunc(s.handleRotateSSHKey)))
	mux.Handle("GET /api/config/discover", auth.RequireAdmin(http.HandlerFunc(s.handleDiscover)))
	mux.HandleFunc("GET /api/config/routers", s.handleListConfigRouters)
	mux.Handle("POST /api/config/routers", auth.RequireAdmin(http.HandlerFunc(s.handleAddConfigRouter)))
	mux.Handle("PUT /api/config/routers/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleUpdateConfigRouter)))
	mux.Handle("DELETE /api/config/routers/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleDeleteConfigRouter)))
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

// POST /api/config/sshkey/rotate — rota el par ed25519 del servidor (#242).
// Respalda el par actual (keyPath.bak.<epoch>) y genera uno nuevo; devuelve
// la nueva pública + fingerprint para reautorizarla en los routers. La clave
// vieja deja de funcionar de inmediato: exige confirmación explícita del admin
// (la UI pide escribir la palabra de confirmación).
func (s *server) handleRotateSSHKey(w http.ResponseWriter, r *http.Request) {
	key, err := sshkey.RotateKeypair(s.cfg.SSHKeyPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rotate_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"publicKey":   key.PublicKey,
		"fingerprint": key.Fingerprint,
		"warning":     "The previous key is no longer valid. Re-authorize this public key on every router before rotating again.",
	})
}

// GET /api/config/discover?force=1 — escaneo de la LAN (cacheado 60 s).
func (s *server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	result := discover.Routers(r.Context(), s.db.DB, s.cfg.SSHKeyPath, r.URL.Query().Get("force") == "1")
	writeJSON(w, http.StatusOK, result)
}

// GET /api/config/routers — lista configurada (no el estado sondeado).
func (s *server) handleListConfigRouters(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"routers": routerstore.ListRouters(s.db.DB)})
}

type routerInput struct {
	Name      *string `json:"name"`
	Host      *string `json:"host"`
	Type      string  `json:"type"`
	Gateway   bool    `json:"gateway"`
	AgentOnly bool    `json:"agent_only"`
	// FirmwareTarget: versión objetivo del firmware (issue #241; opcional).
	FirmwareTarget *string `json:"firmware_target"`
	// SNMP (issue #309): credenciales para sondeo SNMP del switch gestionado.
	SnmpEnabled   *bool   `json:"snmp_enabled"`
	SnmpCommunity *string `json:"snmp_community"`
	SnmpPort      *int    `json:"snmp_port"`
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

// validateAdGuardHost permite host o host:puerto (1..65535). Devuelve
// host, puerto y mensaje de error.
func validateAdGuardHost(host string) (string, int, string) {
	h := strings.TrimSpace(host)
	if len(h) < 1 {
		return "", 0, "String must contain at least 1 character(s)"
	}
	if len(h) > 253 {
		return "", 0, "String must contain at most 253 character(s)"
	}
	m := hostPortRe.FindStringSubmatch(h)
	if m == nil {
		return "", 0, "host debe ser una IP, hostname o host:puerto válido"
	}
	addr := m[1]
	port := 0
	if m[2] != "" {
		p, err := strconv.Atoi(m[2][1:])
		if err != nil || p < 1 || p > 65535 {
			return "", 0, "puerto fuera de rango (1-65535)"
		}
		port = p
	}
	return addr, port, ""
}

// POST /api/config/routers — añadir router manualmente desde Ajustes.
func (s *server) handleAddConfigRouter(w http.ResponseWriter, r *http.Request) {
	var in routerInput
	if st := readJSONBody(w, r, &in); st != 0 {
		writeBodyError(w, st, "invalid_json", "")
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
	if typ != "glinet" && typ != "openwrt" && typ != "managed-switch" && typ != "external" {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid enum value. Expected 'glinet' | 'openwrt' | 'managed-switch' | 'external'")
		return
	}
	var firmwareTarget string
	if in.FirmwareTarget != nil {
		firmwareTarget = strings.TrimSpace(*in.FirmwareTarget)
	}
	snmpEnabled := false
	if in.SnmpEnabled != nil {
		snmpEnabled = *in.SnmpEnabled
	}
	snmpCommunity := ""
	if in.SnmpCommunity != nil {
		snmpCommunity = strings.TrimSpace(*in.SnmpCommunity)
	}
	snmpPort := 0
	if in.SnmpPort != nil {
		snmpPort = *in.SnmpPort
		if snmpPort < 0 || snmpPort > 65535 {
			writeError(w, http.StatusBadRequest, "invalid_input", "snmp_port must be between 0 and 65535")
			return
		}
	}
	for _, rt := range routerstore.ListRouters(s.db.DB) {
		if rt.Host == host {
			writeError(w, http.StatusConflict, "duplicate_host", "Ya hay un router con "+host)
			return
		}
	}
	created, err := routerstore.AddRouter(s.db.DB, routerstore.AddInput{
		Name: name, Host: host, Type: typ, IsGateway: in.Gateway, AgentOnly: in.AgentOnly,
		FirmwareTarget: firmwareTarget,
		SnmpEnabled: snmpEnabled, SnmpCommunity: snmpCommunity, SnmpPort: snmpPort,
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

// PUT /api/config/routers/:id — edita host/name/type/gateway/agent_only.
// Campos omitidos (nil) no se tocan. 404 si el router no existe.
func (s *server) handleUpdateConfigRouter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in routerInput
	if st := readJSONBody(w, r, &in); st != 0 {
		writeBodyError(w, st, "invalid_json", "")
		return
	}
	var name *string
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if len(n) < 1 {
			writeError(w, http.StatusBadRequest, "invalid_input", "String must contain at least 1 character(s)")
			return
		}
		if len(n) > 60 {
			writeError(w, http.StatusBadRequest, "invalid_input", "String must contain at most 60 character(s)")
			return
		}
		name = &n
	}
	var host *string
	if in.Host != nil {
		h, msg := validateHost(*in.Host)
		if msg != "" {
			writeError(w, http.StatusBadRequest, "invalid_input", msg)
			return
		}
		// Duplicado: otro router con ese host (excluyendo el propio id).
		for _, rt := range routerstore.ListRouters(s.db.DB) {
			if rt.Host == h && rt.ID != id {
				writeError(w, http.StatusConflict, "duplicate_host", "Ya hay un router con "+h)
				return
			}
		}
		host = &h
	}
	var typ *string
	if in.Type != "" {
		t := in.Type
		if t != "glinet" && t != "openwrt" && t != "managed-switch" && t != "external" {
			writeError(w, http.StatusBadRequest, "invalid_input", "Invalid enum value. Expected 'glinet' | 'openwrt' | 'managed-switch' | 'external'")
			return
		}
		typ = &t
	}
	gw := in.Gateway
	ao := in.AgentOnly
	var firmwareTarget *string
	if in.FirmwareTarget != nil {
		v := strings.TrimSpace(*in.FirmwareTarget)
		firmwareTarget = &v
	}
	var snmpPort *int
	if in.SnmpPort != nil {
		p := *in.SnmpPort
		if p < 0 || p > 65535 {
			writeError(w, http.StatusBadRequest, "invalid_input", "snmp_port must be between 0 and 65535")
			return
		}
		snmpPort = &p
	}
	var snmpCommunity *string
	if in.SnmpCommunity != nil {
		v := strings.TrimSpace(*in.SnmpCommunity)
		snmpCommunity = &v
	}
	updated, ok := routerstore.UpdateRouter(s.db.DB, id, routerstore.UpdateInput{
		Name: name, Host: host, Type: typ,
		IsGateway: &gw, AgentOnly: &ao,
		FirmwareTarget: firmwareTarget,
		SnmpEnabled: in.SnmpEnabled, SnmpCommunity: snmpCommunity, SnmpPort: snmpPort,
	})
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	s.syncRouters()
	writeJSON(w, http.StatusOK, map[string]any{"router": updated})
}

// --- AdGuard Home (GL.iNet) — solo admin; la contraseña NO se devuelve ---

func kvGet(db *sql.DB, key string) string {
	var v string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&v); err != nil {
		return ""
	}
	return v
}

// GET /api/config/adguard — {mode, host, port, user ('root' por defecto), passSet}.
func (s *server) handleGetAdguardConfig(w http.ResponseWriter, r *http.Request) {
	user := kvGet(s.db.DB, "adguard_user")
	if user == "" {
		user = "root"
	}
	mode := kvGet(s.db.DB, "adguard_mode")
	if mode == "" {
		mode = "glinet"
	}
	portStr := kvGet(s.db.DB, "adguard_port")
	port := 0
	if p, err := strconv.Atoi(portStr); err == nil {
		port = p
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":    mode,
		"host":    kvGet(s.db.DB, "adguard_host"),
		"port":    port,
		"user":    user,
		"passSet": kvGet(s.db.DB, "adguard_pass") != "",
	})
}

type adguardInput struct {
	Mode     string  `json:"mode"`
	Host     *string `json:"host"`
	Port     int     `json:"port"`
	User     string  `json:"user"`
	Password string  `json:"password"`
}

// PUT /api/config/adguard — upsert en kv (password solo si viene). 204.
func (s *server) handlePutAdguardConfig(w http.ResponseWriter, r *http.Request) {
	var in adguardInput
	if st := readJSONBody(w, r, &in); st != 0 {
		writeBodyError(w, st, "invalid_json", "")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "glinet"
	}
	if mode != "glinet" && mode != "standard" {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid enum value. Expected 'glinet' | 'standard'")
		return
	}
	if in.Host == nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Required")
		return
	}
	host := strings.TrimSpace(*in.Host)
	port := 0
	if mode == "standard" {
		hostFromVal, portFromHost, msg := validateAdGuardHost(host)
		if msg != "" {
			writeError(w, http.StatusBadRequest, "invalid_input", msg)
			return
		}
		host = hostFromVal
		if in.Port < 1 || in.Port > 65535 {
			writeError(w, http.StatusBadRequest, "invalid_input", "port must be between 1 and 65535")
			return
		}
		port = in.Port
		if port == 0 {
			port = portFromHost
		}
		if port == 0 {
			port = 3000
		}
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
	if _, err := s.db.Exec(upsert, "adguard_mode", mode); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if _, err := s.db.Exec(upsert, "adguard_host", host); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if mode == "standard" {
		if _, err := s.db.Exec(upsert, "adguard_port", strconv.Itoa(port)); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
	} else {
		if _, err := s.db.Exec("DELETE FROM kv WHERE key = ?", "adguard_port"); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
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
