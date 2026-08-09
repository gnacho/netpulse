// settings.go — ajustes globales persistidos en kv (issue #121).
//
// Por ahora solo el flag de orquestación: el menú de orquestación (escritura
// en routers) está oculto por defecto y se muestra solo si el admin lo activa.
package httpapi

import (
	"database/sql"
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/auth"
)

// orchestrationKey es la clave kv que activa el menú de orquestación (#121).
const orchestrationKey = "settings.orchestration_enabled"

// kvGetBool lee un flag "1"/"0" del kv. Ausente o inválido → false.
func kvGetBool(db *sql.DB, key string) bool {
	return kvGet(db, key) == "1" || kvGet(db, key) == "true"
}

// kvSetBool escribe un flag "1"/"0" en el kv (UPSERT).
func kvSetBool(db *sql.DB, key string, val bool) error {
	v := "0"
	if val {
		v = "1"
	}
	_, err := db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, v)
	return err
}

// registerSettingsRoutes: GET/PUT /api/settings/orchestration (admin).
func (s *server) registerSettingsRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/settings/orchestration", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": kvGetBool(s.db.DB, orchestrationKey)})
	})))
	mux.Handle("PUT /api/settings/orchestration", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if !readJSONBody(r, &body) {
			writeError(w, http.StatusBadRequest, "invalid_body")
			return
		}
		if err := kvSetBool(s.db.DB, orchestrationKey, body.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "kv_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": body.Enabled})
	})))
}
