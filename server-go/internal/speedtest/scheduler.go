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
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// ErrAlreadyRunning lo devuelve RunNow (y lo traduce el API a 409) cuando un
// test está en marcha.
var ErrAlreadyRunning = errors.New("speedtest already running")

// ValidIntervals son los intervalos permitidos en modo "interval" (horas).
// El mínimo de 6 h es deliberado: cada test satura el enlace y consume datos
// (las notas de la integración de Home Assistant dicen lo mismo). 168/720
// son los "semanal/mensual" históricos pre-#744: se aceptan para no romper
// instalaciones existentes; la programación con día y hora concretos va por
// scheduleKind weekly/monthly.
var ValidIntervals = []int{6, 12, 24, 168, 720}

// DefaultSettings: desactivado por defecto (opt-in explícito; un test consume
// datos de la línea y eso lo debe decidir el admin).
const (
	DefaultIntervalHours = 12
	DefaultAlertPct      = 50
	// DefaultSchedTime (#1066): hora por defecto de la programación. Era
	// 01:00; a las 03:00 local el tráfico residencial suele estar en su
	// mínimo, así el test no compite con el uso real de la línea.
	DefaultSchedTime = "03:00"
	testTimeout      = 3 * time.Minute
	tickEvery        = 30 * time.Second
)

// #1066: programación "auto": el scheduler elige la hora de menos tráfico
// de su propio histórico (tabla metrics, últimos 7 días); sin datos, cae a
// la hora por defecto (03:00).
const (
	scheduleKindAuto  = "auto"
	defaultQuietHour  = 3
	quietHourLookback = 7 * 24 * time.Hour
)

// Settings persistidas en kv (claves settings.speedtest.*).
type Settings struct {
	Enabled       bool   `json:"enabled"`
	IntervalHours int    `json:"intervalHours"`
	ServerURL     string `json:"serverUrl"`
	AlertPct      int    `json:"alertPct"`
	// Provider (#976): metodo de medicion. ""/ookla = speedtest.net;
	// cloudflare = speed.cloudflare.com por HTTP; librespeed = instancia
	// LibreSpeed cuya URL base viaja en ServerURL; custom (#1001) =
	// endpoint HTTP libre cuya URL completa viaja en ServerURL.
	Provider string `json:"provider,omitempty"`
	// Programación (#744): "interval" (cada IntervalHours, comportamiento
	// histórico) | "weekly" (DayOfWeek a la Time local) | "monthly"
	// (DayOfMonth a la Time local; si el mes no tiene ese día, el último) |
	// "auto" (#1066: diaria a la hora de menos tráfico del histórico propio;
	// Time la deriva el scheduler y se ignora el valor persistido).
	ScheduleKind string `json:"scheduleKind,omitempty"`
	DayOfWeek    *int   `json:"dayOfWeek,omitempty"`  // 0=domingo..6=sábado (weekly)
	DayOfMonth   *int   `json:"dayOfMonth,omitempty"` // 1..31 (monthly)
	Time         string `json:"time,omitempty"`       // "HH:MM" hora local
}

