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

		// #1080: solo Fresh() convertía cualquier hueco de frescura del
		// agente (reconexión, backhaul, reinicio) en "no reporta radios".
		// Es una vista de análisis: mejor el último payload conocido aunque
		// pase el TTL que fingir que el equipo no tiene WiFi. El aviso solo
		// tiene sentido si no existe payload alguno (o no trae wireless).
		var radios []probe.Radio
		if s.agents != nil {
			payload, ok := s.agents.Fresh(slug)
			if !ok {
				payload, _ = s.agents.StalePayload(slug)
			}
			if payload != nil && payload.Data.Wireless != nil {
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
		// #1214: flotas con NETPULSE_SCAN_INTERVAL=0 no escanean nunca por
		// intervalo - con la ventana fija de 24h la vista moría en cuanto el
		// último scan cumplía un día (solo quedaba el bloque propio
		// sintético y un espectro vacío). Misma filosofía que #1080 con los
		// radios: mejor el último dato conocido, aunque sea antiguo, que
		// fingir que no hay nada; el "Escanear ahora" lo renueva al momento.
		if len(scans) == 0 {
			scans, err = s.channelPlan.RecentScans(slug, 30*24*time.Hour)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "channel_plan_error")
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"routerId": routerID,
			"radios":   recommendations,
			"scans":    scans,
		})
	})))

	// #1214: scan ad hoc para flotas con NETPULSE_SCAN_INTERVAL=0. El evento
	// "refresh" fuerza en el agente un ciclo con ForceScan (el throttle del
	// intervalo no aplica) y el push siguiente trae los vecinos nuevos a
	// wifi_scans. Requiere el agente conectado por SSE.
	mux.Handle("POST /api/wifi/channel-plan/scan", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routerID := r.URL.Query().Get("routerId")
		if routerID == "" {
			writeError(w, http.StatusBadRequest, "invalid_body", "routerId requerido")
			return
		}
		slug := agentSlugForRouter(routerID)
		if s.agentHub == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "SSE agentHub no configurado")
			return
		}
		if !s.agentHub.Send(slug, "refresh", map[string]any{}) {
			writeError(w, http.StatusNotFound, "not_found", "el agente no está conectado por SSE")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	})))
}
