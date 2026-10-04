// thresholds_settings.go - umbrales server-wide (#904). Hoy solo el de señal
// débil; el endpoint es genérico para sumar más (latencia WAN, etc.) sin
// multiplicar rutas. GET devuelve el valor efectivo; PUT valida y persiste.
package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/auth"
)

func (s *server) registerThresholdsRoutes(mux *http.ServeMux) {
	kv := &dbKVAdapter{db: s.db.DB}
	mux.Handle("GET /api/settings/thresholds", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"weakSignalDbm": alerts.WeakSignalDbm(s.db.DB),
		})
	})))

	mux.Handle("PUT /api/settings/thresholds", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			WeakSignalDbm *int `json:"weakSignalDbm"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "body JSON inválido")
			return
		}
		if body.WeakSignalDbm != nil {
			if *body.WeakSignalDbm < -90 || *body.WeakSignalDbm > -50 {
				writeError(w, http.StatusBadRequest, "invalid_value", "weakSignalDbm debe estar entre -90 y -50")
				return
			}
			if err := kv.Set(alerts.KeyWeakSignalDbm, strconv.Itoa(*body.WeakSignalDbm)); err != nil {
				writeError(w, http.StatusInternalServerError, "kv_error", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"weakSignalDbm": alerts.WeakSignalDbm(s.db.DB),
		})
	})))
}
