// autosched.go - auto-actualización programada del propio server (#759).
//
// Off por defecto (opt-in explícito del admin). Patrón de backups (#741),
// speedtest (#744) y firmware (#761): ajustes en kv, vencimiento derivado
// del last_run PERSISTIDO (reinicios y el propio update no resetean el
// reloj) y single-flight. El disparo: Check y, solo si hay novedad fresca y
// el layout puede aplicar, ApplyBy("scheduled"): binario únicamente, con la
// verificación de salud y el rollback del helper root de #480. El last_run
// se persiste ANTES de actuar: el apply reinicia el proceso y el arranque
// posterior no debe volver a disparar el mismo slot.
package updater

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// Kinds de programación del auto-update.
const (
	AutoDaily   = "daily"
	AutoWeekly  = "weekly"
	AutoMonthly = "monthly"
)

// AutoUpdateSettings persistidas en kv (claves settings.autoupdate.*).
type AutoUpdateSettings struct {
	Enabled bool   `json:"enabled"`
	Kind    string `json:"kind"` // daily | weekly | monthly
	Time    string `json:"time"` // "HH:MM" hora local
	// DayOfWeek 0=domingo..6 (weekly); DayOfMonth 1..31 con clamp (monthly).
	DayOfWeek  *int `json:"dayOfWeek,omitempty"`
	DayOfMonth *int `json:"dayOfMonth,omitempty"`
}

// LastRunMs / LastResult viajan aparte (no los escribe el PUT).
type autoRunState struct {
	LastRunMs  int64  `json:"lastRunMs,omitempty"`
	LastResult string `json:"lastResult,omitempty"` // "" | applied | up-to-date | check-failed | cannot-apply
}

// CheckerApplier es lo que el scheduler necesita del updater (lo satisface
// *Updater; fakes en tests).
type CheckerApplier interface {
	Check(ctx context.Context) Status
	CanApply() bool
	ApplyBy(initiatedBy string) bool
}

// AlertEmitter lo cumple *alerts.Engine (nil = sin alertas, p. ej. tests).
type AlertEmitter interface {
	Emit(ev alerts.AlertEvent) bool
}

// Scheduler ejecuta el auto-update programado.
type Scheduler struct {
	db   *sql.DB
	ua   CheckerApplier
	emit AlertEmitter

	now  func() time.Time
	logf func(format string, args ...any)

	running atomic.Bool
}

// NewScheduler construye el scheduler (Start lo arranca como daemon).
func NewScheduler(db *sql.DB, ua CheckerApplier) *Scheduler {
	return &Scheduler{
		db:  db,
		ua:  ua,
		now: time.Now,
		logf: func(f string, a ...any) {
			log.Printf("[autoupdate] "+f, a...)
		},
	}
}

// SetAlertEmitter fija el motor de alertas (llamado tras construir el
// adapter live en main).
func (s *Scheduler) SetAlertEmitter(e AlertEmitter) { s.emit = e }

// Start lanza el bucle periódico (daemon; no retorna).
func (s *Scheduler) Start() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.tick()
	}
}

func (s *Scheduler) tick() {
	if s.db == nil || s.ua == nil {
		return
	}
	st := LoadAutoUpdateSettings(s.db)
	state := loadAutoRunState(s.db)
	if !AutoUpdateDue(st, state.LastRunMs, s.now()) {
		return
	}
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.running.Store(false)
		s.shot(st, state)
	}()
}

// shot ejecuta un disparo vencido: marca el slot, comprueba y aplica si
// procede. El resultado queda en kv (UI) y en el historial del updater.
func (s *Scheduler) shot(st AutoUpdateSettings, prev autoRunState) {
	nowMs := s.now().UnixMilli()
	setAutoKv(s.db, "last_run", strconv.FormatInt(nowMs, 10))

	status := s.ua.Check(context.Background())
	switch {
	case status.CheckFailed:
		saveAutoResult(s.db, "check-failed")
		s.alert(false, fmt.Sprintf("No se pudo comprobar (razón: %s); se reintentará en el próximo disparo", status.CheckErr), status.CheckErr)
	case !status.UpdateAvailable:
		saveAutoResult(s.db, "up-to-date")
	case !status.CanApply:
		// Readiness o layout sin apply: se avisa (hay novedad esperando y no
		// se podrá aplicar hasta que el admin lo resuelva).
		saveAutoResult(s.db, "cannot-apply")
		s.alert(false, "Hay una actualización disponible pero este layout no puede aplicarla ahora (revisa readiness en Ajustes)", "cannot-apply")
	default:
		saveAutoResult(s.db, "applied")
		if s.ua.ApplyBy("scheduled") {
			s.alert(true, fmt.Sprintf("Actualización programada iniciada: %s (solo binario, con verificación y rollback automático)", statusLatestDeref(status)), "")
		} else {
			s.alert(false, "La actualización programada no pudo arrancar (¿otra actualización en curso?)", "apply-busy")
		}
	}
}

