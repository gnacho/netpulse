// recurrence.go - programación recurrente de firmware por router (#761).
//
// Patrón de speedtest (#744) y backups (#741): ajustes en kv, vencimiento
// derivado del last_run PERSISTIDO (reinicios/auto-update no resetean el
// reloj) y decisión pura RecurrenceDue. La ejecución es idempotente por
// contrato: el disparo solo flashea si la versión instalada difiere del
// target guardado; un router al día es un no-op silencioso.
package firmware

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"
)

// Kinds de recurrencia.
const (
	RecurrenceOnce    = "once"
	RecurrenceWeekly  = "weekly"
	RecurrenceMonthly = "monthly"
)

// Recurrence es la programación persistida de un router.
type Recurrence struct {
	Enabled bool `json:"enabled"`
	// Kind: once (AtMs fijo, se desactiva tras disparar) | weekly
	// (DayOfWeek a Time local) | monthly (DayOfMonth a Time local; clamp
	// al último día si el mes no lo tiene).
	Kind       string `json:"kind"`
	AtMs       int64  `json:"atMs,omitempty"`       // once, epoch ms
	DayOfWeek  *int   `json:"dayOfWeek,omitempty"`  // 0=domingo..6 (weekly)
	DayOfMonth *int   `json:"dayOfMonth,omitempty"` // 1..31 (monthly)
	Time       string `json:"time,omitempty"`       // "HH:MM" hora local
	LastRunMs  int64  `json:"lastRunMs,omitempty"`  // último disparo (cualquier resultado)
}

// ValidateRecurrence comprueba la coherencia kind/campos (patrón speedtest).
func ValidateRecurrence(rc Recurrence) error {
	switch rc.Kind {
	case RecurrenceOnce:
		if rc.AtMs <= 0 {
			return fmt.Errorf("once exige atMs (epoch ms)")
		}
	case RecurrenceWeekly:
		if rc.DayOfWeek == nil || *rc.DayOfWeek < 0 || *rc.DayOfWeek > 6 {
			return fmt.Errorf("weekly exige dayOfWeek 0-6 (0 = domingo)")
		}
		if !validRecTime(rc.Time) {
			return fmt.Errorf("weekly exige time HH:MM")
		}
	case RecurrenceMonthly:
		if rc.DayOfMonth == nil || *rc.DayOfMonth < 1 || *rc.DayOfMonth > 31 {
			return fmt.Errorf("monthly exige dayOfMonth 1-31")
		}
		if !validRecTime(rc.Time) {
			return fmt.Errorf("monthly exige time HH:MM")
		}
	default:
		return fmt.Errorf("kind debe ser once, weekly o monthly")
	}
	return nil
}

// RecurrenceDue decide si toca disparar (función pura). El vencimiento
// siempre deriva de LastRunMs: weekly exige día exacto de la semana y que el
// slot de hoy sea posterior al último disparo; monthly aplica clamp de fin de
// mes; once dispara una única vez (AtMs) aunque LastRunMs sea 0.
func RecurrenceDue(rc Recurrence, now time.Time) bool {
	if !rc.Enabled {
		return false
	}
	switch rc.Kind {
	case RecurrenceOnce:
		return now.UnixMilli() >= rc.AtMs && rc.LastRunMs < rc.AtMs
	case RecurrenceWeekly:
		dow := 0
		if rc.DayOfWeek != nil {
			dow = *rc.DayOfWeek
		}
		if int(now.Weekday()) != dow {
			return false
		}
		slot := recTodaySlot(rc.Time, now)
		return !now.Before(slot) && rc.LastRunMs < slot.UnixMilli()
	case RecurrenceMonthly:
		dom := 1
		if rc.DayOfMonth != nil {
			dom = *rc.DayOfMonth
		}
		h, m := recParseTime(rc.Time)
		slot := time.Date(now.Year(), now.Month(), recClampDay(dom, now), h, m, 0, 0, now.Location())
		return !now.Before(slot) && rc.LastRunMs < slot.UnixMilli()
	}
	return false
}

// NextRecurrenceAt calcula el próximo disparo para la UI (Status).
func NextRecurrenceAt(rc Recurrence, now time.Time) time.Time {
	h, m := recParseTime(rc.Time)
	switch rc.Kind {
	case RecurrenceOnce:
		return time.UnixMilli(rc.AtMs)
	case RecurrenceWeekly:
		dow := 0
		if rc.DayOfWeek != nil {
			dow = *rc.DayOfWeek
		}
		today := recTodaySlot(rc.Time, now)
		if int(now.Weekday()) == dow && (now.Before(today) || rc.LastRunMs < today.UnixMilli()) {
			return today
		}
		days := (dow - int(now.Weekday()) + 7) % 7
		if days == 0 {
			days = 7
		}
		d := now.AddDate(0, 0, days)
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, now.Location())
	case RecurrenceMonthly:
		dom := 1
		if rc.DayOfMonth != nil {
			dom = *rc.DayOfMonth
		}
		slot := time.Date(now.Year(), now.Month(), recClampDay(dom, now), h, m, 0, 0, now.Location())
		if now.Before(slot) || rc.LastRunMs < slot.UnixMilli() {
			return slot
		}
		next := now.AddDate(0, 1, 0)
		return time.Date(next.Year(), next.Month(), recClampDay(dom, next), h, m, 0, 0, now.Location())
	}
	return time.Time{}
}

