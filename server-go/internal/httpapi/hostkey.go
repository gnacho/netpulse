package httpapi

// hostkey.go — confirmación explícita de re-onboard SSH tras una host key
// cambiada (issue #603). La conexión a un host con clave distinta se rechaza
// (posible MITM); este endpoint, admin-only, borra la entrada known_hosts y
// fuerza un sondeo para que la nueva clave se registre con TOFU.

import (
	"net/http"
)

// handleAcceptHostKey: POST /api/routers/{id}/accept-host-key (admin).
func (s *server) handleAcceptHostKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	acc, ok := s.adapter.(interface {
		AcceptHostKey(routerID string) error
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "el adapter no soporta aceptar host key")
		return
	}
	if err := acc.AcceptHostKey(id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	// Sondeo inmediato: al no quedar entrada, el dial vuelve a hacer TOFU y el
	// router recupera el estado online si la clave nueva es legítima.
	if s.pollNow != nil {
		s.pollNow()
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}
