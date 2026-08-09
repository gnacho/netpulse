// data.go — rutas de datos (paridad routes/data.js). Paginación zod-coerce,
// filtros literales y envelopes exactos.
package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/roamevents"
)

var bands = []string{"5 GHz", "2.4 GHz", "cable"}
var severities = []string{"warn", "critical", "info", "ok"}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// coercePage replica z.coerce.number().int().min(min).max(max): el valor debe
// ser numérico y entero ("" → 0, "abc" → NaN → inválido).
func coerceInt(raw string, def, min, max int64, hasDefault bool) (int64, bool) {
	if raw == "" && hasDefault {
		// zod: undefined → default; "" → Number("")=0 → inválido si min>0.
		// La query de Go no distingue "?page=" de ausencia; r.URL.Query()
		// devuelve [""] en ambos casos, tratado aquí por el llamador.
		return def, true
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(f) || f != math.Trunc(f) {
		return 0, false
	}
	n := int64(f)
	if n < min || n > max {
		return 0, false
	}
	return n, true
}

// parsePagination lee page/pageSize con semántica zod-coerce. hasParam
// distingue ausencia (default) de "?page=" (→ 0 → inválido).
func parsePagination(r *http.Request, defaultPageSize int64) (page, pageSize int64, ok bool) {
	q := r.URL.Query()
	page, pageSize = 1, defaultPageSize
	if _, present := q["page"]; present {
		raw := q.Get("page")
		if raw == "" {
			return 0, 0, false // Number("") = 0 < min
		}
		n, valid := coerceInt(raw, 1, 1, math.MaxInt64, false)
		if !valid {
			return 0, 0, false
		}
		page = n
	}
	if _, present := q["pageSize"]; present {
		raw := q.Get("pageSize")
		if raw == "" {
			return 0, 0, false
		}
		n, valid := coerceInt(raw, defaultPageSize, 1, 1000, false)
		if !valid {
			return 0, 0, false
		}
		pageSize = n
	}
	return page, pageSize, true
}

// paginate: slice (page-1)*pageSize; items nunca null ([] si vacío).
func paginate[T any](items []T, page, pageSize int64) map[string]any {
	total := len(items)
	start := (page - 1) * pageSize
	var out []T
	if start < int64(total) {
		end := start + pageSize
		if end > int64(total) {
			end = int64(total)
		}
		out = items[start:end]
	}
	if out == nil {
		out = []T{}
	}
	return map[string]any{"items": out, "total": total, "page": page, "pageSize": pageSize}
}

// handleOverview: último overview del poller o GetOverview en caliente.
// El read-state de alertas se aplica SIEMPRE al servir (server truth,
// SPEC-ALERTAS §4): el caché del poller puede llevar hasta 5 s de retraso
// y el badge debe reflejar read/read-all al instante.
func (s *server) handleOverview(w http.ResponseWriter, r *http.Request) {
	var ov *adapters.Overview
	if s.lastOv != nil {
		ov = s.lastOv()
	}
	if ov == nil {
		fresh, err := s.adapter.GetOverview(r.Context())
		if err != nil || fresh == nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		ov = fresh
	}
	out := *ov
	engine := s.adapter.AlertsEngine()
	out.Alerts = engine.List()
	out.UnreadAlerts = engine.UnreadCount()
	// Menú de orquestación: opt-in por admin (#121). Se expone aquí (no en el
	// adapter) porque el kv vive en el server, no en el poller.
	out.Orchestration = kvGetBool(s.db.DB, orchestrationKey)
	writeJSON(w, http.StatusOK, &out)
}

// handleRouters: {routers: [Router…]}.
func (s *server) handleRouters(w http.ResponseWriter, r *http.Request) {
	routers := s.adapter.GetRouters(r.Context())
	if routers == nil {
		routers = []adapters.Router{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"routers": routers})
}

// handleRouterDetail: RouterDetail o 404 {error:'not_found'}.
func (s *server) handleRouterDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.adapter.GetRouterDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if detail == nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleDevices: filtros q/routerId/band/type/status + paginación.
func (s *server) handleDevices(w http.ResponseWriter, r *http.Request) {
	page, pageSize, ok := parsePagination(r, 50)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query", "page/pageSize inválidos")
		return
	}
	q := r.URL.Query()
	query := strings.ToLower(strings.TrimSpace(q.Get("q")))
	if len([]rune(query)) > 100 {
		query = string([]rune(query)[:100])
	}
	routerID := q.Get("routerId")
	band := q.Get("band")
	typ := q.Get("type")
	status := q.Get("status")

	if band != "" && !contains(bands, band) {
		writeError(w, http.StatusBadRequest, "invalid_query", "band debe ser una de: "+strings.Join(bands, ", "))
		return
	}
	if status != "" && status != "online" && status != "offline" {
		writeError(w, http.StatusBadRequest, "invalid_query", "status debe ser online|offline")
		return
	}

	items := s.adapter.GetDevices(r.Context())
	filtered := make([]adapters.Device, 0, len(items))
	for _, d := range items {
		if routerID != "" && d.RouterID != routerID {
			continue
		}
		if band != "" && d.Band != band {
			continue
		}
		if typ != "" && d.Type != typ && d.Group != typ {
			continue
		}
		if status == "online" && !d.Online {
			continue
		}
		if status == "offline" && d.Online {
			continue
		}
		if query != "" && !deviceMatches(d, query) {
			continue
		}
		filtered = append(filtered, d)
	}
	writeJSON(w, http.StatusOK, paginate(filtered, page, pageSize))
}

