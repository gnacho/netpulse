package httpapi

import (
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/auth"
)

func (s *server) handleTokensList(w http.ResponseWriter, r *http.Request) {
	if s.tokenStore == nil {
		writeError(w, http.StatusServiceUnavailable, "tokens_disabled")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	tokens, err := s.tokenStore.List(user.ID, user.Role == "admin")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	type item struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Scope      string `json:"scope"`
		UserID     int64  `json:"userId"`
		CreatedAt  int64  `json:"createdAt"`
		ExpiresAt  int64  `json:"expiresAt"`
		LastUsedAt int64  `json:"lastUsedAt"`
	}
	out := make([]item, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, item{
			ID: t.ID, Name: t.Name, Scope: t.Scope, UserID: t.UserID,
			CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func (s *server) handleTokensCreate(w http.ResponseWriter, r *http.Request) {
	if s.tokenStore == nil {
		writeError(w, http.StatusServiceUnavailable, "tokens_disabled")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Name       string `json:"name"`
		Scope      string `json:"scope"`
		ExpiresIn  int    `json:"expiresInDays"`
	}
	if status := readJSONBody(w, r, &body); status != 0 {
		writeBodyError(w, status, "invalid_body", "")
		return
	}
	if body.Name == "" || body.Scope == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "name and scope required")
		return
	}
	id, raw, err := s.tokenStore.Create(body.Name, body.Scope, user.ID, body.ExpiresIn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "name": body.Name, "scope": body.Scope, "token": raw,
	})
}

func (s *server) handleTokensDelete(w http.ResponseWriter, r *http.Request) {
	if s.tokenStore == nil {
		writeError(w, http.StatusServiceUnavailable, "tokens_disabled")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	if err := s.tokenStore.Delete(id, user.ID, user.Role == "admin"); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
