package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
)

// #1795: programación semanal del backup de configuración de los agentes.
// El backup lo ejecuta cada agente al recibir config_backup y sube el
// tar.gz resultante, así que el disparo solo alcanza agentes SSE conectados.
const (
	configBackupScheduleTick = 30 * time.Second
	cfgBackupSchedulePrefix  = "config_backup.schedule."
)

// ConfigBackupSchedule: día 0=domingo..6 y hora local HH:MM, igual que la
// recurrencia de firmware.
type ConfigBackupSchedule struct {
	Enabled   bool   `json:"enabled"`
	DayOfWeek int    `json:"dayOfWeek"`
	Time      string `json:"time"`
	LastRunMs int64  `json:"lastRunMs"`
	NextRunMs int64  `json:"nextRunMs,omitempty"`
}

func defaultConfigBackupSchedule() ConfigBackupSchedule {
	return ConfigBackupSchedule{DayOfWeek: 0, Time: "03:00"}
}

func (s *server) loadConfigBackupSchedule() ConfigBackupSchedule {
	sched := defaultConfigBackupSchedule()
	if s.db == nil {
		return sched
	}
	sched.Enabled = kvGetBool(s.db.DB, cfgBackupSchedulePrefix+"enabled")
	sched.DayOfWeek = kvGetInt(s.db.DB, cfgBackupSchedulePrefix+"day_of_week", 0)
	if v := kvGet(s.db.DB, cfgBackupSchedulePrefix+"time"); v != "" {
		sched.Time = v
	}
	if v := kvGet(s.db.DB, cfgBackupSchedulePrefix+"last_run_ms"); v != "" {
		sched.LastRunMs, _ = strconv.ParseInt(v, 10, 64)
	}
	if validateConfigBackupSchedule(sched) != nil {
		sched.Enabled = false
		sched.DayOfWeek = 0
		sched.Time = "03:00"
	}
	sched.NextRunMs = nextConfigBackupSchedule(sched.DayOfWeek, sched.Time, time.Now()).UnixMilli()
	return sched
}

func (s *server) saveConfigBackupSchedule(sched ConfigBackupSchedule) error {
	if s.db == nil {
		return fmt.Errorf("no_db")
	}
	if err := validateConfigBackupSchedule(sched); err != nil {
		return err
	}
	if err := kvSetBool(s.db.DB, cfgBackupSchedulePrefix+"enabled", sched.Enabled); err != nil {
		return err
	}
	if err := kvSetStr(s.db.DB, cfgBackupSchedulePrefix+"day_of_week", strconv.Itoa(sched.DayOfWeek)); err != nil {
		return err
	}
	if err := kvSetStr(s.db.DB, cfgBackupSchedulePrefix+"time", sched.Time); err != nil {
		return err
	}
	return kvSetStr(s.db.DB, cfgBackupSchedulePrefix+"last_run_ms", strconv.FormatInt(sched.LastRunMs, 10))
}

func validateConfigBackupSchedule(sched ConfigBackupSchedule) error {
	if sched.DayOfWeek < 0 || sched.DayOfWeek > 6 {
		return fmt.Errorf("dayOfWeek debe estar entre 0 y 6")
	}
	parsed, err := time.Parse("15:04", sched.Time)
	if err != nil || parsed.Format("15:04") != sched.Time {
		return fmt.Errorf("time debe ser HH:MM")
	}
	return nil
}

// configBackupScheduleDue sigue el contrato de la recurrencia semanal de
// firmware: solo el día elegido, con el slot ya alcanzado, y sin catch-up de
// días perdidos.
func configBackupScheduleDue(sched ConfigBackupSchedule, now time.Time) bool {
	if !sched.Enabled || int(now.Weekday()) != sched.DayOfWeek {
		return false
	}
	parsed, err := time.Parse("15:04", sched.Time)
	if err != nil {
		return false
	}
	slot := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	return !now.Before(slot) && sched.LastRunMs < slot.UnixMilli()
}

func nextConfigBackupSchedule(day int, clock string, now time.Time) time.Time {
	parsed, err := time.Parse("15:04", clock)
	if err != nil {
		parsed = time.Date(0, 1, 1, 3, 0, 0, 0, time.UTC)
	}
	days := (day - int(now.Weekday()) + 7) % 7
	next := time.Date(now.Year(), now.Month(), now.Day()+days, parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 7)
	}
	return next
}

func (s *server) registerConfigBackupScheduleRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/settings/config-backup-schedule", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.loadConfigBackupSchedule())
	})))
	mux.Handle("PUT /api/settings/config-backup-schedule", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.db == nil {
			writeError(w, http.StatusServiceUnavailable, "no_db")
			return
		}
		var req ConfigBackupSchedule
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "JSON inválido")
			return
		}
		if err := validateConfigBackupSchedule(req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_schedule", err.Error())
			return
		}
		prev := s.loadConfigBackupSchedule()
		req.LastRunMs = prev.LastRunMs
		// Al activar, el slot ya pasado no se ejecuta de inmediato. Si hoy
		// aún no ha llegado, last_run=ahora sigue siendo anterior al slot.
		if req.Enabled && !prev.Enabled {
			req.LastRunMs = time.Now().UnixMilli()
		}
		if err := s.saveConfigBackupSchedule(req); err != nil {
			writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		req.NextRunMs = nextConfigBackupSchedule(req.DayOfWeek, req.Time, time.Now()).UnixMilli()
		writeJSON(w, http.StatusOK, req)
	})))
}

func (s *server) configBackupScheduleLoop(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.configBackupScheduleTick()
		}
	}
}

func (s *server) configBackupScheduleTick() {
	s.configBackupScheduleTickAt(time.Now())
}

func (s *server) configBackupScheduleTickAt(now time.Time) {
	sched := s.loadConfigBackupSchedule()
	if !configBackupScheduleDue(sched, now) || s.agentHub == nil {
		return
	}
	sent := 0
	for _, slug := range s.agentHub.ConnectedSlugs() {
		if err := s.agentHub.SendPrivileged(slug, "config_backup", map[string]any{}); err != nil {
			log.Printf("[netpulse] config backup programado: %s: %v", slug, err)
			continue
		}
		sent++
	}
	if sent == 0 {
		// Sin agentes conectados no se quema el slot: se reintenta al
		// reconectar dentro de la misma semana.
		return
	}
	lastRun := time.Now().UnixMilli()
	if err := kvSetStr(s.db.DB, cfgBackupSchedulePrefix+"last_run_ms", strconv.FormatInt(lastRun, 10)); err != nil {
		log.Printf("[netpulse] config backup programado: guardar last_run: %v", err)
		return
	}
	log.Printf("[netpulse] config backup programado enviado a %d agentes", sent)
}
