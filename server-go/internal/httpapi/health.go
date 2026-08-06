// health.go — GET /api/health (público, contrato) y GET /health (SPEC §2.1/§2.2).
package httpapi

import (
	"net/http"
	"runtime"
	"time"
)

// handleAPIHealth: {ok, version, mode, uptimeSec, db, agentsConnected,
// sseConnections, devicesTotal}.
func (s *server) handleAPIHealth(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbOk := s.db.CheckHealth()
		dbStatus := "ok"
		if !dbOk {
			dbStatus = "error"
		}

		// Métricas operativas (Fase 8): agentes vivos (dentro de TTL),
		// clientes SSE conectados y total de dispositivos del último
		// overview del poller. Nil-safe: demo/sin datos → 0.
		agentsConnected := 0
		if s.agents != nil {
			agentsConnected = s.agents.ActiveCount()
		}
		sseConnections := 0
		if s.hub != nil {
			sseConnections = s.hub.Size()
		}
		devicesTotal := 0
		if s.lastOv != nil {
			if ov := s.lastOv(); ov != nil {
				devicesTotal = ov.DeviceTotals.Total
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":              dbOk,
			"version":         Version,
			"mode":            mode,
			"uptimeSec":       int64(time.Since(s.started).Seconds()),
			"db":              dbStatus,
			"agentsConnected": agentsConnected,
			"sseConnections":  sseConnections,
			"devicesTotal":    devicesTotal,
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
