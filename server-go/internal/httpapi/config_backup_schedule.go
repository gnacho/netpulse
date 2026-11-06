package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
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
	if !configBackupScheduleDue(sched, now) {
		return
	}
	servedByAgent := map[string]bool{}
	sent := 0
	if s.agentHub != nil {
		for _, slug := range s.agentHub.ConnectedSlugs() {
			if err := s.agentHub.SendPrivileged(slug, "config_backup", map[string]any{}); err != nil {
				log.Printf("[netpulse] config backup programado: %s: %v", slug, err)
				continue
			}
			servedByAgent[slug] = true
			sent++
		}
	}
	// #1924: las unidades sin agente se copian por SSH en el mismo tick, y
	// solo las que su agente no haya servido ya en este pase.
	sent += s.configBackupSshPass(now, servedByAgent)
	if sent == 0 {
		// Ninguna unidad copiada no quema el slot: se reintenta dentro de la
		// misma semana.
		return
	}
	lastRun := time.Now().UnixMilli()
	if err := kvSetStr(s.db.DB, cfgBackupSchedulePrefix+"last_run_ms", strconv.FormatInt(lastRun, 10)); err != nil {
		log.Printf("[netpulse] config backup programado: guardar last_run: %v", err)
		return
	}
	log.Printf("[netpulse] config backup programado: %d unidad(es) copiada(s)", sent)
}

// configBackupSshRetry: una unidad sin agente solo se intenta por SSH cada
// este intervalo. El tick es de 30 s y un dial SSH puede tardar 120 s: sin
// freno, una unidad caída recibiría un intento cada medio minuto, el ruido que
// ya se reportó en #1893.
const configBackupSshRetry = 10 * time.Minute

// configBackupSshTargets: selección pura del pase SSH (#1924). Entra toda
// unidad que se pueda copiar por SSH (OpenWrt con host) que no esté marcada
// agent-only y que su agente no haya servido ya en este pase.
func configBackupSshTargets(rows []adapters.RouterConfig, servedByAgent map[string]bool) []adapters.RouterConfig {
	out := make([]adapters.RouterConfig, 0, len(rows))
	for _, rc := range rows {
		if rc.AgentOnly || servedByAgent[rc.ID] || rc.Host == "" {
			continue
		}
		if !routerConfigBackupSshCapable(rc) {
			continue
		}
		out = append(out, rc)
	}
	return out
}

// cfgBackupSshAllowed aplica el freno entre pases SSH. Se marca el intento al
// empezar: un pase lento o con unidades caídas tampoco reintenta enseguida.
func (s *server) cfgBackupSshAllowed(now time.Time) bool {
	s.cfgBackupSshMu.Lock()
	defer s.cfgBackupSshMu.Unlock()
	if !s.lastCfgBackupSsh.IsZero() && now.Sub(s.lastCfgBackupSsh) < configBackupSshRetry {
		return false
	}
	s.lastCfgBackupSsh = now
	return true
}

// configBackupSshPass copia por SSH las unidades sin agente y devuelve cuántas
// copias se guardaron. Una unidad que falle no aborta el resto: se registra y
// se sigue con la siguiente.
func (s *server) configBackupSshPass(now time.Time, servedByAgent map[string]bool) int {
	if s.db == nil || s.pool == nil {
		return 0
	}
	targets := configBackupSshTargets(routerstore.ListRouters(s.db.DB), servedByAgent)
	if len(targets) == 0 {
		return 0
	}
	if !s.cfgBackupSshAllowed(now) {
		log.Printf("[netpulse] config backup programado: %d unidad(es) sin agente esperando turno por SSH", len(targets))
		return 0
	}
	copied := 0
	for _, rc := range targets {
		label := rc.Name
		if label == "" {
			label = rc.ID
		}
		name, size, err := s.backupConfigOverSsh(context.Background(), rc.ID, rc.SSHAddr())
		if err != nil {
			log.Printf("[netpulse] config backup programado: %s: %v", label, err)
			continue
		}
		copied++
		log.Printf("[netpulse] config backup programado: %s: copia por SSH %s (%d bytes)", label, name, size)
	}
	return copied
}
