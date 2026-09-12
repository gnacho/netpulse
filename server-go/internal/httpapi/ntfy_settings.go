// ntfy_settings.go - ajustes del canal ntfy (#766): GET (sin token nunca),
// PUT (token vacío = conservar) y test de publicación real.
package httpapi

import (
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/ntfy"
)

type ntfyConfigResponse struct {
	Server   string `json:"server"`
	Topic    string `json:"topic"`
	TokenSet bool   `json:"tokenSet"`
	Enabled  bool   `json:"enabled"`
}

func (s *server) registerNtfyRoutes(mux *http.ServeMux) {
	adapter := &dbKVAdapter{db: s.db.DB}

	mux.Handle("GET /api/settings/ntfy", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := ntfy.LoadConfig(adapter)
		writeJSON(w, http.StatusOK, ntfyConfigResponse{
			Server:   cfg.Server,
			Topic:    cfg.Topic,
			TokenSet: cfg.Token != "",
			Enabled:  cfg.Enabled,
		})
	})))

	mux.Handle("PUT /api/settings/ntfy", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Server  string `json:"server"`
			Topic   string `json:"topic"`
			Token   string `json:"token"`
			Enabled bool   `json:"enabled"`
			// Clear: desactivar y limpiar el canal entero (como el disable de
			// telegram): el PUT normal con enabled=false solo apaga.
			Clear bool `json:"clear"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			return
		}
		cfg := ntfy.LoadConfig(adapter)
		if body.Clear {
			cfg = ntfy.Config{Server: ntfy.DefaultServer}
		} else {
			if body.Server != "" {
				cfg.Server = body.Server
			}
			if body.Topic != "" {
				cfg.Topic = body.Topic
			}
			if body.Token != "" {
				cfg.Token = body.Token
			}
			cfg.Enabled = body.Enabled
		}
		if err := ntfy.SaveConfig(adapter, cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))

	mux.Handle("POST /api/settings/ntfy/test", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := ntfy.SendTest(adapter); err != nil {
			writeError(w, http.StatusBadRequest, "ntfy_test_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))
}
