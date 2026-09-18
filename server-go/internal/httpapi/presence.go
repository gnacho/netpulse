// presence.go — timeline de presencia por cliente y salud de roaming (#771).
//
// Los eventos YA se generaban: device_events (#184, online/offline del
// poller) y roam_events (Fase 14.5, AP-STA-CONNECTED/DISCONNECTED vía
// logread). Estos handlers los funden en tramos de presencia por MAC y
// exponen la salud de roaming de la flota; la retención configurable vive en
// kv (presence.retention_days) y la aplica un loop de poda en main.
package httpapi

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/deviceevents"
)

const presenceRetentionKey = "presence.retention_days"

// handleDevicePresence devuelve los tramos de presencia de una MAC (dias
// visibles, default 7, máx 30) más los eventos recientes y el nº de
// conexiones en las últimas 24 h. En demo devuelve una muestra sintética
// determinista para que la UI sea explorable sin red real.
func (s *server) handleDevicePresence(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	mac = strings.ToUpper(mac)
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 30 {
			days = n
		}
	}
	now := time.Now()
	since := now.Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()

	if s.cfg.DemoMode {
		writeJSON(w, http.StatusOK, demoPresence(mac, days, now))
		return
	}

	intervals, err := deviceevents.Intervals(s.db.DB, mac, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	events, err := deviceevents.ListEvents(s.db.DB, 50, since, "", mac, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	connects24h, _ := deviceevents.CountConnects(s.db.DB, mac, now.Add(-24*time.Hour).UnixMilli())
	writeJSON(w, http.StatusOK, map[string]any{
		"mac":         mac,
		"days":        days,
		"nowMs":       now.UnixMilli(),
		"intervals":   intervals,
		"events":      events,
		"connects24h": connects24h,
	})
}

// handlePresenceRoaming devuelve las MACs con más eventos 'connected' en las
// últimas 24 h (los primeros puestos son los clientes que más rebotan entre
// APs), con nombre resuelto desde la flota actual o los alias conocidos.
func (s *server) handlePresenceRoaming(w http.ResponseWriter, r *http.Request) {
	type item struct {
		MAC      string `json:"mac"`
		Name     string `json:"name"`
		Connects int    `json:"connects"`
	}
	if s.cfg.DemoMode {
		writeJSON(w, http.StatusOK, map[string]any{"items": []item{}})
		return
	}
	since := time.Now().Add(-24 * time.Hour).UnixMilli()
	counts, err := deviceevents.RoamCounts(s.db.DB, since, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	names := map[string]string{}
	if s.adapter != nil {
		for _, d := range s.adapter.GetDevices(r.Context()) {
			names[strings.ToUpper(d.MAC)] = d.Name
		}
	}
	items := make([]item, 0, len(counts))
	for _, c := range counts {
		mac := strings.ToUpper(c.MAC)
		nm := names[mac]
		if nm == "" {
			nm = s.knownMacName(mac)
		}
		items = append(items, item{MAC: mac, Name: nm, Connects: c.Connects})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// knownMacName resuelve el alias de una MAC desde known_macs ("" si no hay).
func (s *server) knownMacName(mac string) string {
	var name string
	_ = s.db.QueryRow("SELECT name FROM known_macs WHERE mac = ?", mac).Scan(&name)
	return name
}

// handlePresenceSettingsGet/Put exponen la retención de eventos de presencia
// (días; 0 = conservar siempre). La poda la aplica el loop de main.
func (s *server) handlePresenceSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"retention_days": s.presenceRetentionDays()})
}

func (s *server) handlePresenceSettingsPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RetentionDays int `json:"retention_days"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if body.RetentionDays < 0 || body.RetentionDays > 365 {
		writeError(w, http.StatusBadRequest, "invalid_retention", "retención 0..365 días")
		return
	}
	if _, err := s.db.Exec("INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		presenceRetentionKey, strconv.Itoa(body.RetentionDays)); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"retention_days": body.RetentionDays})
}

// PresenceRetentionDays lee la retención configurada del kv (default 30
// días; 0 = conservar siempre). La usa el handler de settings y el loop de
// poda de main.
func PresenceRetentionDays(db *sql.DB) int {
	if v := kvGet(db, presenceRetentionKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 30
}

// presenceRetentionDays es la variante del server para el handler de settings.
func (s *server) presenceRetentionDays() int {
	return PresenceRetentionDays(s.db.DB)
}

// demoPresence genera una muestra determinista para el modo demo: dos tramos
// hoy (mañana en el router par, tarde en impar) y un tramo de ayer, más la
// ventana pedida vacía en días anteriores.
func demoPresence(mac string, days int, now time.Time) map[string]any {
	// Hash simple de la MAC para desplazar las horas de forma estable.
	h := 0
	for _, c := range mac {
		h = (h*31 + int(c)) % 240
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	mk := func(d time.Time, h1, h2 int) deviceevents.Interval {
		s := d.Add(time.Duration(h1)*time.Hour + time.Duration(h%60)*time.Minute).UnixMilli()
		e := d.Add(time.Duration(h2) * time.Hour).UnixMilli()
		return deviceevents.Interval{RouterID: "rt-a", StartMs: s, EndMs: &e}
	}
	yesterday := dayStart.Add(-24 * time.Hour)
	intervals := []deviceevents.Interval{
		mk(yesterday, 18, 23),
		mk(dayStart, 8, 12),
	}
	nowMs := now.UnixMilli()
	if h%2 == 0 {
		intervals = append(intervals, deviceevents.Interval{RouterID: "rt-b", StartMs: dayStart.Add(13 * time.Hour).UnixMilli()})
	} else {
		e := dayStart.Add(20 * time.Hour).UnixMilli()
		intervals = append(intervals, deviceevents.Interval{RouterID: "rt-a", StartMs: dayStart.Add(13 * time.Hour).UnixMilli(), EndMs: &e})
	}
	return map[string]any{
		"mac":         mac,
		"days":        days,
		"nowMs":       nowMs,
		"intervals":   intervals,
		"events":      []deviceevents.Event{},
		"connects24h": 2 + h%3,
	}
}
