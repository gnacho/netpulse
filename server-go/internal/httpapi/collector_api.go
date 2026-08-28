package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/collectorreader"
)

func (s *server) handleCollectorMetrics(w http.ResponseWriter, r *http.Request) {
	if s.collectorReader == nil {
		writeJSON(w, http.StatusOK, map[string]any{"metrics": []any{}, "available": false})
		return
	}
	metrics, err := s.collectorReader.ListMetrics()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "collector_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": metrics, "available": true})
}

func (s *server) handleCollectorSeries(w http.ResponseWriter, r *http.Request) {
	if s.collectorReader == nil {
		writeError(w, http.StatusServiceUnavailable, "collector_unavailable")
		return
	}
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "metric required")
		return
	}
	now := time.Now().Unix()
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	pointsStr := r.URL.Query().Get("points")
	from := now - 86400
	to := now
	points := 500
	if fromStr != "" {
		if v, err := strconv.ParseInt(fromStr, 10, 64); err == nil {
			from = v
		}
	}
	if toStr != "" {
		if v, err := strconv.ParseInt(toStr, 10, 64); err == nil {
			to = v
		}
	}
	if pointsStr != "" {
		if v, err := strconv.Atoi(pointsStr); err == nil && v > 0 {
			points = v
		}
	}
	resolution, data, err := s.collectorReader.Series(metric, from, to, points)
	if err != nil {
		writeError(w, http.StatusBadRequest, "series_error", err.Error())
		return
	}
	if data == nil {
		data = []collectorreader.Point{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"metric": metric, "resolution": resolution, "points": data,
		"from": from, "to": to,
	})
}
