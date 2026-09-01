package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
)

// handleReanchorRecommendations: GET /api/wifi-reanchor/recommendations
// Devuelve la lista de clientes WiFi que estarían mejor en otro AP según
// usteer (preferido) o DAWN. Requiere sesión de admin.
func (s *server) handleReanchorRecommendations(w http.ResponseWriter, r *http.Request) {
	cfg := adapters.ReanchorConfig{
		MinRecommendedSignal: -65,
		MinDeltaDbm:          10,
	}
	recs, daemon, err := s.adapter.GetReanchorRecommendations(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if recs == nil {
		recs = []adapters.ReanchorRecommendation{}
	}
	writeJSON(w, http.StatusOK, adapters.ReanchorResponse{Daemon: daemon, Recommendations: recs})
}

// handleReanchorMove: POST /api/wifi-reanchor/{mac}/move
// Ejecuta del_client + ban_time en el AP donde está asociada la MAC,
// usando el daemon activo para decidir el target. Requiere admin.
func (s *server) handleReanchorMove(w http.ResponseWriter, r *http.Request) {
	mac, ok := validMAC(r.PathValue("mac"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	mac = strings.ToUpper(mac)

	cfg := adapters.ReanchorConfig{
		MinRecommendedSignal: -65,
		MinDeltaDbm:          10,
	}
	recs, daemon, err := s.adapter.GetReanchorRecommendations(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	var rec *adapters.ReanchorRecommendation
	for i := range recs {
		if recs[i].MAC == mac {
			rec = &recs[i]
			break
		}
	}
	if rec == nil {
		writeError(w, http.StatusConflict, "no_recommendation", "No hay recomendación activa para este cliente")
		return
	}

	switch daemon {
	case adapters.RoamingDaemonUsteer:
		if err := s.adapter.KickUsteerClient(r.Context(), mac); err != nil {
			writeError(w, http.StatusBadGateway, "ssh_failed", err.Error())
			return
		}
	case adapters.RoamingDaemonDawn:
		if rec.CurrentHost == "" {
			writeError(w, http.StatusConflict, "router_unknown", "No se encontró el router del AP actual")
			return
		}
		script := adapters.ReanchorKickScript(rec.MAC, rec.CurrentIface)
		if _, err := s.pool.Run(rec.CurrentHost, script, 15*time.Second); err != nil {
			writeError(w, http.StatusBadGateway, "ssh_failed", err.Error())
			return
		}
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable", "No hay daemon de roaming disponible")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac, "from": rec.CurrentHostname})
}

// registerReanchorRoutes registra las rutas de re-anclaje WiFi.
func (s *server) registerReanchorRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/wifi-reanchor/recommendations", auth.RequireAdmin(http.HandlerFunc(s.handleReanchorRecommendations)))
	mux.Handle("POST /api/wifi-reanchor/{mac}/move", auth.RequireAdmin(http.HandlerFunc(s.handleReanchorMove)))
}
