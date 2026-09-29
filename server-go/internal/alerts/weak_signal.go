// weak_signal.go - umbral configurable de la alerta de señal débil (#904).
//
// Hasta ahora la alerta "Señal débil" se disparaba con -70 dBm hardcodeado
// y el ajuste "Weak signal" de Ajustes solo alimentaba un caption local del
// navegador: cambiarlo no alteraba ni la alerta ni nada observable. El
// umbral vive ahora en kv (alerts.weak_signal_dbm, default -70, clamp
// -90..-50) para que sea server-wide (mismo valor para todos los clientes)
// y pueda alimentar la alerta, la app móvil y el matrix filter.
package alerts

import (
	"database/sql"
	"strconv"
)

const KeyWeakSignalDbm = "alerts.weak_signal_dbm"

const (
	DefaultWeakSignalDbm = -70
	minWeakSignalDbm     = -90
	maxWeakSignalDbm     = -50
)

// WeakSignalDbm devuelve el umbral efectivo (dBm). Valores corruptos o fuera
// de rango devuelven el default.
func WeakSignalDbm(db *sql.DB) int {
	if db == nil {
		return DefaultWeakSignalDbm
	}
	var raw string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", KeyWeakSignalDbm).Scan(&raw); err != nil {
		return DefaultWeakSignalDbm
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < minWeakSignalDbm || n > maxWeakSignalDbm {
		return DefaultWeakSignalDbm
	}
	return n
}
