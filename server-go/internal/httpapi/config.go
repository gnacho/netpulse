// config.go — rutas /api/config/* (paridad src/routes/config.js, SPEC §2.17):
// CRUD de routers (tabla `routers`), clave SSH propia del servidor,
// descubrimiento de la LAN y credenciales AdGuard GL.iNet (kv; la contraseña
// NUNCA se devuelve). Tras cada mutación se sincroniza el adapter
// (SetRouters) sin reiniciar.
package httpapi

import (
	"database/sql"
	"net/http"
	urlpkg "net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/discover"
	"github.com/gnacho/netpulse/server-go/internal/pve"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sshkey"
)

var (
	hostRe     = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
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
	mux.Handle("GET /api/config/proxmox", auth.RequireAdmin(http.HandlerFunc(s.handleGetProxmoxConfig)))
	mux.Handle("PUT /api/config/proxmox", auth.RequireAdmin(http.HandlerFunc(s.handlePutProxmoxConfig)))
	mux.Handle("DELETE /api/config/proxmox/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleDeleteProxmoxConfig)))
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
	SnmpEnabled      *bool   `json:"snmp_enabled"`
	SnmpCommunity    *string `json:"snmp_community"`
	SnmpPort         *int    `json:"snmp_port"`
	SnmpPollInterval *int    `json:"snmp_poll_interval"` // issue #414; segundos
	// SSHPort (issue #605): puerto SSH del router (dropbear en puerto no
	// estándar). Ausente/0 → 22.
	SSHPort *int `json:"ssh_port"`
	// TempThreshold (issue #716): umbral de alerta por temperatura alta (°C).
	// Ausente = no tocar; 0 = reset a default (65); 1..150 = fijar.
	TempThreshold *int `json:"temp_threshold"`
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
	snmpInterval := 60
	if in.SnmpPollInterval != nil {
		snmpInterval = *in.SnmpPollInterval
		if snmpInterval < 10 || snmpInterval > 3600 {
			writeError(w, http.StatusBadRequest, "invalid_input", "snmp_poll_interval must be between 10 and 3600")
			return
		}
	}
	for _, rt := range routerstore.ListRouters(s.db.DB) {
		if rt.Host == host {
			writeError(w, http.StatusConflict, "duplicate_host", "Ya hay un router con "+host)
			return
		}
	}
	sshPort := 0
	if in.SSHPort != nil {
		p := *in.SSHPort
		if p < 1 || p > 65535 {
			writeError(w, http.StatusBadRequest, "invalid_input", "ssh_port must be between 1 and 65535")
			return
		}
		sshPort = p
	}
	created, err := routerstore.AddRouter(s.db.DB, routerstore.AddInput{
		Name: name, Host: host, Type: typ, IsGateway: in.Gateway, AgentOnly: in.AgentOnly,
		FirmwareTarget: firmwareTarget,
		SnmpEnabled:    snmpEnabled, SnmpCommunity: snmpCommunity, SnmpPort: snmpPort, SnmpPollInterval: snmpInterval,
		SSHPort: sshPort,
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
	var snmpPollInterval *int
	if in.SnmpPollInterval != nil {
		v := *in.SnmpPollInterval
		if v < 10 || v > 3600 {
			writeError(w, http.StatusBadRequest, "invalid_input", "snmp_poll_interval must be between 10 and 3600")
			return
		}
		snmpPollInterval = &v
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
	var sshPort *int
	if in.SSHPort != nil {
		p := *in.SSHPort
		if p < 1 || p > 65535 {
			writeError(w, http.StatusBadRequest, "invalid_input", "ssh_port must be between 1 and 65535")
			return
		}
		sshPort = &p
	}
	var tempThreshold *int
	if in.TempThreshold != nil {
		v := *in.TempThreshold
		if v > 150 {
			writeError(w, http.StatusBadRequest, "invalid_input", "temp_threshold must be between 0 and 150")
			return
		}
		tempThreshold = &v
	}
	updated, ok := routerstore.UpdateRouter(s.db.DB, id, routerstore.UpdateInput{
		Name: name, Host: host, Type: typ,
		IsGateway: &gw, AgentOnly: &ao,
		FirmwareTarget: firmwareTarget,
		SnmpEnabled:    in.SnmpEnabled, SnmpCommunity: snmpCommunity, SnmpPort: snmpPort, SnmpPollInterval: snmpPollInterval,
		SSHPort:       sshPort,
		TempThreshold: tempThreshold,
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

// GET /api/config/proxmox - instancias configuradas (#764 multi-endpoint).
// El secret nunca sale: tokenSet por instancia. Los campos planos legacy
// (url/tokenId/tokenSet de la PRIMERA instancia) se mantienen una version
// para no romper frontends cacheados.
func (s *server) handleGetProxmoxConfig(w http.ResponseWriter, r *http.Request) {
	instances := pve.LoadInstances(s.db.DB)
	// Vista sanitizada: el secret JAMAS sale del server (#764).
	view := make([]map[string]string, 0, len(instances))
	for _, in := range instances {
		view = append(view, map[string]string{
			"id": in.ID, "name": in.Name, "url": in.URL, "tokenId": in.TokenID,
		})
	}
	out := map[string]any{
		"instances": view,
		"url":       "",
		"tokenId":   "",
		"tokenSet":  false,
	}
	if len(instances) > 0 {
		out["url"] = instances[0].URL
		out["tokenId"] = instances[0].TokenID
		out["tokenSet"] = instances[0].Secret != ""
	}
	writeJSON(w, http.StatusOK, out)
}

type proxmoxInput struct {
	URL     *string `json:"url"` // nil = no tocar; "" = desactivar
	TokenID string  `json:"tokenId"`
	Secret  string  `json:"secret"` // solo si viene (se conserva el anterior)
}

type proxmoxInstanceInput struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	TokenID string `json:"tokenId"`
	Secret  string `json:"secret"` // vacío en edición = conservar el actual
}

// PUT /api/config/proxmox - upsert de UNA instancia multi (#764): body
// {id, name, url, tokenId, secret?}. Compat: si el body NO lleva id, se
// aplica al estilo legacy single (url/tokenId/secret sobre la instancia
// migrada "default", creándola si hace falta; url "" desactiva todo).
func (s *server) handlePutProxmoxConfig(w http.ResponseWriter, r *http.Request) {
	var in proxmoxInstanceInput
	if st := readJSONBody(w, r, &in); st != 0 {
		writeBodyError(w, st, "invalid_json", "")
		return
	}
	// Parcial (#764): sobre una instancia EXISTENTE, los campos vacíos se
	// conservan (url/tokenId/secret opcionales); para una NUEVA se exigen
	// url + tokenId + secret. Borrar es DELETE (ya no "url vacía").
	tokenID := strings.TrimSpace(in.TokenID)
	if len(tokenID) > 128 || len(in.Secret) > 256 {
		writeError(w, http.StatusBadRequest, "invalid_input", "token too long")
		return
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "default" // legacy sin id
	}
	url := strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if url != "" {
		u, err := urlpkg.Parse(url)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			writeError(w, http.StatusBadRequest, "invalid_input", "url must be a valid http(s) URL")
			return
		}
	}
	list := pve.LoadInstances(s.db.DB)
	var current *pve.Instance
	for i := range list {
		if list[i].ID == id {
			current = &list[i]
			break
		}
	}
	if current != nil {
		if url == "" && tokenID == "" && strings.TrimSpace(in.Name) == "" && in.Secret == "" {
			writeError(w, http.StatusBadRequest, "invalid_input", "nada que actualizar")
			return
		}
		if url != "" {
			current.URL = url
		}
		if tokenID != "" {
			current.TokenID = tokenID
		}
		if in.Secret != "" {
			current.Secret = in.Secret
		}
		if n := strings.TrimSpace(in.Name); n != "" {
			current.Name = n
		}
		if err := pve.SaveInstances(s.db.DB, list); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Instancia nueva: completa.
	if url == "" || tokenID == "" || in.Secret == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "url, tokenId y secret son requeridos para una instancia nueva")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = id
	}
	inst := pve.Instance{ID: id, Name: name, Config: pve.Config{URL: url, TokenID: tokenID, Secret: in.Secret}}
	if err := pve.UpsertInstance(s.db.DB, inst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/config/proxmox/{id} - elimina una instancia (#764).
func (s *server) handleDeleteProxmoxConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !pve.ValidInstanceID(id) {
		writeError(w, http.StatusBadRequest, "invalid_input", "id inválido")
		return
	}
	if err := pve.DeleteInstance(s.db.DB, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
