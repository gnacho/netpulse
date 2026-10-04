package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/auth"
)

// registerChannelPlanRoutes añade los endpoints de channel planning (#452).
func (s *server) registerChannelPlanRoutes(mux *http.ServeMux) {
	if s.channelPlan == nil {
		return
	}

	// agentSlugForRouter resuelve la clave que usa la UI (el id del router
	// en el overview: hostname o nombre amigable) al slug del agente bajo el
	// que viven Fresh() y wifi_scans (#475): la flota manda flint2/rt2 y los
	// agentes se llaman gateway/redmi-ax6. Tres capas: slug directo,
	// hostname del board del último payload, e id/nombre de la tabla routers
	// resuelto con resolveAgentRouter.
	agentSlugForRouter := func(routerID string) string {
		norm := func(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
		routerByID := map[string]routerIdentity{}
		slugs := []string{}
		if s.db != nil {
			if rows, err := s.db.Query("SELECT id, type, name, host, COALESCE(mac,'') FROM routers"); err == nil {
				for rows.Next() {
					var id, typ, name, host, mac string
					if rows.Scan(&id, &typ, &name, &host, &mac) == nil {
						routerByID[id] = routerIdentity{ID: id, Type: typ, Name: name, Host: host, Mac: mac}
					}
				}
				rows.Close()
			}
			if rows, err := s.db.Query("SELECT key FROM kv WHERE key LIKE ?", agentTokenKeyPrefix+"%"); err == nil {
				for rows.Next() {
					var key string
					if rows.Scan(&key) == nil {
						slugs = append(slugs, strings.TrimPrefix(key, agentTokenKeyPrefix))
					}
				}
				rows.Close()
			}
		}
		want := norm(routerID)
		for _, slug := range slugs {
			if slug == routerID {
				return slug
			}
			var payload *probe.Payload
			if s.agents != nil {
				if st := s.agents.Snapshot(slug); st != nil {
					payload = st.Payload
				}
			}
			if payload != nil && payload.Data.System != nil && payload.Data.System.Board != nil {
				if norm(payload.Data.System.Board.Hostname) == want {
					return slug
				}
			}
			if id := resolveAgentRouter(slug, payload, routerByID); id != "" {
				if id == routerID || norm(routerByID[id].Name) == want || norm(routerByID[id].Host) == want {
					return slug
				}
			}
		}
		return routerID
	}

	mux.Handle("GET /api/wifi/channel-plan", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routerID := r.URL.Query().Get("routerId")
		if routerID == "" {
			writeError(w, http.StatusBadRequest, "invalid_body", "routerId requerido")
			return
		}
		slug := agentSlugForRouter(routerID)

		var radios []probe.Radio
		if s.agents != nil {
			if payload, ok := s.agents.Fresh(slug); ok && payload.Data.Wireless != nil {
				radios = payload.Data.Wireless.Radios
			}
		}

		recommendations, err := s.channelPlan.Recommend(slug, radios, 24*time.Hour)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "channel_plan_error")
			return
		}
		scans, err := s.channelPlan.RecentScans(slug, 24*time.Hour)
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
