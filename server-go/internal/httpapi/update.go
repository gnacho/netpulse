// update.go — rutas /api/update/* (paridad src/routes/update.js, SPEC §2.18,
// solo admin):
//
//	GET  /api/update/status → estado actual
//	POST /api/update/check  → fuerza un chequeo contra GitHub
//	POST /api/update/apply  → aplica la actualización (202 | 409)
package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/updater"
)

// registerUpdateRoutes registra las rutas /api/update/* (solo si hay updater).
func (s *server) registerUpdateRoutes(mux *http.ServeMux, u *updater.Updater) {
	mux.Handle("GET /api/update/status", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, u.Status())
	})))
	mux.Handle("POST /api/update/check", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		writeJSON(w, http.StatusOK, u.Check(ctx))
	})))
	mux.Handle("POST /api/update/apply", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !u.Apply() {
			// already_updating (o update_copy_failed: mismo 409 que el JS,
			// que solo devuelve false en ambos casos)
			writeError(w, http.StatusConflict, "already_updating")
			return
		}
		writeJSON(w, http.StatusAccepted, u.Status())
	})))
}
