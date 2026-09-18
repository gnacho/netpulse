// onboarding.go — alta y despido rápido de dispositivos desconocidos (#772).
//
// El alta (nombre + icono + reserva opcional) reutiliza los endpoints
// existentes (known-macs, /api/devices/{mac}/override, /api/devices/{mac}/
// reservation): esta pieza solo añade el "dejar como anónimo", que silencia
// la alerta de desconocido para una MAC sin darle nombre ni confiarza.
package httpapi

import (
	"net/http"
	"strings"
)

// handleOnboardingDismiss silencia la alerta "dispositivo desconocido" para
// la MAC pedida (#772): la marca como ya avisada en kv (sobrevive a
// reinicios, issue #248) y en la memoria del adapter para que el ciclo de
// sondeo actual no vuelva a alertar.
func (s *server) handleOnboardingDismiss(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MAC string `json:"mac"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	mac := normalizeMAC(body.MAC)
	if len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	// La convención de memoria del adapter es MAC en MAYÚSCULAS (las MAC de
	// los dispositivos viajan así); la clave kv debe coincidir para que la
	// carga de arranque (unknownAlerted) case con trackUnknownDevices.
	mac = strings.ToUpper(mac)
	if s.db != nil {
		_, _ = s.db.Exec("INSERT INTO kv (key, value) VALUES (?, '1') ON CONFLICT(key) DO NOTHING", "unknown_alerted:"+mac)
	}
	if s.adapter != nil {
		s.adapter.DismissUnknownDevice(mac)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac})
}
