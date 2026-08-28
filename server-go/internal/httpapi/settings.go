// settings.go — ajustes globales persistidos en kv (issue #121).
//
// Por ahora el flag de orquestación y la velocidad WAN contratada (issue
// #151): el menú de orquestación (escritura en routers) está oculto por
// defecto y se muestra solo si el admin lo activa.
package httpapi

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// orchestrationKey es la clave kv que activa el menú de orquestación (#121).
const orchestrationKey = "settings.orchestration_enabled"

// Claves kv de la velocidad WAN contratada (#151).
const (
	wanSpeedDownKey = "settings.wan.speed_down"
	wanSpeedUpKey   = "settings.wan.speed_up"
)

// maxContractMbps es el techo de sanidad de la velocidad contratada (1 Gbps
// simétrico → 100000 Mbps; por encima es casi seguro un error de entrada).
const maxContractMbps = 100000.0

// kvGetBool lee un flag "1"/"0" del kv. Ausente o inválido → false.
func kvGetBool(db *sql.DB, key string) bool {
	return kvGet(db, key) == "1" || kvGet(db, key) == "true"
}

// kvSetBool escribe un flag "1"/"0" en el kv (UPSERT).
func kvSetBool(db *sql.DB, key string, val bool) error {
	v := "0"
	if val {
		v = "1"
	}
	_, err := db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, v)
	return err
}

// kvGetFloat lee un número del kv. Devuelve (valor, false) si la clave está
// ausente o no es un número válido (→ "no configurado").
func kvGetFloat(db *sql.DB, key string) (float64, bool) {
	raw := kvGet(db, key)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// kvSetFloat escribe un número en el kv (UPSERT).
func kvSetFloat(db *sql.DB, key string, val float64) error {
	_, err := db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, strconv.FormatFloat(val, 'f', -1, 64))
	return err
}

// wanSpeedResponse es la forma JSON de GET /api/settings/wanspeed. Los campos
// son punteros: null cuando la velocidad no está configurada.
type wanSpeedResponse struct {
	DownMbps *float64 `json:"downMbps"`
	UpMbps   *float64 `json:"upMbps"`
}

// validWanSpeed valida una velocidad contratada (> 0, ≤ maxContractMbps).
func validWanSpeed(v float64) bool {
	return v > 0 && v <= maxContractMbps
}

// registerSettingsRoutes: GET/PUT /api/settings/orchestration (admin) y
// GET/PUT /api/settings/wanspeed (admin, #151).
func (s *server) registerSettingsRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/settings/orchestration", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": kvGetBool(s.db.DB, orchestrationKey)})
	})))
	mux.Handle("PUT /api/settings/orchestration", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "")
			return
		}
		if err := kvSetBool(s.db.DB, orchestrationKey, body.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "kv_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": body.Enabled})
	})))

	// GET /api/settings/wanspeed — velocidad contratada declarada (#151).
	mux.Handle("GET /api/settings/wanspeed", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res := wanSpeedResponse{}
		if v, ok := kvGetFloat(s.db.DB, wanSpeedDownKey); ok {
			res.DownMbps = &v
		}
		if v, ok := kvGetFloat(s.db.DB, wanSpeedUpKey); ok {
			res.UpMbps = &v
		}
		writeJSON(w, http.StatusOK, res)
	})))

	// PUT /api/settings/wanspeed — guarda ambas velocidades (down y up).
	mux.Handle("PUT /api/settings/wanspeed", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body wanSpeedResponse
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "")
			return
		}
		if body.DownMbps == nil || body.UpMbps == nil {
			writeError(w, http.StatusBadRequest, "invalid_input", "downMbps and upMbps are required")
			return
		}
		if !validWanSpeed(*body.DownMbps) || !validWanSpeed(*body.UpMbps) {
			writeError(w, http.StatusBadRequest, "invalid_input", "velocidad contratada debe ser mayor que 0 y como máximo 100000 Mbps")
			return
		}
		if err := kvSetFloat(s.db.DB, wanSpeedDownKey, *body.DownMbps); err != nil {
			writeError(w, http.StatusInternalServerError, "kv_error")
			return
		}
		if err := kvSetFloat(s.db.DB, wanSpeedUpKey, *body.UpMbps); err != nil {
			writeError(w, http.StatusInternalServerError, "kv_error")
			return
		}
		writeJSON(w, http.StatusOK, wanSpeedResponse{DownMbps: body.DownMbps, UpMbps: body.UpMbps})
	})))

	s.registerKnownMacsRoutes(mux)
	s.registerTelegramRoutes(mux)
}

// knownMacItem es la forma JSON de una entrada de la allowlist (#196).
type knownMacItem struct {
	MAC  string `json:"mac"`
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
}

// validMAC normaliza y valida una dirección MAC (AA:BB:CC:DD:EE:FF).
func validMAC(raw string) (string, bool) {
	m := strings.ToUpper(strings.TrimSpace(raw))
	if len(m) != 17 {
		return "", false
	}
	for i := 0; i < 17; i++ {
		if i%3 == 2 {
			if m[i] != ':' {
				return "", false
			}
		} else if !isHex(m[i]) {
			return "", false
		}
	}
	return m, true
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')
}

// registerKnownMacsRoutes: GET/PUT/DELETE /api/settings/known-macs (admin,
// issue #196) — allowlist de dispositivos confiables que no alertan como
// "desconocido" y cuyo nombre se usa como alias.
func (s *server) registerKnownMacsRoutes(mux *http.ServeMux) {
	// GET /api/settings/known-macs — lista completa de la allowlist.
	mux.Handle("GET /api/settings/known-macs", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items, err := s.db.ListKnownMacs()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error")
			return
		}
		out := make([]knownMacItem, 0, len(items))
		for _, k := range items {
			out = append(out, knownMacItem{MAC: k.MAC, Name: k.Name, Note: k.Note})
		}
		writeJSON(w, http.StatusOK, map[string][]knownMacItem{"items": out})
	})))

	// PUT /api/settings/known-macs — alta o actualización de una MAC.
	mux.Handle("PUT /api/settings/known-macs", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body knownMacItem
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "")
			return
		}
		mac, ok := validMAC(body.MAC)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_input", "dirección MAC inválida (AA:BB:CC:DD:EE:FF)")
			return
		}
		if err := s.db.UpsertKnownMac(db.KnownMac{MAC: mac, Name: body.Name, Note: body.Note}); err != nil {
			writeError(w, http.StatusInternalServerError, "db_error")
			return
		}
		writeJSON(w, http.StatusOK, knownMacItem{MAC: mac, Name: body.Name, Note: body.Note})
	})))

	// DELETE /api/settings/known-macs/{mac} — baja de la allowlist.
	mux.Handle("DELETE /api/settings/known-macs/{mac}", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mac, ok := validMAC(r.PathValue("mac"))
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_input", "dirección MAC inválida (AA:BB:CC:DD:EE:FF)")
			return
		}
		if err := s.db.DeleteKnownMac(mac); err != nil {
			writeError(w, http.StatusInternalServerError, "db_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))
}
