package httpapi

import (
	"net/http"
)

func (s *server) handleInternetHealth(w http.ResponseWriter, _ *http.Request) {
	if s.internetHealth == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
		"summary":   s.internetHealth.Summary(),
		"probes":    s.internetHealth.RecentProbes(24),
		"outages":   s.internetHealth.RecentOutages(10),
	})
}
