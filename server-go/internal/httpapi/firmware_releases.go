// firmware_releases.go - endpoints de disponibilidad de releases (#1444).
//
// GET /api/firmware/releases: lista cacheada de versiones publicadas (la
// misma fuente que alimenta el gating de la alerta y, en unidades sin owut,
// el desplegable de target).
// POST /api/firmware/releases/refresh: fuerza el chequeo ahora.
// PUT /api/firmware/releases/settings: cadencia del chequeo (horas).
package httpapi

import (
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
)

func (s *server) registerFirmwareReleasesRoutes(mux *http.ServeMux) {
	if s.fwReleases == nil {
		return
	}

	mux.Handle("GET /api/firmware/releases", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if s.db == nil {
			writeError(w, http.StatusServiceUnavailable, "no_db")
			return
		}
		st, ok := firmware.LoadReleasesRecord(s.db.DB)
		versions := []string{}
		if len(st.Versions) > 0 {
			versions = st.Versions
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"versions":      versions,
			"ts":            st.Ts,
			"available":     ok && len(st.Versions) > 0,
			"intervalHours": firmware.ReleasesIntervalHours(s.db.DB),
			// #1478: el último intento puede haber fallado sin tirar la lista
			// buena; la tarjeta lo distingue del último chequeo exitoso.
			"lastAttemptTs":    st.LastAttemptTs,
			"lastError":        st.LastError,
			"failCount":        st.FailCount,
			"nextRetrySeconds": int(firmware.ReleasesBackoff(st.FailCount).Seconds()),
		})
	})))

	mux.Handle("POST /api/firmware/releases/refresh", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.db == nil {
			writeError(w, http.StatusServiceUnavailable, "no_db")
			return
		}
		if err := s.fwReleases.RunNow(r.Context()); err != nil {
			writeError(w, http.StatusBadGateway, "fetch_failed", err.Error())
			return
		}
		st, _ := firmware.LoadReleases(s.db.DB)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "versions": len(st.Versions), "ts": st.Ts})
	})))

	mux.Handle("PUT /api/firmware/releases/settings", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.db == nil {
			writeError(w, http.StatusServiceUnavailable, "no_db")
			return
		}
		var body struct {
			IntervalHours *int `json:"intervalHours"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "body JSON inválido")
			return
		}
		if body.IntervalHours != nil {
			if err := firmware.SetReleasesIntervalHours(s.db.DB, *body.IntervalHours); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_value", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"intervalHours": firmware.ReleasesIntervalHours(s.db.DB),
		})
	})))
}