// --- Persistencia kv (claves firmware.recurrence.<router>.<campo>) ---

func recKey(routerID, field string) string {
	return "firmware.recurrence." + routerID + "." + field
}

// LoadRecurrence lee la programación del router (valores ausentes = default).
func LoadRecurrence(db *sql.DB, routerID string) Recurrence {
	rc := Recurrence{}
	if db == nil || routerID == "" {
		return rc
	}
	rc.Enabled = recKVGet(db, recKey(routerID, "enabled")) == "1"
	rc.Kind = recKVGet(db, recKey(routerID, "kind"))
	rc.AtMs = recKVParseInt(db, recKey(routerID, "at_ms"))
	rc.LastRunMs = recKVParseInt(db, recKey(routerID, "last_run"))
	if v, ok := recKVIntPtr(db, recKey(routerID, "dow")); ok {
		rc.DayOfWeek = v
	}
	if v, ok := recKVIntPtr(db, recKey(routerID, "dom")); ok {
		rc.DayOfMonth = v
	}
	rc.Time = recKVGet(db, recKey(routerID, "time"))
	return rc
}

// SaveRecurrence persiste la programación completa (UPSERT por clave).
func SaveRecurrence(db *sql.DB, routerID string, rc Recurrence) error {
	if db == nil || routerID == "" {
		return fmt.Errorf("firmware: db o router vacío")
	}
	if err := ValidateRecurrence(rc); err != nil {
		return err
	}
	recKVSet(db, recKey(routerID, "enabled"), boolRecStr(rc.Enabled))
	recKVSet(db, recKey(routerID, "kind"), rc.Kind)
	recKVSet(db, recKey(routerID, "at_ms"), strconv.FormatInt(rc.AtMs, 10))
	recKVSet(db, recKey(routerID, "last_run"), strconv.FormatInt(rc.LastRunMs, 10))
	dow, dom := "", ""
	if rc.DayOfWeek != nil {
		dow = strconv.Itoa(*rc.DayOfWeek)
	}
	if rc.DayOfMonth != nil {
		dom = strconv.Itoa(*rc.DayOfMonth)
	}
	recKVSet(db, recKey(routerID, "dow"), dow)
	recKVSet(db, recKey(routerID, "dom"), dom)
	recKVSet(db, recKey(routerID, "time"), rc.Time)
	return nil
}

// SetRecurrenceLastRun actualiza solo el último disparo (lo toca el loop).
func SetRecurrenceLastRun(db *sql.DB, routerID string, ms int64) {
	recKVSet(db, recKey(routerID, "last_run"), strconv.FormatInt(ms, 10))
}

// DisableRecurrence apaga la programación (config inválida detectada en
// runtime: sin target guardado, etc.).
func DisableRecurrence(db *sql.DB, routerID string) {
	recKVSet(db, recKey(routerID, "enabled"), "0")
}

// --- helpers (patrón speedtest, locales del paquete) ---

func validRecTime(v string) bool {
	if v == "" {
		return false
	}
	_, err := time.Parse("15:04", v)
	return err == nil
}

func recParseTime(v string) (int, int) {
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, 0
	}
	return t.Hour(), t.Minute()
}

func recDaysInMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}

func recClampDay(dom int, t time.Time) int {
	if dom < 1 {
		dom = 1
	}
	if max := recDaysInMonth(t); dom > max {
		dom = max
	}
	return dom
}

func recTodaySlot(v string, now time.Time) time.Time {
	h, m := recParseTime(v)
	return time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
}

func boolRecStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func recKVGet(db *sql.DB, key string) string {
	var v string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&v); err != nil {
		return ""
	}
	return v
}

func recKVParseInt(db *sql.DB, key string) int64 {
	n, _ := strconv.ParseInt(recKVGet(db, key), 10, 64)
	return n
}

func recKVIntPtr(db *sql.DB, key string) (*int, bool) {
	raw := recKVGet(db, key)
	if raw == "" {
		return nil, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, false
	}
	return &n, true
}

func recKVSet(db *sql.DB, key, val string) {
	if _, err := db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, val); err != nil {
		log.Printf("[firmware] kv set %s: %v", key, err)
	}
}
