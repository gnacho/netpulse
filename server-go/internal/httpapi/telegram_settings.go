package httpapi

import (
	"database/sql"
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/telegram"
)

type dbKVAdapter struct {
	db *sql.DB
}

func (a *dbKVAdapter) Get(key string) (string, bool) {
	v := kvGet(a.db, key)
	if v == "" {
		return "", false
	}
	return v, true
}

func (a *dbKVAdapter) Set(key, value string) error {
	_, err := a.db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

type telegramConfigResponse struct {
	BotToken string `json:"botToken"`
	ChatID   string `json:"chatId"`
	Enabled  bool   `json:"enabled"`
	BotName  string `json:"botName,omitempty"`
	ChatName string `json:"chatName,omitempty"`
}

func (s *server) registerTelegramRoutes(mux *http.ServeMux) {
	adapter := &dbKVAdapter{db: s.db.DB}

	mux.Handle("GET /api/settings/telegram", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := telegram.LoadConfig(adapter)
		resp := telegramConfigResponse{
			ChatID:  cfg.ChatID,
			Enabled: cfg.Enabled,
		}
		if cfg.BotToken != "" {
			if len(cfg.BotToken) > 10 {
				resp.BotToken = cfg.BotToken[:6] + "..." + cfg.BotToken[len(cfg.BotToken)-4:]
			} else {
				resp.BotToken = "***"
			}
		}
		writeJSON(w, http.StatusOK, resp)
	})))

	mux.Handle("PUT /api/settings/telegram", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			BotToken string `json:"botToken"`
			ChatID   string `json:"chatId"`
			Enabled  bool   `json:"enabled"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "")
			return
		}

		cfg := telegram.LoadConfig(adapter)
		if body.BotToken != "" {
			cfg.BotToken = body.BotToken
		}
		cfg.ChatID = body.ChatID
		cfg.Enabled = body.Enabled

		if cfg.BotToken == "" || cfg.ChatID == "" {
			writeError(w, http.StatusBadRequest, "invalid_input", "botToken and chatId are required")
			return
		}

		botName, chatName, err := telegram.ValidateConfig(cfg.BotToken, cfg.ChatID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		if err := telegram.SaveConfig(adapter, cfg); err != nil {
			writeError(w, http.StatusInternalServerError, "kv_error")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"botName":  botName,
			"chatName": chatName,
		})
	})))

	mux.Handle("POST /api/settings/telegram/test", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := telegram.SendTest(adapter); err != nil {
			writeError(w, http.StatusBadRequest, "send_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))
}
