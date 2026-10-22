// hide_recovery.go - ocultar los eventos de recuperación del feed (#1518).
//
// Los eventos severity "ok" son cierres informativos (señal recuperada,
// alerta resuelta, agente recuperado...): no piden ninguna acción. Con el
// ajuste activo (alerts.hide_recovery en kv, default off) se filtran de
// List() y de UnreadCount(): no aparecen en el feed ni despiertan el
// contador de no leídas. Los avisos originales resueltos (resolvedTs,
// #1349) siguen consultables en el filtro Resueltas. Lectura por llamada
// del kv (patrón RetentionDays): cambiar el ajuste no requiere reinicio.
package alerts

import "database/sql"

// KeyHideRecovery es la clave kv del ajuste ("1" = ocultar).
const KeyHideRecovery = "alerts.hide_recovery"

// HideRecoveryEvents devuelve el estado del ajuste. Valores corruptos o
// ausentes: off (comportamiento por defecto, todo visible).
func HideRecoveryEvents(db *sql.DB) bool {
	if db == nil {
		return false
	}
	var raw string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", KeyHideRecovery).Scan(&raw); err != nil {
		return false
	}
	return raw == "1"
}

// hideRecoveryLocked dice si el feed debe ocultar los eventos severity ok.
// La consulta al kv se hace bajo e.mu, igual que las escrituras de
// persistLocked: una sola lectura por llamada de List()/UnreadCount().
func (e *Engine) hideRecoveryLocked() bool {
	if e.db == nil {
		return false
	}
	return HideRecoveryEvents(e.db.DB)
}

// NotifyChange dispara el callback onChange (#1434) sin mutar nada. Para
// ajustes que alteran lo que List()/UnreadCount() sirven (p. ej. ocultar
// eventos de recuperación) y quieren re-emitir el feed por SSE al momento.
func (e *Engine) NotifyChange() {
	e.mu.Lock()
	cb := e.onChange
	e.mu.Unlock()
	if cb != nil {
		cb()
	}
}
