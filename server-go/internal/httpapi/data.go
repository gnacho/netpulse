// data.go — rutas de datos (paridad routes/data.js). Paginación zod-coerce,
// filtros literales y envelopes exactos.
package httpapi

import (
	"database/sql"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/deviceevents"
	"github.com/gnacho/netpulse/server-go/internal/portseries"
	"github.com/gnacho/netpulse/server-go/internal/roamevents"
	"github.com/gnacho/netpulse/server-go/internal/speedtest"
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
	if start >= 0 && start < int64(total) {
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
	EnrichOverview(s.db.DB, s.speedtest, &out)
	writeJSON(w, http.StatusOK, &out)
}

// EnrichOverview inyecta los campos que viven en el kv del server (menú de
// orquestación #121, velocidad contratada #151 y última medición del
// speedtest #511). La usan handleOverview Y el poller antes de emitir el
// snapshot SSE: sin la segunda, el evento pisaría la copia enriquecida del
// HTTP y los campos desaparecerían de la UI al primer snapshot (bug
// preexistente de #151 que el speedtest hizo evidente).
func EnrichOverview(handle *sql.DB, sched *speedtest.Scheduler, out *adapters.Overview) {
	// Menú de orquestación: opt-in por admin (#121). Se expone aquí (no en
	// el adapter) porque el kv vive en el server, no en el poller.
	out.Orchestration = kvGetBool(handle, orchestrationKey)
	// Velocidad WAN contratada (#151): declarada por el admin en Ajustes y
	// persistida en kv.
	if v, ok := kvGetFloat(handle, wanSpeedDownKey); ok {
		out.WAN.ContractDownMbps = &v
	}
	if v, ok := kvGetFloat(handle, wanSpeedUpKey); ok {
		out.WAN.ContractUpMbps = &v
	}
	// Última medición real del speedtest (#511): el scheduler escribe la
	// serie; aquí solo se expone la última para la tarjeta WAN. nil (demo o
	// sin ningún test) → campos ausentes y la UI muestra su estado vacío.
	if sched != nil {
		if last, err := sched.Store().Latest(); err == nil && last != nil {
			down, up := last.DownMbps, last.UpMbps
			ts := last.TS.UnixMilli()
			out.WAN.SpeedtestDownMbps = &down
			out.WAN.SpeedtestUpMbps = &up
			out.WAN.SpeedtestTs = &ts
			out.WAN.SpeedtestServer = last.ServerName
		}
	}
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
	typeCounts := map[string]int{}
	for _, d := range items {
		typeCounts[d.Type]++
	}
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
	out := paginate(filtered, page, pageSize)
	out["typeCounts"] = typeCounts
	writeJSON(w, http.StatusOK, out)
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
	if st := readJSONBody(w, r, &patch); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if patch == nil {
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
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
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

// handleAlertsSilence (POST): body {"id":"...","duration":"1h|24h|forever"} →
// silencia alertas con la misma dedup key (category|title|routerId).
func (s *server) handleAlertsSilence(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Duration string `json:"duration"` // "1h", "24h", "forever"
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if body.ID == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "id is required")
		return
	}
	var dur time.Duration
	switch body.Duration {
	case "1h":
		dur = time.Hour
	case "24h":
		dur = 24 * time.Hour
	case "forever", "":
		dur = 0
	default:
		writeError(w, http.StatusBadRequest, "invalid_input", "duration must be 1h, 24h or forever")
		return
	}
	key := s.adapter.AlertsEngine().Silence(body.ID, dur)
	if key == "" {
		writeError(w, http.StatusNotFound, "not_found", "alert not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "key": key})
}

// handleAlertsUnsilence (POST): body {"key":"cat|title|routerId"} → quita silencio.
func (s *server) handleAlertsUnsilence(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if body.Key == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "key is required")
		return
	}
	s.adapter.AlertsEngine().Unsilence(body.Key)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAlertsSilenced (GET): devuelve las dedup keys silenciadas activas.
func (s *server) handleAlertsSilenced(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.adapter.AlertsEngine().Silenced())
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

// handleUsteer: 503 {error:'unavailable'} si ningún router tiene usteer.
func (s *server) handleUsteer(w http.ResponseWriter, r *http.Request) {
	usteer, err := s.adapter.GetUsteer(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if usteer == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, usteer)
}

// handleUsteerKick: POST /api/usteer/{mac}/kick - expulsa un cliente de su AP
// usteer actual para forzar la reconexión a otro AP.
func (s *server) handleUsteerKick(w http.ResponseWriter, r *http.Request) {
	mac := r.PathValue("mac")
	if mac == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "mac is required")
		return
	}
	if err := s.adapter.KickUsteerClient(r.Context(), mac); err != nil {
		if err.Error() == "invalid MAC" {
			writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		if strings.Contains(err.Error(), "client not found") {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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

// handleDeviceEvents: eventos offline/online de dispositivos detectados por
// el poller (issue #184), espejo de /api/roam-events.
// Query params: limit (default 100, máx 1000), since (epoch ms), router, mac,
// state ('offline' | 'online').
func (s *server) handleDeviceEvents(w http.ResponseWriter, r *http.Request) {
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
	mac := r.URL.Query().Get("mac")
	state := r.URL.Query().Get("state")
	events, err := deviceevents.ListEvents(s.db.DB, limit, since, routerID, mac, state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if events == nil {
		events = []deviceevents.Event{}
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

// handlePortSeries: GET /api/routers/{id}/ports/{portId}/series
// Query params: from (unix seconds), to (unix seconds), resolution (raw|5m|daily).
func (s *server) handlePortSeries(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.db.PortSeries == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	routerID := r.PathValue("id")
	portID := r.PathValue("portId")
	q := r.URL.Query()
	now := time.Now()
	to := now
	from := now.Add(-24 * time.Hour)
	if v := q.Get("from"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			from = time.Unix(ts, 0)
		}
	}
	if v := q.Get("to"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			to = time.Unix(ts, 0)
		}
	}
	resolution := q.Get("resolution")
	if resolution == "" {
		resolution = portseries.Resolution(from, to)
	}
	if resolution != "raw" && resolution != "5m" && resolution != "daily" {
		writeError(w, http.StatusBadRequest, "invalid_resolution")
		return
	}
	points, err := s.db.PortSeries.GetSeries(routerID, portID, from, to, resolution)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_error", err.Error())
		return
	}
	if points == nil {
		points = []portseries.PortPoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"points":     points,
		"resolution": resolution,
		"routerId":   routerID,
		"portId":     portID,
	})
}
