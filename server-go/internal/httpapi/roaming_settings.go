// roaming_settings.go - cadencia de ingesta de los eventos de roaming (#907).
// La retención del histórico reutiliza el ajuste existente presence.retention
// (UI en Ajustes + loop de poda en main); aquí solo el intervalo del
// collector. GET devuelve los valores efectivos; PUT valida y persiste.
package httpapi

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/roamcfg"
)

// roamingSettingsResponse: los dos valores que la cabecera de la tabla de
// eventos necesita para describirse con datos reales (#907).
func roamingSettings(db *sql.DB) map[string]any {
	return map[string]any{
		"retentionDays":      PresenceRetentionDays(db),
		"collectIntervalSec": roamcfg.CollectIntervalSec(db),
	}
}

func (s *server) registerRoamingSettingsRoutes(mux *http.ServeMux) {
	kv := &dbKVAdapter{db: s.db.DB}
	mux.Handle("GET /api/settings/roaming", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, roamingSettings(s.db.DB))
	})))

	mux.Handle("PUT /api/settings/roaming", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CollectIntervalSec *int `json:"collectIntervalSec"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "body JSON inválido")
			return
		}
		if body.CollectIntervalSec != nil {
			if *body.CollectIntervalSec < 15 || *body.CollectIntervalSec > 3600 {
				writeError(w, http.StatusBadRequest, "invalid_value", "collectIntervalSec debe estar entre 15 y 3600")
				return
			}
			if err := kv.Set(roamcfg.KeyCollectInterval, strconv.Itoa(*body.CollectIntervalSec)); err != nil {
				writeError(w, http.StatusInternalServerError, "kv_error", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, roamingSettings(s.db.DB))
	})))
}
