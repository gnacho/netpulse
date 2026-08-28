package httpapi

import (
	"net/http"
	"strconv"
	"time"
)

func (s *server) handleWiFiSLESummary(w http.ResponseWriter, r *http.Request) {
	if s.wifiSLE == nil {
		writeJSON(w, http.StatusOK, map[string]any{"sles": []any{}, "available": false})
		return
	}
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h > 0 {
			hours = h
		}
	}
	summaries, err := s.wifiSLE.AllSummaries(hours)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sle_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sles": summaries, "available": true, "windowHours": hours})
}

func (s *server) handleWiFiSLESeries(w http.ResponseWriter, r *http.Request) {
	if s.wifiSLE == nil {
		writeError(w, http.StatusServiceUnavailable, "sle_unavailable")
		return
	}
	routerID := r.URL.Query().Get("router")
	if routerID == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "router required")
		return
	}
	now := time.Now().UnixMilli()
	from := now - 86400000
	to := now
	if v := r.URL.Query().Get("from"); v != "" {
		if f, err := strconv.ParseInt(v, 10, 64); err == nil {
			from = f
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := strconv.ParseInt(v, 10, 64); err == nil {
			to = t
		}
	}
	series, err := s.wifiSLE.Series(routerID, from, to)
	if err != nil {
		writeError(w, http.StatusBadRequest, "series_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"router": routerID, "series": series, "from": from, "to": to})
}
