// refresh.go — POST /api/refresh: sondeo manual bajo demanda (botón
// "Refrescar" de la página Topología). No devuelve datos: encola un ciclo de
// sondeo inmediato (tier rápido; los caches LLDP 45 s / backhaul 5 min del
// adapter live se respetan) y el snapshot fresco llega por el SSE normal.
// Anti-martilleo: mínimo 5 s entre refreshes manuales → 429 + Retry-After.
package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"time"
)

// refreshMinInterval es el mínimo entre sondeos manuales (global: el sondeo
// es de todos los routers, da igual qué usuario lo pida).
const refreshMinInterval = 5 * time.Second

func (s *server) handleRefresh(w http.ResponseWriter, _ *http.Request) {
	s.refreshMu.Lock()
	if since := time.Since(s.lastRefresh); since < refreshMinInterval {
		retry := int(math.Ceil((refreshMinInterval - since).Seconds()))
		s.refreshMu.Unlock()
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":         "rate_limited",
			"retryAfterSec": retry,
		})
		return
	}
	s.lastRefresh = time.Now()
	s.refreshMu.Unlock()

	// Dispara el sondeo en background (nil en tests sin poller → no-op). En
	// modo demo el poller re-emite el snapshot demo por SSE igualmente; en
	// live sondea todos los routers (tier rápido). Nunca 500 por esto.
	if s.pollNow != nil {
		s.pollNow()
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}