// alert emite la notificación del disparo (info en éxito, warn en aviso).
func (s *Scheduler) alert(ok bool, desc, code string) {
	s.logf("%s", desc)
	if s.emit == nil {
		return
	}
	sev := "warn"
	if ok {
		sev = "info"
	}
	s.emit.Emit(alerts.AlertEvent{
		ID:          fmt.Sprintf("alert-autoupdate-%d", s.now().UnixMilli()),
		Category:    alerts.CatSystem,
		Urgent:      true,
		Severity:    sev,
		Title:       "Auto-actualización programada",
		Description: desc,
		Time:        "ahora mismo",
		Ts:          s.now().Unix(),
		Type:        "autoupdate",
		Vars:        map[string]string{"result": code, "detail": desc},
	})
}

// --- Validación y due (funciones puras) ---

// ValidateAutoUpdate comprueba la coherencia kind/campos.
func ValidateAutoUpdate(st AutoUpdateSettings) error {
	switch st.Kind {
	case AutoDaily:
		if !validAutoTime(st.Time) {
			return fmt.Errorf("daily exige time HH:MM")
		}
	case AutoWeekly:
		if st.DayOfWeek == nil || *st.DayOfWeek < 0 || *st.DayOfWeek > 6 {
			return fmt.Errorf("weekly exige dayOfWeek 0-6 (0 = domingo)")
		}
		if !validAutoTime(st.Time) {
			return fmt.Errorf("weekly exige time HH:MM")
		}
	case AutoMonthly:
		if st.DayOfMonth == nil || *st.DayOfMonth < 1 || *st.DayOfMonth > 31 {
			return fmt.Errorf("monthly exige dayOfMonth 1-31")
		}
		if !validAutoTime(st.Time) {
			return fmt.Errorf("monthly exige time HH:MM")
		}
	default:
		return fmt.Errorf("kind debe ser daily, weekly o monthly")
	}
	return nil
}

// AutoUpdateDue decide si toca disparar (función pura, patrón #761).
func AutoUpdateDue(st AutoUpdateSettings, lastRunMs int64, now time.Time) bool {
	if !st.Enabled {
		return false
	}
	switch st.Kind {
	case AutoDaily:
		slot := autoTodaySlot(st.Time, now)
		return !now.Before(slot) && lastRunMs < slot.UnixMilli()
	case AutoWeekly:
		dow := 0
		if st.DayOfWeek != nil {
			dow = *st.DayOfWeek
		}
		if int(now.Weekday()) != dow {
			return false
		}
		slot := autoTodaySlot(st.Time, now)
		return !now.Before(slot) && lastRunMs < slot.UnixMilli()
	case AutoMonthly:
		dom := 1
		if st.DayOfMonth != nil {
			dom = *st.DayOfMonth
		}
		h, m := autoParseTime(st.Time)
		slot := time.Date(now.Year(), now.Month(), autoClampDay(dom, now), h, m, 0, 0, now.Location())
		return !now.Before(slot) && lastRunMs < slot.UnixMilli()
	}
	return false
}

// NextAutoUpdateAt calcula el próximo disparo para la UI.
func NextAutoUpdateAt(st AutoUpdateSettings, lastRunMs int64, now time.Time) time.Time {
	h, m := autoParseTime(st.Time)
	switch st.Kind {
	case AutoDaily:
		slot := autoTodaySlot(st.Time, now)
		if now.Before(slot) || lastRunMs < slot.UnixMilli() {
			return slot
		}
		return slot.AddDate(0, 0, 1)
	case AutoWeekly:
		dow := 0
		if st.DayOfWeek != nil {
			dow = *st.DayOfWeek
		}
		today := autoTodaySlot(st.Time, now)
		if int(now.Weekday()) == dow && (now.Before(today) || lastRunMs < today.UnixMilli()) {
			return today
		}
		days := (dow - int(now.Weekday()) + 7) % 7
		if days == 0 {
			days = 7
		}
		d := now.AddDate(0, 0, days)
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, now.Location())
	case AutoMonthly:
		dom := 1
		if st.DayOfMonth != nil {
			dom = *st.DayOfMonth
		}
		slot := time.Date(now.Year(), now.Month(), autoClampDay(dom, now), h, m, 0, 0, now.Location())
		if now.Before(slot) || lastRunMs < slot.UnixMilli() {
			return slot
		}
		next := now.AddDate(0, 1, 0)
		return time.Date(next.Year(), next.Month(), autoClampDay(dom, next), h, m, 0, 0, now.Location())
	}
	return time.Time{}
}

