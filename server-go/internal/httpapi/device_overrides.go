// device_overrides.go — mutaciones manuales de dispositivo (issue #437):
//
//	PUT /api/devices/{mac}/override → alias + icono.
//	PUT /api/devices/{mac}/ban    → bandas a bloquear/desbloquear (Fase 2).
package httpapi

import (
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var macNormalizeRe = regexp.MustCompile(`[^0-9a-fA-F]`)

// normalizeMAC devuelve MAC en minúsculas con ':' (la forma canónica del
// server y de device_overrides).
func normalizeMAC(mac string) string {
	hex := macNormalizeRe.ReplaceAllString(mac, "")
	hex = strings.ToLower(hex)
	var out strings.Builder
	for i := 0; i < len(hex); i += 2 {
		if i > 0 {
			out.WriteByte(':')
		}
		if i+2 <= len(hex) {
			out.WriteString(hex[i : i+2])
		} else {
			out.WriteString(hex[i:])
		}
	}
	return out.String()
}

// validateIcon comprueba que el icono enviado es uno de los permitidos por
// DEVICE_ICONS del frontend. Lista cerrada para evitar inyección de clases.
func validateIcon(icon string) bool {
	if icon == "" {
		return true
	}
	allowed := map[string]bool{
		"monitor": true, "laptop": true, "smartphone": true, "tablet": true,
		"tv": true, "gamepad": true, "camera": true, "speaker": true,
		"router": true, "server": true, "watch": true, "car": true,
		"home": true, "printer": true, "plug": true, "shield": true,
		"help-circle": true, "wifi": true, "ethernet": true,
	}
	return allowed[icon]
}

// handleDeviceOverridePut: body {"name":"...","icon":"..."}. Vacío borra.
func (s *server) handleDeviceOverridePut(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if mac == "" || len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	var body struct {
		Name string `json:"name"`
		Icon string `json:"icon"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if !validateIcon(body.Icon) {
		writeError(w, http.StatusBadRequest, "invalid_icon")
		return
	}
	now := time.Now().UnixMilli()
	if body.Name == "" && body.Icon == "" {
		_, _ = s.db.Exec("DELETE FROM device_overrides WHERE mac = ?", mac)
	} else {
		_, err := s.db.Exec(
			`INSERT INTO device_overrides (mac, name, icon, banned_bands, created_at, updated_at)
			 VALUES (?, ?, ?, '', ?, ?)
			 ON CONFLICT(mac) DO UPDATE SET name=excluded.name, icon=excluded.icon, updated_at=excluded.updated_at`,
			mac, body.Name, body.Icon, now, now,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
	}
	// Dispara un sondeo para que el cambio se refleje en el próximo buildDevices.
	if s.pollNow != nil {
		s.pollNow()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac})
}

// handleDeviceOverrideGet: devuelve el override actual de un dispositivo.
func (s *server) handleDeviceOverrideGet(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if mac == "" || len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	var name, icon, banned sql.NullString
	row := s.db.QueryRow("SELECT name, icon, banned_bands FROM device_overrides WHERE mac = ?", mac)
	if err := row.Scan(&name, &icon, &banned); err != nil && err != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mac":         mac,
		"name":        name.String,
		"icon":        icon.String,
		"bannedBands": banned.String,
	})
}

// handleDeviceBanPut: body {"bands":["2.4","5","6","all"]} o {"bands":[]}.
// Aplica la política de MAC filter en el router asociado (Fase 2 del issue #437).
func (s *server) handleDeviceBanPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Bands []string `json:"bands"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	_ = body
	writeError(w, http.StatusNotImplemented, "not_implemented", "baneo por banda: Fase 2 del issue #437")
}
