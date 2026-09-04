// scheduler.go — ejecución periódica del test (issue #511).
//
// Bucle de tick corto (30 s) que decide en cada pasada si toca medir: el
// vencimiento se deriva del último resultado + intervalo configurado, así
// un cambio de ajustes aplica en menos de 30 s sin canales ni reinicios.
// Single-flight con CAS: nunca dos tests simultáneos (un test satura el
// enlace; solaparlos falsearía ambas mediciones).
package speedtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// ErrAlreadyRunning lo devuelve RunNow (y lo traduce el API a 409) cuando un
// test está en marcha.
var ErrAlreadyRunning = errors.New("speedtest already running")

// ValidIntervals son los intervalos permitidos (horas). El mínimo de 6 h es
// deliberado: cada test satura el enlace y consume datos (las notas de la
// integración de Home Assistant dicen lo mismo).
var ValidIntervals = []int{6, 12, 24}

// DefaultSettings: desactivado por defecto (opt-in explícito; un test consume
// datos de la línea y eso lo debe decidir el admin).
const (
	DefaultIntervalHours = 12
	DefaultAlertPct      = 50
	testTimeout          = 3 * time.Minute
	tickEvery            = 30 * time.Second
)

// Settings persistidas en kv (claves settings.speedtest.*).
type Settings struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"intervalHours"`
	ServerID      int  `json:"serverId"`
	AlertPct      int  `json:"alertPct"`
}

// Claves kv (mismo formato que settings.wan.speed_* de #151).
const (
	kvEnabled   = "settings.speedtest.enabled"
	kvInterval  = "settings.speedtest.interval_h"
	kvServerID  = "settings.speedtest.server_id"
	kvAlertPct  = "settings.speedtest.alert_pct"
	kvContractD = "settings.wan.speed_down" // del issue #151 (lectura)
)

// AlertEmitter lo cumple *alerts.Engine (emisión por el mismo motor que el
// resto de alertas del server; nil = sin alertas, p. ej. en demo).
type AlertEmitter interface {
	Emit(ev alerts.AlertEvent) bool
}

// Scheduler orquesta store + runner + settings.
type Scheduler struct {
	store  *Store
	db     *sql.DB
	runner Runner
	emit   AlertEmitter

	// contractDown lee el plan contratado declarado (#151). Inyectada para
	// no duplicar la lógica kv del httpapi; nil = nunca alertar.
	contractDown func() (float64, bool)

	now  func() time.Time
	logf func(format string, args ...any)

	running  atomic.Bool
	mu       sync.Mutex
	lastErr  string
	belowPln bool // debounce de la alerta: true hasta que un test recupere
}

func NewScheduler(store *Store, db *sql.DB, runner Runner) *Scheduler {
	return &Scheduler{
		store: store, db: db, runner: runner,
		now:  time.Now,
		logf: func(f string, a ...any) { log.Printf("[speedtest] "+f, a...) },
	}
}

// SetAlertEmitter fija el motor de alertas (llamado tras construir el
// adapter live en main).
func (s *Scheduler) SetAlertEmitter(e AlertEmitter) { s.emit = e }

// Store expone el store para las rutas de lectura (history/latest).
func (s *Scheduler) Store() *Store { return s.store }

// SetContractDown inyecta el lector del plan contratado.
func (s *Scheduler) SetContractDown(fn func() (float64, bool)) { s.contractDown = fn }

// Start lanza el bucle periódico (daemon; no retorna).
func (s *Scheduler) Start() {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for range ticker.C {
		s.tick()
	}
}

func (s *Scheduler) tick() {
	st := s.LoadSettings()
	if !st.Enabled || s.running.Load() {
		return
	}
	last, err := s.store.Latest()
	if err != nil {
		s.setLastError(err)
		return
	}
	// Sin resultados previos (primera activación) el primer test sale en
	// el siguiente tick: el admin activa y ve datos en <30 s.
	if last != nil && s.now().Sub(last.TS) < time.Duration(st.IntervalHours)*time.Hour {
		return
	}
	if err := s.tryExecute(st, "scheduled"); err != nil {
		s.logf("test programado falló: %v", err)
	}
}

// RunNow lanza un test manual en segundo plano (POST /api/speedtest/run).
// ErrAlreadyRunning si ya hay uno en marcha.
func (s *Scheduler) RunNow() error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	go func() {
		if err := s.executeLocked(s.LoadSettings(), "manual"); err != nil {
			s.logf("test manual falló: %v", err)
		}
	}()
	return nil
}

// tryExecute toma el lock de single-flight y ejecuta si estaba libre.
func (s *Scheduler) tryExecute(st Settings, origin string) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	return s.executeLocked(st, origin)
}

// executeLocked corre el test asumiendo running==true (el defer libera).
func (s *Scheduler) executeLocked(st Settings, origin string) error {
	defer s.running.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	res, err := s.runner.Run(ctx, st.ServerID)
	if err != nil {
		s.setLastError(err)
		return err
	}
	res.Origin = origin
	if res.TS.IsZero() {
		res.TS = s.now() // runner sin TS (fakes/defensa): la serie exige ts válido
	}
	if err := s.store.Insert(res); err != nil {
		s.setLastError(err)
		return err
	}
	s.setLastError(nil)
	if err := s.store.PruneBefore(s.now().Add(-RawRetention)); err != nil {
		s.logf("prune: %v", err)
	}
	s.maybeAlert(st, res)
	s.logf("test %s: %.1f↓ / %.1f↑ Mbps (server %s)",
		origin, res.DownMbps, res.UpMbps, res.ServerName)
	return nil
}

// Status es la foto para GET /api/speedtest/status.
type Status struct {
	Running   bool     `json:"running"`
	LastError string   `json:"lastError,omitempty"`
	Last      *Result  `json:"last,omitempty"`
	NextRun   *int64   `json:"nextRunMs,omitempty"` // unix ms; nil = sin programar
}

// Status compone la foto actual (running, último error, último resultado y
// próxima ejecución programada según settings + serie).
func (s *Scheduler) Status() Status {
	st := s.LoadSettings()
	out := Status{Running: s.running.Load()}
	s.mu.Lock()
	out.LastError = s.lastErr
	s.mu.Unlock()
	if last, err := s.store.Latest(); err == nil {
		out.Last = last
		if st.Enabled && last != nil {
			next := last.TS.Add(time.Duration(st.IntervalHours) * time.Hour)
			if next.Before(s.now()) {
				next = s.now().Add(tickEvery)
			}
			ms := next.UnixMilli()
			out.NextRun = &ms
		} else if st.Enabled {
			ms := s.now().Add(tickEvery).UnixMilli()
			out.NextRun = &ms
		}
	}
	return out
}

// maybeAlert emite "velocidad por debajo del plan" con debounce por episode:
// una alerta cuando empieza el problema y silencio hasta que un test vuelva
// a superar el umbral (re-alertar cada 12 h sería ruido, no información).
func (s *Scheduler) maybeAlert(st Settings, res Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emit == nil || st.AlertPct <= 0 || s.contractDown == nil {
		s.belowPln = false
		return
	}
	contract, ok := s.contractDown()
	if !ok || contract <= 0 || res.DownMbps >= contract*float64(st.AlertPct)/100 {
		s.belowPln = false
		return
	}
	if s.belowPln {
		return
	}
	s.belowPln = true
	s.emit.Emit(alerts.AlertEvent{
		ID:       fmt.Sprintf("alert-wanslow-%d", res.TS.UnixMilli()),
		Category: alerts.CatInternet, Urgent: false, Severity: "warn",
		Title: "Velocidad WAN por debajo del plan",
		Description: fmt.Sprintf(
			"Medido %.0f Mbps de bajada contra %.0f Mbps contratados (menos del %d%% del plan)",
			res.DownMbps, contract, st.AlertPct),
		Hint: alerts.HintFor(alerts.HintWanSlow),
		Time: "ahora mismo", Ts: res.TS.Unix(),
	})
}

func (s *Scheduler) setLastError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.lastErr = ""
		return
	}
	s.lastErr = err.Error()
}

// LoadSettings lee la configuración del kv con defaults sanos (valores
// inválidos o ausentes → default, nunca error: el test debe poder arrancar).
func (s *Scheduler) LoadSettings() Settings {
	st := Settings{IntervalHours: DefaultIntervalHours, AlertPct: DefaultAlertPct}
	if s.db == nil {
		return st
	}
	st.Enabled = kvGet(s.db, kvEnabled) == "1"
	if v, ok := kvInt(s.db, kvInterval); ok && validInterval(v) {
		st.IntervalHours = v
	}
	if v, ok := kvInt(s.db, kvServerID); ok && v > 0 {
		st.ServerID = v
	}
	if v, ok := kvInt(s.db, kvAlertPct); ok && v >= 0 && v <= 90 {
		st.AlertPct = v
	}
	return st
}

// SaveSettings valida y persiste (UPSERT por clave).
func (s *Scheduler) SaveSettings(st Settings) error {
	if !validInterval(st.IntervalHours) {
		return fmt.Errorf("intervalo inválido (%d): permite %v", st.IntervalHours, ValidIntervals)
	}
	if st.ServerID < 0 {
		return errors.New("serverId no puede ser negativo")
	}
	if st.AlertPct < 0 || st.AlertPct > 90 {
		return errors.New("alertPct debe estar entre 0 y 90 (0 = desactivada)")
	}
	kvSet(s.db, kvEnabled, boolStr(st.Enabled))
	kvSet(s.db, kvInterval, fmt.Sprintf("%d", st.IntervalHours))
	kvSet(s.db, kvServerID, fmt.Sprintf("%d", st.ServerID))
	kvSet(s.db, kvAlertPct, fmt.Sprintf("%d", st.AlertPct))
	return nil
}

func validInterval(v int) bool {
	for _, i := range ValidIntervals {
		if i == v {
			return true
		}
	}
	return false
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func kvGet(db *sql.DB, key string) string {
	var v string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&v); err != nil {
		return ""
	}
	return v
}

func kvInt(db *sql.DB, key string) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(kvGet(db, key), "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}

func kvSet(db *sql.DB, key, val string) {
	if _, err := db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, val); err != nil {
		log.Printf("[speedtest] kv set %s: %v", key, err)
	}
}
