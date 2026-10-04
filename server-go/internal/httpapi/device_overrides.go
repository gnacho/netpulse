// device_overrides.go — mutaciones manuales de dispositivo (issue #437,
// #797):
//
//	PUT /api/devices/{mac}/override → icono, nombre visible y/o tipo.
//	PUT /api/devices/{mac}/ban    → bandas a bloquear/desbloquear (Fase 2).
//
// Semántica del PUT: cada campo es opcional; ausente = no tocar, vacío =
// limpiar ese override. Así los clientes antiguos que solo envían {icon}
// (p. ej. el alta rápida de desconocidos, #772) no borran nombre/tipo.
package httpapi

import (
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
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

// deviceNameMax cota el nombre visible personalizado (#797).
const deviceNameMax = 64

// handleDeviceOverridePut: campos opcionales {"icon","name","type"}; vacío
// limpia, ausente no toca. Si los tres quedan vacíos la fila se borra.
func (s *server) handleDeviceOverridePut(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if mac == "" || len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	var body struct {
		Icon *string `json:"icon"`
		Name *string `json:"name"`
		Type *string `json:"type"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if body.Icon != nil && !validateIcon(*body.Icon) {
		writeError(w, http.StatusBadRequest, "invalid_icon")
		return
	}
	if body.Type != nil && *body.Type != "" && !adapters.ValidDeviceTypes[*body.Type] {
		writeError(w, http.StatusBadRequest, "invalid_type")
		return
	}
	name := ""
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
		if strings.ContainsAny(name, "\r\n") {
			writeError(w, http.StatusBadRequest, "invalid_name")
			return
		}
		if len(name) > deviceNameMax {
			name = name[:deviceNameMax]
		}
	}

	// Valores actuales (los campos ausentes conservan lo persistido).
	var curIcon sql.NullString
	var curName, curType string
	row := s.db.QueryRow("SELECT icon, name, device_type FROM device_overrides WHERE mac = ?", mac)
	switch err := row.Scan(&curIcon, &curName, &curType); err {
	case nil:
	case sql.ErrNoRows:
	default:
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	nextIcon := curIcon.String
	nextName, nextType := curName, curType
	if body.Icon != nil {
		nextIcon = *body.Icon
	}
	if body.Name != nil {
		nextName = name
	}
	if body.Type != nil {
		nextType = *body.Type
	}

	now := time.Now().UnixMilli()
	if nextIcon == "" && nextName == "" && nextType == "" {
		_, _ = s.db.Exec("DELETE FROM device_overrides WHERE mac = ?", mac)
	} else {
		_, err := s.db.Exec(
			`INSERT INTO device_overrides (mac, icon, name, device_type, banned_bands, created_at, updated_at)
			 VALUES (?, ?, ?, ?, '', ?, ?)
			 ON CONFLICT(mac) DO UPDATE SET icon=excluded.icon, name=excluded.name,
			   device_type=excluded.device_type, updated_at=excluded.updated_at`,
			mac, nextIcon, nextName, nextType, now, now,
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
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "mac": mac, "icon": nextIcon, "name": nextName, "type": nextType,
	})
}

// handleDeviceOverrideGet: devuelve el override actual de un dispositivo.
func (s *server) handleDeviceOverrideGet(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if mac == "" || len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	var icon, name, deviceType, banned sql.NullString
	row := s.db.QueryRow("SELECT icon, name, device_type, banned_bands FROM device_overrides WHERE mac = ?", mac)
	if err := row.Scan(&icon, &name, &deviceType, &banned); err != nil && err != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mac":         mac,
		"icon":        icon.String,
		"name":        name.String,
		"type":        deviceType.String,
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