// Claves kv (mismo formato que settings.wan.speed_* de #151).
const (
	kvEnabled   = "settings.speedtest.enabled"
	kvInterval  = "settings.speedtest.interval_h"
	kvServerURL = "settings.speedtest.server_url"
	kvAlertPct  = "settings.speedtest.alert_pct"
	kvProvider  = "settings.speedtest.provider"
	kvContractD = "settings.wan.speed_down" // del issue #151 (lectura)

	kvScheduleKind = "settings.speedtest.schedule_kind"
	kvDayOfWeek    = "settings.speedtest.day_of_week"
	kvDayOfMonth   = "settings.speedtest.day_of_month"
	kvTime         = "settings.speedtest.time"
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

	// httpRunner (#976) ejecuta los providers HTTP (cloudflare/librespeed/
	// custom); nil = se construye un HTTPRunner por ejecucion. Inyectable
	// en tests.
	httpRunner Runner

	// ndtRunner (#1037) ejecuta el provider M-Lab NDT; nil = NDTRunner{} por
	// ejecución. Inyectable en tests.
	ndtRunner Runner

	// contractDown lee el plan contratado declarado (#151). Inyectada para
	// no duplicar la lógica kv del httpapi; nil = nunca alertar.
	contractDown func() (float64, bool)

	now  func() time.Time
	logf func(format string, args ...any)

	running  atomic.Bool
	mu       sync.Mutex
	lastErr  string
	belowPln bool   // debounce de la alerta: true hasta que un test recupere
	autoTime string // #1066: última hora elegida por kind "auto" ("HH:MM")
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

// SetHTTPRunner inyecta el runner de los providers HTTP (#976), para tests.
func (s *Scheduler) SetHTTPRunner(r Runner) { s.httpRunner = r }

// SetNDTRunner inyecta el runner del provider M-Lab NDT (#1037), para tests.
func (s *Scheduler) SetNDTRunner(r Runner) { s.ndtRunner = r }

// runnerFor elige la implementacion segun el provider configurado.
func (s *Scheduler) runnerFor(provider string) Runner {
	// #1037: "" (no configurado) y el default NDT van al runner NDT; Ookla
	// solo explícito.
	if provider == "" || provider == ProviderNDT {
		if s.ndtRunner != nil {
			return s.ndtRunner
		}
		return NDTRunner{}
	}
	if provider == ProviderOokla {
		return s.runner
	}
	if s.httpRunner != nil {
		return s.httpRunner
	}
	return HTTPRunner{Provider: provider}
}

// Store expone el store para las rutas de lectura (history/latest).
func (s *Scheduler) Store() *Store { return s.store }

// SetContractDown inyecta el lector del plan contratado.
func (s *Scheduler) SetContractDown(fn func() (float64, bool)) { s.contractDown = fn }

// Start lanza el bucle periódico (daemon; no retorna).
func (s *Scheduler) Start() {
	s.sanitizeServerURL()
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for range ticker.C {
		s.tick()
	}
}

func (s *Scheduler) tick() {
	st := s.LoadSettings()
	if st.ScheduleKind == scheduleKindAuto {
		// #1066: la hora se reevalúa en cada tick (el patrón de consumo
		// cambia: findes, vacaciones) y queda cacheada para Status.
		st.Time = s.resolveAutoTime()
	}
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
	if !testDue(st, last, s.now()) {
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
	res, err := s.runnerFor(st.Provider).Run(ctx, st.ServerURL)
	if err != nil {
		s.setLastError(err)
		return err
	}
	// Sanity (#744): una medición con bajada o subida <= 0 es la firma del
	// servidor equivocado (p. ej. la web de Ookla como server_url): se
	// descarta SIN persistir para no ensuciar la serie ni las gráficas.
	if res.DownMbps <= 0 || res.UpMbps <= 0 {
		err := fmt.Errorf("resultado descartado (down %.1f / up %.1f Mbps): servidor de pruebas inválido o inalcanzable, revisa la URL del servidor o déjala vacía para autoselección", res.DownMbps, res.UpMbps)
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
	Running   bool    `json:"running"`
	LastError string  `json:"lastError,omitempty"`
	Last      *Result `json:"last,omitempty"`
	NextRun   *int64  `json:"nextRunMs,omitempty"` // unix ms; nil = sin programar
	// AutoTime (#1066): hora elegida por la programación "auto" ("HH:MM",
	// hora local); solo presente con scheduleKind "auto".
	AutoTime string `json:"autoTime,omitempty"`
}

// Status compone la foto actual (running, último error, último resultado y
// próxima ejecución programada según settings + serie).
func (s *Scheduler) Status() Status {
	st := s.LoadSettings()
	out := Status{Running: s.running.Load()}
	if st.ScheduleKind == scheduleKindAuto {
		// La hora la conoce el tick; si aún no ha pasado ninguno (server
		// recién arrancado), se calcula ahora para que la UI pueda enseñarla.
		s.mu.Lock()
		auto := s.autoTime
		s.mu.Unlock()
		if auto == "" {
			auto = s.resolveAutoTime()
		}
		st.Time = auto
		out.AutoTime = auto
	}
	s.mu.Lock()
	out.LastError = s.lastErr
	s.mu.Unlock()
	if last, err := s.store.Latest(); err == nil {
		out.Last = last
		if st.Enabled {
			next := nextRunAt(st, last, s.now())
			if next.Before(s.now()) {
				next = s.now().Add(tickEvery)
			}
			ms := next.UnixMilli()
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
		Title: "WAN speed below plan",
		Description: fmt.Sprintf(
			"Measured %.0f Mbps down against %.0f Mbps contracted (less than %d%% of the plan)",
			res.DownMbps, contract, st.AlertPct),
		Hint: alerts.HintFor(alerts.HintWanSlow),
		Type: alerts.HintWanSlow,
		Vars: map[string]string{"down": fmt.Sprintf("%.0f", res.DownMbps), "plan": fmt.Sprintf("%.0f", contract), "pct": strconv.Itoa(st.AlertPct)},
		Time: "just now", Ts: res.TS.Unix(),
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

// resolveAutoTime (#1066) calcula la hora elegida por la programación "auto"
// (la de menos tráfico medio del histórico) y la cachea para Status.
func (s *Scheduler) resolveAutoTime() string {
	tm := fmt.Sprintf("%02d:00", s.quietHour(s.now()))
	s.mu.Lock()
	s.autoTime = tm
	s.mu.Unlock()
	return tm
}

// quietHour devuelve la hora del día (local) con menor tráfico medio
// (bajada+subida) de los últimos 7 días según la tabla metrics; sin datos
// suficientes devuelve defaultQuietHour (03:00). La hora se agrupa en la
// zona horaria local del server (misma referencia que el resto del
// scheduler), corrigiendo el epoch con el offset activo en `now`.
func (s *Scheduler) quietHour(now time.Time) int {
	if s.db == nil {
		return defaultQuietHour
	}
	_, off := now.Zone()
	rows, err := s.db.Query(
		`SELECT ((ts/1000 + ?) % 86400) / 3600 AS h,
		        AVG(COALESCE(rx_bps, 0) + COALESCE(tx_bps, 0)), COUNT(*)
		 FROM metrics WHERE ts >= ? GROUP BY h`,
		off, now.Add(-quietHourLookback).UnixMilli())
	if err != nil {
		return defaultQuietHour
	}
	defer rows.Close()
	best, bestAvg, seen := 0, 0.0, false
	for rows.Next() {
		var h, n int
		var avg float64
		if err := rows.Scan(&h, &avg, &n); err != nil {
			continue
		}
		if !seen || avg < bestAvg {
			best, bestAvg, seen = h, avg, true
		}
	}
	if !seen {
		return defaultQuietHour
	}
	return best
}

// LoadSettings lee la configuración del kv con defaults sanos (valores
// inválidos o ausentes → default, nunca error: el test debe poder arrancar).
func (s *Scheduler) LoadSettings() Settings {
	st := Settings{IntervalHours: DefaultIntervalHours, AlertPct: DefaultAlertPct, ScheduleKind: "interval", Time: DefaultSchedTime}
	if s.db == nil {
		return st
	}
	st.Enabled = kvGet(s.db, kvEnabled) == "1"
	if v, ok := kvInt(s.db, kvInterval); ok && validInterval(v) {
		st.IntervalHours = v
	}
	if v := kvGet(s.db, kvServerURL); v != "" {
		st.ServerURL = v
	}
	if v := kvGet(s.db, kvProvider); validProvider(v) {
		st.Provider = v
	} else {
		// #1037: default NDT (privacidad) cuando no hay nada configurado.
		st.Provider = ProviderNDT
	}
	if v, ok := kvInt(s.db, kvAlertPct); ok && v >= 0 && v <= 90 {
		st.AlertPct = v
	}
	if v := kvGet(s.db, kvScheduleKind); validScheduleKind(v) {
		st.ScheduleKind = v
	}
	if v, ok := kvInt(s.db, kvDayOfWeek); ok && v >= 0 && v <= 6 {
		d := v
		st.DayOfWeek = &d
	}
	if v, ok := kvInt(s.db, kvDayOfMonth); ok && v >= 1 && v <= 31 {
		d := v
		st.DayOfMonth = &d
	}
	if v := kvGet(s.db, kvTime); validTimeStr(v) {
		st.Time = v
	}
	return st
}

// SaveSettings valida y persiste (UPSERT por clave).
func (s *Scheduler) SaveSettings(st Settings) error {
	if st.ScheduleKind == "" {
		st.ScheduleKind = "interval"
	}
	if !validScheduleKind(st.ScheduleKind) {
		return errors.New("scheduleKind debe ser interval, auto, weekly o monthly")
	}
	switch st.ScheduleKind {
	case "weekly":
		if st.DayOfWeek == nil || *st.DayOfWeek < 0 || *st.DayOfWeek > 6 {
			return errors.New("weekly exige dayOfWeek 0-6 (0 = domingo)")
		}
		if !validTimeStr(st.Time) {
			return errors.New("weekly exige time HH:MM")
		}
	case "monthly":
		if st.DayOfMonth == nil || *st.DayOfMonth < 1 || *st.DayOfMonth > 31 {
			return errors.New("monthly exige dayOfMonth 1-31")
		}
		if !validTimeStr(st.Time) {
			return errors.New("monthly exige time HH:MM")
		}
	case scheduleKindAuto:
		// sin campos extra: la hora la deriva el scheduler del histórico y
		// lo que llegue en Time se ignora (no se afirma una hora concreta).
	default:
		if !validInterval(st.IntervalHours) {
			return fmt.Errorf("intervalo inválido (%d): permite %v", st.IntervalHours, ValidIntervals)
		}
	}
	if strings.TrimSpace(st.ServerURL) != "" && !validServerURL(st.ServerURL) {
		if trapServerURL(st.ServerURL) {
			return errors.New("speedtest.net es la web de Ookla, no un servidor de pruebas: deja el campo vacío para usar el más cercano automáticamente")
		}
		return errors.New("serverUrl debe ser una URL http(s) válida")
	}
	// Provider (#976): librespeed necesita la URL base de la instancia y
	// custom (#1001) la URL completa del endpoint; los demás la ignoran
	// (ookla/ndt: servidor concreto opcional; cloudflare: endpoints fijos).
	// Por defecto NDT (#1037): medición neutral y sin ánimo comercial, mejor
	// privacidad que Ookla; los que ya tengan otro provider en kv lo conservan.
	if st.Provider == "" {
		st.Provider = ProviderNDT
	}
	if !validProvider(st.Provider) {
		return errors.New("provider debe ser ookla, cloudflare, librespeed, custom o ndt")
	}
	if st.Provider == ProviderLibrespeed && strings.TrimSpace(st.ServerURL) == "" {
		return errors.New("librespeed exige serverUrl con la URL base de la instancia")
	}
	if st.Provider == ProviderCustom && strings.TrimSpace(st.ServerURL) == "" {
		return errors.New("custom exige serverUrl con la URL del endpoint de medición")
	}
	if st.AlertPct < 0 || st.AlertPct > 90 {
		return errors.New("alertPct debe estar entre 0 y 90 (0 = desactivada)")
	}
	kvSet(s.db, kvEnabled, boolStr(st.Enabled))
	kvSet(s.db, kvInterval, fmt.Sprintf("%d", st.IntervalHours))
	kvSet(s.db, kvServerURL, strings.TrimSpace(st.ServerURL))
	kvSet(s.db, kvProvider, st.Provider)
	kvSet(s.db, kvAlertPct, fmt.Sprintf("%d", st.AlertPct))
	kvSet(s.db, kvScheduleKind, st.ScheduleKind)
	kvSet(s.db, kvDayOfWeek, intPtrStr(st.DayOfWeek))
	kvSet(s.db, kvDayOfMonth, intPtrStr(st.DayOfMonth))
	kvSet(s.db, kvTime, st.Time)
	return nil
}

// sanitizeServerURL (#744): limpia al arranque el valor trampa conocido
// (https://speedtest.net guardada cuando el campo se entendía como "la web
// de Ookla"): produce mediciones basura (subida ~0, sin identidad de
// servidor). Idempotente; deja cualquier otra URL intacta.
func (s *Scheduler) sanitizeServerURL() {
	if s.db == nil {
		return
	}
	v := kvGet(s.db, kvServerURL)
	if v != "" && trapServerURL(v) {
		kvSet(s.db, kvServerURL, "")
		s.logf("server_url trampa (%q) limpiada a autoselección", v)
	}
}

func validInterval(v int) bool {
	for _, i := range ValidIntervals {
		if i == v {
			return true
		}
	}
	return false
}

// validServerURL: la URL del servidor de test debe ser http(s) con host, y
// no puede ser la web de Ookla (trampa clásica que mide basura, #744).
func validServerURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if !((u.Scheme == "http" || u.Scheme == "https") && u.Host != "") {
		return false
	}
	return !trapServerURL(raw)
}

// trapServerURL detecta la web de Ookla usada como servidor de pruebas.
func trapServerURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	return host == "speedtest.net"
}

func validScheduleKind(v string) bool {
	return v == "interval" || v == "weekly" || v == "monthly" || v == scheduleKindAuto
}

func validTimeStr(v string) bool {
	if v == "" {
		return false
	}
	_, err := time.Parse("15:04", v)
	return err == nil
}

func intPtrStr(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d", *p)
}

// parseTimeHHMM devuelve la hora del día de un "HH:MM" (00:00 si es inválido;
// el guardado ya valida).
func parseTimeHHMM(v string) (int, int) {
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, 0
	}
	return t.Hour(), t.Minute()
}

// daysInMonth devuelve el último día del mes de t.
func daysInMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}

// todaySlot devuelve hoy a la hora programada ("HH:MM", hora local).
func todaySlot(v string, now time.Time) time.Time {
	h, m := parseTimeHHMM(v)
	return time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
}

// clampDay ajusta el día del mes pedido al último día real del mes de now.
func clampDay(dom int, now time.Time) int {
	if dom < 1 {
		dom = 1
	}
	if max := daysInMonth(now); dom > max {
		dom = max
	}
	return dom
}

// testDue decide si toca medir según la programación (#744). El vencimiento
// siempre deriva del último resultado persistido: reinicios y updates no
// resetean el reloj (mismo contrato que los backups automáticos).
func testDue(st Settings, last *Result, now time.Time) bool {
	if !st.Enabled {
		return false
	}
	switch st.ScheduleKind {
	case scheduleKindAuto:
		// Diario a la hora elegida. El tick resuelve st.Time con la hora de
		// menos tráfico; el fallback cubre llamadas directas con Time vacío.
		tm := st.Time
		if !validTimeStr(tm) {
			tm = DefaultSchedTime
		}
		slot := todaySlot(tm, now)
		return !now.Before(slot) && (last == nil || last.TS.Before(slot))
	case "weekly":
		dow := 0
		if st.DayOfWeek != nil {
			dow = *st.DayOfWeek
		}
		if int(now.Weekday()) != dow {
			return false
		}
		slot := todaySlot(st.Time, now)
		return !now.Before(slot) && (last == nil || last.TS.Before(slot))
	case "monthly":
		dom := 1
		if st.DayOfMonth != nil {
			dom = *st.DayOfMonth
		}
		dom = clampDay(dom, now)
		h, m := parseTimeHHMM(st.Time)
		slot := time.Date(now.Year(), now.Month(), dom, h, m, 0, 0, now.Location())
		return !now.Before(slot) && (last == nil || last.TS.Before(slot))
	default: // interval
		if last == nil {
			return true
		}
		return now.Sub(last.TS) >= time.Duration(st.IntervalHours)*time.Hour
	}
}

// nextRunAt calcula el próximo disparo para Status.
func nextRunAt(st Settings, last *Result, now time.Time) time.Time {
	h, m := parseTimeHHMM(st.Time)
	switch st.ScheduleKind {
	case scheduleKindAuto:
		tm := st.Time
		if !validTimeStr(tm) {
			tm = DefaultSchedTime
		}
		ah, am := parseTimeHHMM(tm)
		slot := todaySlot(tm, now)
		if now.Before(slot) || last == nil || last.TS.Before(slot) {
			return slot
		}
		d := now.AddDate(0, 0, 1)
		return time.Date(d.Year(), d.Month(), d.Day(), ah, am, 0, 0, now.Location())
	case "weekly":
		dow := 0
		if st.DayOfWeek != nil {
			dow = *st.DayOfWeek
		}
		today := todaySlot(st.Time, now)
		if int(now.Weekday()) == dow && (now.Before(today) || last == nil || last.TS.Before(today)) {
			return today
		}
		days := (dow - int(now.Weekday()) + 7) % 7
		if days == 0 {
			days = 7
		}
		d := now.AddDate(0, 0, days)
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, now.Location())
	case "monthly":
		dom := 1
		if st.DayOfMonth != nil {
			dom = *st.DayOfMonth
		}
		dom = clampDay(dom, now)
		slot := time.Date(now.Year(), now.Month(), dom, h, m, 0, 0, now.Location())
		if now.Before(slot) || last == nil || last.TS.Before(slot) {
			return slot
		}
		next := now.AddDate(0, 1, 0)
		domNext := clampDay(dom, next)
		return time.Date(next.Year(), next.Month(), domNext, h, m, 0, 0, now.Location())
	default:
		base := now
		if last != nil && last.TS.After(base.Add(-time.Duration(st.IntervalHours)*time.Hour)) {
			base = last.TS
		}
		return base.Add(time.Duration(st.IntervalHours) * time.Hour)
	}
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
