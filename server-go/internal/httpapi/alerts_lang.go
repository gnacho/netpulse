// alerts_lang.go - ajuste del idioma de las notificaciones push (#889).
// GET devuelve el idioma efectivo + los soportados (los del dist embebido);
// PUT valida contra los soportados y persiste en kv "alerts.lang".
package httpapi

import (
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/alertlang"
	"github.com/gnacho/netpulse/server-go/internal/auth"
)

func (s *server) registerAlertsLangRoutes(mux *http.ServeMux) {
	kv := &dbKVAdapter{db: s.db.DB}
	mux.Handle("GET /api/settings/alerts-lang", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"lang":      alertlang.Lang(kv),
			"supported": alertlang.Supported(),
			"default":   alertlang.DefaultLang,
		})
	})))

	mux.Handle("PUT /api/settings/alerts-lang", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Lang string `json:"lang"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "body JSON inválido")
			return
		}
		supported := map[string]bool{}
		for _, l := range alertlang.Supported() {
			supported[l] = true
		}
		if body.Lang != "" && !supported[body.Lang] {
			writeError(w, http.StatusBadRequest, "invalid_lang", "idioma no soportado")
			return
		}
		lang := body.Lang
		if lang == "" {
			lang = alertlang.DefaultLang
		}
		if err := kv.Set(alertlang.KeyLang, lang); err != nil {
			writeError(w, http.StatusInternalServerError, "kv_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"lang": lang})
	})))
}
