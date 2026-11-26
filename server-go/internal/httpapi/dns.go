// dns.go — consultas DNS por dispositivo y por red (#2078).
//
// Sirven el agregado horario del query log de AdGuard. Nunca exponen el log
// crudo: solo top de dominios, totales y dominios nuevos de la ventana, más el
// estado de la ingesta para que la UI pueda decir por qué no hay datos.

package httpapi

import (
	"net/http"
	"time"

	"github.com/gnacho/netpulse/agent/macaddr"
	"github.com/gnacho/netpulse/server-go/internal/dnsactivity"
)

// dnsWindow resuelve la ventana pedida. Cualquier valor raro cae a 24h.
type dnsWindow struct {
	Key  string
	From time.Time
	To   time.Time
}

func parseDNSWindow(raw string, now time.Time) dnsWindow {
	switch raw {
	case "1h":
		return dnsWindow{Key: "1h", From: now.Add(-time.Hour), To: now}
	case "7d":
		return dnsWindow{Key: "7d", From: now.Add(-7 * 24 * time.Hour), To: now}
	default:
		return dnsWindow{Key: "24h", From: now.Add(-24 * time.Hour), To: now}
	}
}

// dnsResponse es el contrato común de los dos endpoints.
type dnsResponse struct {
	Status       string                  `json:"status"`
	StatusTs     int64                   `json:"status_ts"`
	Window       string                  `json:"window"`
	GapTs        int64                   `json:"gap_ts"`
	MAC          string                  `json:"mac,omitempty"`
	TotalQueries int64                   `json:"total_queries"`
	TotalBlocked int64                   `json:"total_blocked"`
	Top          []dnsactivity.DomainRow `json:"top"`
	NewDomains   []dnsactivity.FirstSeen `json:"new_domains"`
}

// GET /api/devices/{mac}/dns?window=1h|24h|7d
func (s *server) handleDeviceDNS(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	mac := macaddr.Normalize(r.PathValue("mac"))
	if mac == "" {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	win := parseDNSWindow(r.URL.Query().Get("window"), time.Now())
	store := dnsactivity.NewStore(s.db.DB)

	top, err := store.TopForMAC(r.Context(), mac, win.From, win.To, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_error", err.Error())
		return
	}
	queries, blocked, err := store.TotalsForMAC(r.Context(), mac, win.From, win.To)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_error", err.Error())
		return
	}
	status, statusTs := dnsactivity.StateOf(r.Context(), s.db.DB)
	writeJSON(w, http.StatusOK, dnsResponse{
		Status: string(status), StatusTs: statusTs,
		Window: win.Key, GapTs: dnsactivity.GapSince(r.Context(), s.db.DB),
		MAC: mac, TotalQueries: queries, TotalBlocked: blocked,
		Top: top, NewDomains: []dnsactivity.FirstSeen{},
	})
}

// GET /api/dns/overview?window=1h|24h|7d
func (s *server) handleDNSOverview(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	win := parseDNSWindow(r.URL.Query().Get("window"), time.Now())
	store := dnsactivity.NewStore(s.db.DB)

	top, err := store.TopAll(r.Context(), win.From, win.To, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_error", err.Error())
		return
	}
	queries, blocked, err := store.TotalsAll(r.Context(), win.From, win.To)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_error", err.Error())
		return
	}
	newDomains, err := store.NewDomains(r.Context(), win.From, win.To, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_error", err.Error())
		return
	}
	status, statusTs := dnsactivity.StateOf(r.Context(), s.db.DB)
	writeJSON(w, http.StatusOK, dnsResponse{
		Status: string(status), StatusTs: statusTs,
		Window: win.Key, GapTs: dnsactivity.GapSince(r.Context(), s.db.DB),
		TotalQueries: queries, TotalBlocked: blocked,
		Top: top, NewDomains: newDomains,
	})
}
