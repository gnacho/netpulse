package httpapi

import (
	"net/http"
)

func (s *server) handlePresenceStatus(w http.ResponseWriter, _ *http.Request) {
	if s.presence == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
		"people":    s.presence.ListPeople(),
		"status":    s.presence.CurrentStatus(),
	})
}

func (s *server) handlePresencePeople(w http.ResponseWriter, _ *http.Request) {
	if s.presence == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.presence.ListPeople())
}
