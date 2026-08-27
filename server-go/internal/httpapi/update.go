// update.go — rutas /api/update/* (paridad src/routes/update.js, SPEC §2.18,
// solo admin):
//
//	GET  /api/update/status          → estado actual (incl. readiness + pendingApply)
//	POST /api/update/check           → fuerza un chequeo contra GitHub
//	POST /api/update/apply           → aplica la actualización (202 | 409)
//	GET  /api/update/stream          → SSE con el estado en cada cambio (issue #280)
//	GET  /api/updates/history        → historial de updates (issue #159)
//	POST /api/update/pending-confirm → descarta la confirmación post-update (issue #161)
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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
	// Stream SSE del estado del update (issue #280): evento inicial con el
	// estado completo, luego un evento por cambio de paso/progreso. El
	// heartbeat de 15 s mantiene la conexión viva; el stream MUERE con el
	// proceso durante el reinicio final (el cliente debe tratarlo como
	// fase "restarting" y sondear /api/health).
	mux.Handle("GET /api/update/stream", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming_unsupported")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		ch, cancel := u.Subscribe()
		defer cancel()
		writeEvent := func(st updater.Status) {
			raw, err := json.Marshal(st)
			if err != nil {
				return
			}
			fmt.Fprintf(w, "event: update\ndata: %s\n\n", raw)
			flusher.Flush()
		}
		writeEvent(u.Status())
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				// Comentario SSE: mantiene viva la conexión sin evento.
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			case st := <-ch:
				writeEvent(st)
			}
		}
	})))
	mux.Handle("POST /api/update/check", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		writeJSON(w, http.StatusOK, u.Check(ctx))
	})))
	mux.Handle("POST /api/update/apply", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !u.CanApply() {
			// Layout install.sh (single-binary): sin deploy/update.sh ni
			// permisos para swap del binario. El usuario debe re-ejecutar
			// install.sh (la vía documentada para estables).
			writeError(w, http.StatusConflict, "update_unavailable")
			return
		}
		if !u.Apply() {
			writeError(w, http.StatusConflict, "already_updating")
			return
		}
		writeJSON(w, http.StatusAccepted, u.Status())
	})))
	// Historial de updates (issue #159): los últimos N registros, por ahora
	// sin paginar (cota de 200 en ListHistory).
	mux.Handle("GET /api/updates/history", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		history, err := updater.ListHistory(s.db.DB, 50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "history_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"history": history})
	})))
	// Descarta la confirmación post-update tras mostrarla (issue #161).
	mux.Handle("POST /api/update/pending-confirm", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.AckPending()
		w.WriteHeader(http.StatusNoContent)
	})))
}