// --- Persistencia kv ---

const autoKvPrefix = "settings.autoupdate."

// LoadAutoUpdateSettings lee los ajustes (defaults sanos).
func LoadAutoUpdateSettings(db *sql.DB) AutoUpdateSettings {
	st := AutoUpdateSettings{}
	if db == nil {
		return st
	}
	st.Enabled = autoKVGet(db, "enabled") == "1"
	if k := autoKVGet(db, "kind"); validAutoKind(k) {
		st.Kind = k
	}
	st.Time = autoKVGet(db, "time")
	if v, ok := autoKVIntPtr(db, "dow"); ok {
		st.DayOfWeek = v
	}
	if v, ok := autoKVIntPtr(db, "dom"); ok {
		st.DayOfMonth = v
	}
	return st
}

// SaveAutoUpdateSettings valida y persiste los ajustes (sin tocar last_run).
func SaveAutoUpdateSettings(db *sql.DB, st AutoUpdateSettings) error {
	if db == nil {
		return fmt.Errorf("autoupdate: sin base de datos")
	}
	if err := ValidateAutoUpdate(st); err != nil {
		return err
	}
	setAutoKv(db, "enabled", autoBoolStr(st.Enabled))
	setAutoKv(db, "kind", st.Kind)
	setAutoKv(db, "time", st.Time)
	dow, dom := "", ""
	if st.DayOfWeek != nil {
		dow = strconv.Itoa(*st.DayOfWeek)
	}
	if st.DayOfMonth != nil {
		dom = strconv.Itoa(*st.DayOfMonth)
	}
	setAutoKv(db, "dow", dow)
	setAutoKv(db, "dom", dom)
	return nil
}

func loadAutoRunState(db *sql.DB) autoRunState {
	return autoRunState{
		LastRunMs:  autoKVParseInt(db, "last_run"),
		LastResult: autoKVGet(db, "last_result"),
	}
}

// AutoRunState expone el estado del último disparo (para la UI).
func AutoRunState(db *sql.DB) (int64, string) {
	st := loadAutoRunState(db)
	return st.LastRunMs, st.LastResult
}

func saveAutoResult(db *sql.DB, result string) {
	setAutoKv(db, "last_result", result)
}

// --- helpers locales (patrón speedtest/firmware) ---

func validAutoKind(v string) bool {
	return v == AutoDaily || v == AutoWeekly || v == AutoMonthly
}

func validAutoTime(v string) bool {
	if v == "" {
		return false
	}
	_, err := time.Parse("15:04", v)
	return err == nil
}

func autoParseTime(v string) (int, int) {
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, 0
	}
	return t.Hour(), t.Minute()
}

func autoTodaySlot(v string, now time.Time) time.Time {
	h, m := autoParseTime(v)
	return time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
}

func autoDaysInMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}

func autoClampDay(dom int, t time.Time) int {
	if dom < 1 {
		dom = 1
	}
	if max := autoDaysInMonth(t); dom > max {
		dom = max
	}
	return dom
}

func autoBoolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func autoKVGet(db *sql.DB, key string) string {
	var v string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", autoKvPrefix+key).Scan(&v); err != nil {
		return ""
	}
	return v
}

func autoKVParseInt(db *sql.DB, key string) int64 {
	n, _ := strconv.ParseInt(autoKVGet(db, key), 10, 64)
	return n
}

func autoKVIntPtr(db *sql.DB, key string) (*int, bool) {
	raw := autoKVGet(db, key)
	if raw == "" {
		return nil, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, false
	}
	return &n, true
}

func setAutoKv(db *sql.DB, key, val string) {
	if _, err := db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		autoKvPrefix+key, val); err != nil {
		log.Printf("[autoupdate] kv set %s: %v", key, err)
	}
}

// LatestDeref devuelve status.Latest sin nil-panic (para mensajes).
func statusLatestDeref(s Status) string {
	if s.Latest != nil {
		return *s.Latest
	}
	return "?"
}
