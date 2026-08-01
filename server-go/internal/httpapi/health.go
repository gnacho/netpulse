// health.go — GET /api/health (público, contrato) y GET /health (SPEC §2.1/§2.2).
package httpapi

import (
	"net/http"
	"runtime"
	"time"
)

// handleAPIHealth: {ok, version, mode, uptimeSec, db}.
func (s *server) handleAPIHealth(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbOk := s.db.CheckHealth()
		dbStatus := "ok"
		if !dbOk {
			dbStatus = "error"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        dbOk,
			"version":   Version,
			"mode":      mode,
			"uptimeSec": int64(time.Since(s.started).Seconds()),
			"db":        dbStatus,
		})
	}
}

// handleHealth: {status, uptime (float segundos), memory:{rss, heap}, db}.
// En Go los bytes salen de runtime.ReadMemStats: rss ≈ Sys (memoria obtenida
// del SO) y heap ≈ HeapAlloc. Los valores absolutos cambian de lenguaje —
// solo el shape es contrato (SPEC §2.2).
func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	dbOk := s.db.CheckHealth()
	status, dbStatus := "ok", "connected"
	if !dbOk {
		status, dbStatus = "degraded", "error"
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": status,
		"uptime": time.Since(s.started).Seconds(),
		"memory": map[string]any{
			"rss":  mem.Sys,
			"heap": mem.HeapAlloc,
		},
		"db": dbStatus,
	})
}
