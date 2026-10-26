// thresholds_settings.go - umbrales y retención server-wide (#904, #1034).
// Empezó solo con el de señal débil; el endpoint es genérico para sumar más
// valores de "Datos y umbrales" (retención del log de alertas, etc.) sin
// multiplicar rutas. GET devuelve los valores efectivos; PUT valida y persiste.
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
			"weakSignalDbm":            alerts.WeakSignalDbm(s.db.DB),
			"weakSignalHysteresisDbm":  alerts.WeakSignalHysteresisDbm(s.db.DB),
			"weakSignalResolveSamples": alerts.WeakSignalResolveSamples(s.db.DB),
			"alertRetentionDays":       alerts.RetentionDays(s.db.DB),
			"hideRecoveryEvents":       alerts.HideRecoveryEvents(s.db.DB),
		})
	})))

	mux.Handle("PUT /api/settings/thresholds", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			WeakSignalDbm            *int  `json:"weakSignalDbm"`
			WeakSignalHysteresisDbm  *int  `json:"weakSignalHysteresisDbm"`
			WeakSignalResolveSamples *int  `json:"weakSignalResolveSamples"`
			AlertRetentionDays       *int  `json:"alertRetentionDays"`
			HideRecoveryEvents       *bool `json:"hideRecoveryEvents"`
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
		// #1452: margen de histéresis y muestras consecutivas de la
		// auto-resolución de la alerta de señal débil.
		if body.WeakSignalHysteresisDbm != nil {
			if *body.WeakSignalHysteresisDbm < 1 || *body.WeakSignalHysteresisDbm > 10 {
				writeError(w, http.StatusBadRequest, "invalid_value", "weakSignalHysteresisDbm debe estar entre 1 y 10")
				return
			}
			if err := kv.Set(alerts.KeyWeakSignalHysteresisDbm, strconv.Itoa(*body.WeakSignalHysteresisDbm)); err != nil {
				writeError(w, http.StatusInternalServerError, "kv_error", err.Error())
				return
			}
		}
		if body.WeakSignalResolveSamples != nil {
			if *body.WeakSignalResolveSamples < 1 || *body.WeakSignalResolveSamples > 10 {
				writeError(w, http.StatusBadRequest, "invalid_value", "weakSignalResolveSamples debe estar entre 1 y 10")
				return
			}
			if err := kv.Set(alerts.KeyWeakSignalResolveSamples, strconv.Itoa(*body.WeakSignalResolveSamples)); err != nil {
				writeError(w, http.StatusInternalServerError, "kv_error", err.Error())
				return
			}
		}
		// #1034: retención del log de alertas en días (0 = poda temporal off;
		// la cota de 500 filas sigue aplicando). Efecto sin reinicio: la poda
		// horaria y la de arranque leen el kv en cada pasada.
		if body.AlertRetentionDays != nil {
			if *body.AlertRetentionDays < 0 || *body.AlertRetentionDays > 365 {
				writeError(w, http.StatusBadRequest, "invalid_value", "alertRetentionDays debe estar entre 0 y 365")
				return
			}
			if err := kv.Set(alerts.KeyRetentionDays, strconv.Itoa(*body.AlertRetentionDays)); err != nil {
				writeError(w, http.StatusInternalServerError, "kv_error", err.Error())
				return
			}
		}
		// #1518: ocultar del feed y del contador de no leídas los eventos de
		// recuperación (severity ok). Efecto sin reinicio: List() y
		// UnreadCount() leen el kv en cada llamada; NotifyChange re-emite el
		// feed por SSE para que el front lo vea al instante.
		if body.HideRecoveryEvents != nil {
			val := "0"
			if *body.HideRecoveryEvents {
				val = "1"
			}
			if err := kv.Set(alerts.KeyHideRecovery, val); err != nil {
				writeError(w, http.StatusInternalServerError, "kv_error", err.Error())
				return
			}
			if eng := s.alertsEngine(); eng != nil {
				eng.NotifyChange()
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"weakSignalDbm":            alerts.WeakSignalDbm(s.db.DB),
			"weakSignalHysteresisDbm":  alerts.WeakSignalHysteresisDbm(s.db.DB),
			"weakSignalResolveSamples": alerts.WeakSignalResolveSamples(s.db.DB),
			"alertRetentionDays":       alerts.RetentionDays(s.db.DB),
			"hideRecoveryEvents":       alerts.HideRecoveryEvents(s.db.DB),
		})
	})))
}
