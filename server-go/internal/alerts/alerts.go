// Package alerts — motor de alertas (SPEC-ALERTAS §1-3): dueño de la lista
// de eventos (máx 100, más recientes primero), config por categoría con 3
// niveles (none/urgent/all) aplicada EN CREACIÓN, dedup de 5 min por
// (category,title,routerId), read-state en servidor y hook Notifier para el
// push (Bloque C; nil = no-op).
//
// Persistencia: tabla kv (misma DB que el resto del servidor):
//   - alerts.config.v1: JSON {"router":"urgent",...} (6 claves, defaults).
//   - alerts.read.v1:   JSON array de IDs leídos (cap 200, FIFO).
package alerts

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// Categorías (taxonomía exacta del SPEC §1).
const (
	CatRouter   = "router"
	CatInternet = "internet"
	CatClients  = "clients"
	CatSignal   = "signal"
	CatVPN      = "vpn"
	CatSystem   = "system"
)

// Categories lista las 6 categorías válidas (orden estable para mensajes).
var Categories = []string{CatRouter, CatInternet, CatClients, CatSignal, CatVPN, CatSystem}

// Niveles de configuración por categoría (SPEC §2).
const (
	LevelNone   = "none"   // el evento se descarta (no se guarda, no cuenta, no empuja)
	LevelUrgent = "urgent" // solo pasan eventos Urgent=true
	LevelAll    = "all"    // pasan todos (los urgentes además notifican)
)

var levels = map[string]bool{LevelNone: true, LevelUrgent: true, LevelAll: true}

// Claves kv y límites (SPEC §2-3).
const (
	configKey   = "alerts.config.v1"
	readKey     = "alerts.read.v1"
	MaxEvents   = 100
	MaxReadIDs  = 200
	DedupWindow = 5 * time.Minute
)

// AlertEvent (máx 100 en memoria, más recientes primero).
type AlertEvent struct {
	ID          string `json:"id"`
	Category    string `json:"category"` // taxonomía SPEC §1
	Urgent      bool   `json:"urgent"`   // "rompe silencio" (push + badge)
	Severity    string `json:"severity"` // display: "warn"|"critical"|"info"|"ok"
	Title       string `json:"title"`
	Description string `json:"description"`
	Time        string `json:"time"` // LEGADO display; se sigue rellenando
	Ts          int64  `json:"ts"`   // unix SEGUNDOS; el frontend calcula el relativo
	Read        bool   `json:"read"`
	RouterID    string `json:"routerId"`
}

// Notifier es el hook de push (Bloque C lo implementa; ahora nil/no-op).
// Se llama SOLO para eventos que pasan config Y son Urgent=true.
type Notifier interface {
	Notify(ev AlertEvent)
}

// DefaultConfig devuelve una copia de los defaults del SPEC §2.
// clients=all desde issue #196: "dispositivo desconocido" ya no es urgente
// pero debe seguir siendo visible (con clients:urgent se descartaría entero).
func DefaultConfig() map[string]string {
	return map[string]string{
		CatRouter:   LevelUrgent,
		CatInternet: LevelUrgent,
		CatClients:  LevelAll,
		CatSignal:   LevelNone,
		CatVPN:      LevelNone,
		CatSystem:   LevelAll,
	}
}

// Engine es el motor: aplica config/dedup/cap en Emit y guarda el read-state.
// Seguro para uso concurrente (los adapters emiten desde varios goroutines).
type Engine struct {
	mu       sync.Mutex
	db       *db.DB // nil → solo memoria (tests)
	notifier Notifier
	now      func() time.Time

	cfg     map[string]string
	list    []AlertEvent
	dedup   map[string]int64 // key (cat|title|routerId) → último emit (unix ms)
	readSet map[string]bool
	readOrd []string // FIFO de IDs (cap 200) para podar readSet
}

// New crea el motor cargando config y read-state de kv (si db != nil).
func New(d *db.DB, n Notifier) *Engine {
	e := &Engine{
		db:       d,
		notifier: n,
		now:      time.Now,
		cfg:      DefaultConfig(),
		dedup:    map[string]int64{},
		readSet:  map[string]bool{},
	}
	if d != nil {
		var raw string
		if err := d.QueryRow("SELECT value FROM kv WHERE key = ?", configKey).Scan(&raw); err == nil {
			saved := map[string]string{}
			if json.Unmarshal([]byte(raw), &saved) == nil {
				for _, cat := range Categories {
					if lv, ok := saved[cat]; ok && levels[lv] {
						e.cfg[cat] = lv
					}
				}
			}
		}
		if err := d.QueryRow("SELECT value FROM kv WHERE key = ?", readKey).Scan(&raw); err == nil {
			ids := []string{}
			if json.Unmarshal([]byte(raw), &ids) == nil {
				for _, id := range ids {
					if id != "" && !e.readSet[id] {
						e.readSet[id] = true
						e.readOrd = append(e.readOrd, id)
					}
				}
			}
		}
	}
	return e
}

// SetNotifier sustituye el hook Notifier (wiring de Bloque C desde main;
// nil = no-op). Seguro en caliente: Emit lo lee bajo el mismo mutex.
func (e *Engine) SetNotifier(n Notifier) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notifier = n
}

// SetClock sustituye el reloj (solo tests).
func (e *Engine) SetClock(f func() time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = f
}

// Config devuelve una copia de la config efectiva (siempre las 6 claves).
func (e *Engine) Config() map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]string, len(e.cfg))
	for k, v := range e.cfg {
		out[k] = v
	}
	return out
}

