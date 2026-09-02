package httpapi

import (
	"net/http"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/auth"
)

// registerChannelPlanRoutes añade los endpoints de channel planning (#452).
func (s *server) registerChannelPlanRoutes(mux *http.ServeMux) {
	if s.channelPlan == nil {
		return
	}

	mux.Handle("GET /api/wifi/channel-plan", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routerID := r.URL.Query().Get("routerId")
		if routerID == "" {
			writeError(w, http.StatusBadRequest, "invalid_body", "routerId requerido")
			return
		}

		var radios []probe.Radio
		if s.agents != nil {
			if payload, ok := s.agents.Fresh(routerID); ok && payload.Data.Wireless != nil {
				radios = payload.Data.Wireless.Radios
			}
		}

		recommendations, err := s.channelPlan.Recommend(routerID, radios, 24*time.Hour)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "channel_plan_error")
			return
		}
		scans, err := s.channelPlan.RecentScans(routerID, 24*time.Hour)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "channel_plan_error")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"routerId": routerID,
			"radios":   recommendations,
			"scans":    scans,
		})
	})))
}