// deviceMatches: substring (minúsculas) sobre name|hostname|ip|mac|manufacturer.
func deviceMatches(d adapters.Device, query string) bool {
	for _, v := range []string{d.Name, d.Hostname, d.IP, d.MAC, d.Manufacturer} {
		if v != "" && strings.Contains(strings.ToLower(v), query) {
			return true
		}
	}
	return false
}

// handleAlerts: severity + category + unread=1 + paginación (default
// pageSize=20). El read-state lo aplica el motor (SPEC-ALERTAS §3-4).
func (s *server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	page, pageSize, ok := parsePagination(r, 20)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query", "page/pageSize inválidos")
		return
	}
	severity := r.URL.Query().Get("severity")
	if severity != "" && !contains(severities, severity) {
		writeError(w, http.StatusBadRequest, "invalid_query", "severity debe ser una de: "+strings.Join(severities, ", "))
		return
	}
	category := r.URL.Query().Get("category")
	if category != "" && !alerts.IsCategory(category) {
		writeError(w, http.StatusBadRequest, "invalid_query", "category debe ser una de: "+strings.Join(alerts.Categories, ", "))
		return
	}
	unreadOnly := r.URL.Query().Get("unread") == "1"
	items := s.adapter.GetAlerts(r.Context())
	filtered := make([]adapters.AlertEvent, 0, len(items))
	for _, a := range items {
		if severity != "" && a.Severity != severity {
			continue
		}
		if category != "" && a.Category != category {
			continue
		}
		if unreadOnly && a.Read {
			continue
		}
		filtered = append(filtered, a)
	}
	writeJSON(w, http.StatusOK, paginate(filtered, page, pageSize))
}

// handleAlertsConfig (GET): las 6 categorías con su nivel efectivo.
func (s *server) handleAlertsConfigGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.adapter.AlertsEngine().Config())
}

// handleAlertsConfigPut (PUT): parche parcial {"signal":"all"}; valida
// categorías y niveles (400 en inválido, SPEC-ALERTAS §4).
func (s *server) handleAlertsConfigPut(w http.ResponseWriter, r *http.Request) {
	var patch map[string]string
	if !readJSONBody(r, &patch) || patch == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "body JSON inválido")
		return
	}
	if err := s.adapter.AlertsEngine().SetConfig(patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.adapter.AlertsEngine().Config())
}

// handleAlertsRead (POST): body {"ids":["a","b"]} → marca leídas.
func (s *server) handleAlertsRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !readJSONBody(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid_body", "body JSON inválido")
		return
	}
	s.adapter.AlertsEngine().MarkRead(body.IDs...)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAlertsReadAll (POST): marca todas como leídas.
func (s *server) handleAlertsReadAll(w http.ResponseWriter, _ *http.Request) {
	s.adapter.AlertsEngine().MarkAllRead()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTopology: {routers, devices} sin paginar.
func (s *server) handleTopology(w http.ResponseWriter, r *http.Request) {
	routers := s.adapter.GetRouters(r.Context())
	if routers == nil {
		routers = []adapters.Router{}
	}
	devices := s.adapter.GetDevices(r.Context())
	if devices == nil {
		devices = []adapters.Device{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"routers": routers, "devices": devices})
}

// handleDawn: 503 {error:'unavailable'} si ningún router tiene DAWN.
func (s *server) handleDawn(w http.ResponseWriter, r *http.Request) {
	dawn, err := s.adapter.GetDawn(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if dawn == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, dawn)
}

// handleDot11r: 503 {error:'unavailable'} si ningún router tiene 802.11r.
func (s *server) handleDot11r(w http.ResponseWriter, r *http.Request) {
	dot11r, err := s.adapter.GetDot11r(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if dot11r == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, dot11r)
}

// handleSurvey: 503 {error:'unavailable'} si ningún router responde.
func (s *server) handleSurvey(w http.ResponseWriter, r *http.Request) {
	survey, err := s.adapter.GetSurvey(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if survey == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, survey)
}

// handleRoamEvents: lista paginada de eventos hostapd/DAWN desde SQLite.
// Query params: limit (default 100, máx 1000), since (epoch ms), router, type.
func (s *server) handleRoamEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	var since int64
	if v := r.URL.Query().Get("since"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			since = n
		}
	}
	routerID := r.URL.Query().Get("router")
	eventType := r.URL.Query().Get("type")
	events, err := roamevents.ListEvents(s.db.DB, limit, since, routerID, eventType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if events == nil {
		events = []roamevents.Event{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// handleAdguardClients: 404 not_configured · 502 adguard_error.
func (s *server) handleAdguardClients(w http.ResponseWriter, r *http.Request) {
	clients, err := s.adapter.GetAdguardClients(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "adguard_error", err.Error())
		return
	}
	if clients == nil {
		writeError(w, http.StatusNotFound, "not_configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"clients": clients})
}
