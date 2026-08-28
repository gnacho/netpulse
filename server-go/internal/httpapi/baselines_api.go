package httpapi

import (
	"net/http"
)

func (s *server) handleBaselines(w http.ResponseWriter, _ *http.Request) {
	if s.baselines == nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, s.baselines.Snapshot())
}