// SetConfig aplica un parche parcial {"signal":"all"}: valida categorías y
// niveles (error → 400 en la API), fusiona y persiste en kv.
func (e *Engine) SetConfig(patch map[string]string) error {
	badCats, badVals := []string{}, []string{}
	for k, v := range patch {
		if !contains(Categories, k) {
			badCats = append(badCats, k)
		} else if !levels[v] {
			badVals = append(badVals, fmt.Sprintf("%s=%q", k, v))
		}
	}
	if len(badCats) > 0 || len(badVals) > 0 {
		sort.Strings(badCats)
		sort.Strings(badVals)
		parts := []string{}
		if len(badCats) > 0 {
			parts = append(parts, "categorías desconocidas: "+strings.Join(badCats, ", "))
		}
		if len(badVals) > 0 {
			parts = append(parts, "niveles inválidos: "+strings.Join(badVals, ", "))
		}
		return fmt.Errorf("config inválida (%s)", strings.Join(parts, "; "))
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, v := range patch {
		e.cfg[k] = v
	}
	e.saveConfigLocked()
	return nil
}

// Emit aplica la semántica none/urgent/all EN CREACIÓN, el dedup de 5 min y
// el cap de 100; devuelve true si el evento pasó (se guardó). Los urgentes
// que pasan disparan el Notifier (hook de Bloque C).
func (e *Engine) Emit(ev AlertEvent) bool {
	e.mu.Lock()
	level, known := e.cfg[ev.Category]
	if known {
		if level == LevelNone || (level == LevelUrgent && !ev.Urgent) {
			e.mu.Unlock()
			return false
		}
	}
	// Categoría desconocida: pasa (la validación estricta es de SetConfig).
	now := e.now()
	key := ev.Category + "|" + ev.Title + "|" + ev.RouterID
	if last, ok := e.dedup[key]; ok && now.UnixMilli()-last < DedupWindow.Milliseconds() {
		e.mu.Unlock()
		return false
	}
	e.dedup[key] = now.UnixMilli()
	if ev.Ts == 0 {
		ev.Ts = now.Unix()
	}
	if ev.Time == "" {
		ev.Time = "ahora mismo"
	}
	ev.Read = false
	e.list = append([]AlertEvent{ev}, e.list...)
	if len(e.list) > MaxEvents {
		e.list = e.list[:MaxEvents]
	}
	n := e.notifier
	e.mu.Unlock()
	if n != nil && ev.Urgent {
		n.Notify(ev)
	}
	return true
}

// Seed inserta un evento histórico (arranque del modo demo) SIN aplicar el
// filtro de config — el SPEC §5 exige que las 5 canon sobrevivan con los
// defaults aunque "vpn:none"/"clients:urgent" descartarían 2 en creación —.
// Mantiene dedup, cap y Read=false; NO dispara el Notifier (son historia).
// Devuelve true si se guardó.
func (e *Engine) Seed(ev AlertEvent) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	key := ev.Category + "|" + ev.Title + "|" + ev.RouterID
	if last, ok := e.dedup[key]; ok && now.UnixMilli()-last < DedupWindow.Milliseconds() {
		return false
	}
	e.dedup[key] = now.UnixMilli()
	if ev.Ts == 0 {
		ev.Ts = now.Unix()
	}
	if ev.Time == "" {
		ev.Time = "ahora mismo"
	}
	ev.Read = false
	e.list = append([]AlertEvent{ev}, e.list...)
	if len(e.list) > MaxEvents {
		e.list = e.list[:MaxEvents]
	}
	return true
}

// List devuelve copia de los eventos (más recientes primero) con Read
// aplicado desde el read-set del servidor.
func (e *Engine) List() []AlertEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]AlertEvent, len(e.list))
	for i, ev := range e.list {
		ev.Read = e.readSet[ev.ID]
		out[i] = ev
	}
	return out
}

// UnreadCount cuenta eventos almacenados (ya pasaron config) no leídos.
// Es la fuente de Overview.UnreadAlerts (server truth, SPEC §4).
func (e *Engine) UnreadCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.list {
		if !e.readSet[ev.ID] {
			n++
		}
	}
	return n
}

// MarkRead marca IDs como leídos (persiste; cap 200 FIFO).
func (e *Engine) MarkRead(ids ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, id := range ids {
		if id == "" || e.readSet[id] {
			continue
		}
		e.readSet[id] = true
		e.readOrd = append(e.readOrd, id)
	}
	for len(e.readOrd) > MaxReadIDs {
		delete(e.readSet, e.readOrd[0])
		e.readOrd = e.readOrd[1:]
	}
	e.saveReadLocked()
}

// MarkAllRead marca leídas todas las alertas actuales.
func (e *Engine) MarkAllRead() {
	e.mu.Lock()
	ids := make([]string, 0, len(e.list))
	for _, ev := range e.list {
		ids = append(ids, ev.ID)
	}
	e.mu.Unlock()
	e.MarkRead(ids...)
}

func (e *Engine) saveConfigLocked() {
	if e.db == nil {
		return
	}
	raw, err := json.Marshal(e.cfg)
	if err != nil {
		return
	}
	_, _ = e.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		configKey, string(raw))
}

func (e *Engine) saveReadLocked() {
	if e.db == nil {
		return
	}
	raw, err := json.Marshal(e.readOrd)
	if err != nil {
		return
	}
	_, _ = e.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		readKey, string(raw))
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// IsCategory reporta si v es una categoría válida (validación de query).
func IsCategory(v string) bool { return contains(Categories, v) }
