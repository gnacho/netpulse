// speedtest_api.go — rutas del test de velocidad WAN (issue #511).
//
// GET  /api/speedtest/status   foto: running, último error/resultado, next.
// GET  /api/speedtest/history  serie de la ventana ?hours= (default 168).
// POST /api/speedtest/run      lanza un test manual (202; 409 si ya corre).
// GET/PUT /api/settings/speedtest  configuración (intervalo, servidor,
//                                 umbral de alerta) — admin.
//
// Deps.Speedtest nil (demo) → 503 unavailable, como el resto de deps nulas.
package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/speedtest"
)

// historyDefaultHours: una semana hacia atrás por defecto (1-4 puntos/día).
const (
	historyDefaultHours = 168
	historyMaxHours     = 365 * 24
)

func (s *server) registerSpeedtestRoutes(mux *http.ServeMux) {
	if s.speedtest == nil {
		for _, route := range []string{
			"GET /api/speedtest/status", "GET /api/speedtest/history",
			"POST /api/speedtest/run",
			"GET /api/settings/speedtest", "PUT /api/settings/speedtest",
		} {
			mux.Handle(route, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeError(w, http.StatusServiceUnavailable, "unavailable",
					"speedtest no disponible en este modo")
			}))
		}
		return
	}

	mux.HandleFunc("GET /api/speedtest/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.speedtest.Status())
	})

	mux.HandleFunc("GET /api/speedtest/history", func(w http.ResponseWriter, r *http.Request) {
		hours := historyDefaultHours
		if v := r.URL.Query().Get("hours"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 || n > historyMaxHours {
				writeError(w, http.StatusBadRequest, "invalid_input",
					"hours debe ser un entero entre 1 y 8760")
				return
			}
			hours = n
		}
		now := time.Now()
		items, err := s.speedtest.Store().History(now.Add(-time.Duration(hours)*time.Hour), now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})

	mux.Handle("POST /api/speedtest/run", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.speedtest.RunNow(); err != nil {
			if errors.Is(err, speedtest.ErrAlreadyRunning) {
				writeError(w, http.StatusConflict, "already_running",
					"ya hay un test en marcha")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
	})))

	mux.Handle("GET /api/settings/speedtest", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.speedtest.LoadSettings())
	})))

	mux.Handle("PUT /api/settings/speedtest", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body speedtest.Settings
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "")
			return
		}
		if err := s.speedtest.SaveSettings(body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s.speedtest.LoadSettings())
	})))
}
