// retention.go - retención temporal configurable del log de alertas (#1034).
//
// Hasta ahora alert_log solo se podaba por CANTIDAD (PersistMaxEvents = 500
// filas). Esto añade una poda por ANTIGÜEDAD: alerts.retentionDays en kv
// (default 30 días; 0 = poda temporal desactivada, la cota de filas sigue
// aplicando en cada persist). Lectura por evento del kv (patrón
// WeakSignalDbm): cambiar el ajuste no requiere reinicio. La poda corre al
// arrancar el engine y cada hora desde el loop de main.
package alerts

import (
	"database/sql"
	"strconv"
	"time"
)

// KeyRetentionDays es la clave kv del ajuste (días de retención del log).
const KeyRetentionDays = "alerts.retentionDays"

const (
	// DefaultRetentionDays: un mes de histórico de alertas en disco.
	DefaultRetentionDays = 30
	maxRetentionDays     = 365
)

// RetentionDays devuelve la retención efectiva del log (días). Valores
// corruptos o fuera de rango devuelven el default. 0 = poda temporal off.
func RetentionDays(db *sql.DB) int {
	if db == nil {
		return DefaultRetentionDays
	}
	var raw string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", KeyRetentionDays).Scan(&raw); err != nil {
		return DefaultRetentionDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > maxRetentionDays {
		return DefaultRetentionDays
	}
	return n
}

// PruneLogByRetention aplica la retención temporal sobre alert_log: borra las
// filas con ts anterior a ahora - retentionDays y las retira de la lista en
// memoria para que el feed sea consistente sin reinicio. Las alertas
// VOLÁTILES (#966) nunca salen: viven solo en memoria y representan una
// condición aún activa (p.ej. agente caído desde hace 40 días). Devuelve las
// filas borradas del log. Best-effort: un fallo de BD nunca rompe el camino
// de alertado.
func (e *Engine) PruneLogByRetention() int64 {
	if e.db == nil || !e.persistLog {
		return 0
	}
	days := RetentionDays(e.db.DB)
	if days <= 0 {
		return 0
	}
	e.mu.Lock()
	cutoff := e.now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	kept := e.list[:0]
	for _, ev := range e.list {
		if ev.Ts >= cutoff || ev.Volatile {
			kept = append(kept, ev)
		}
	}
	e.list = kept
	e.mu.Unlock()
	res, err := e.db.Exec("DELETE FROM alert_log WHERE ts < ?", cutoff)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}
