package httpapi

import (
	"net/http"
)

func (s *server) handlePathSummaries(w http.ResponseWriter, r *http.Request) {
	if s.pathAnalysis == nil {
		writeJSON(w, http.StatusOK, map[string]any{"summaries": []any{}, "available": false})
		return
	}
	routerID := r.URL.Query().Get("router")
	if routerID == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "router required")
		return
	}
	summaries, err := s.pathAnalysis.Summaries(routerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "path_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"summaries": summaries, "available": true})
}

func (s *server) handlePathLatest(w http.ResponseWriter, r *http.Request) {
	if s.pathAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "path_unavailable")
		return
	}
	routerID := r.URL.Query().Get("router")
	dest := r.URL.Query().Get("destination")
	if routerID == "" || dest == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "router and destination required")
		return
	}
	result, err := s.pathAnalysis.Latest(routerID, dest)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) handlePathHistory(w http.ResponseWriter, r *http.Request) {
	if s.pathAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "path_unavailable")
		return
	}
	routerID := r.URL.Query().Get("router")
	dest := r.URL.Query().Get("destination")
	if routerID == "" || dest == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "router and destination required")
		return
	}
	results, err := s.pathAnalysis.History(routerID, dest, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "path_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *server) handlePathDestinations(w http.ResponseWriter, r *http.Request) {
	if s.pathAnalysis == nil {
		writeJSON(w, http.StatusOK, map[string]any{"destinations": []any{}, "available": false})
		return
	}
	dests, err := s.pathAnalysis.AllDestinations()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "path_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"destinations": dests, "available": true})
}
