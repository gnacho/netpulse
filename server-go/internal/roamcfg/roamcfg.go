// Package roamcfg - ajustes configurables del collector de eventos de
// roaming (#907). Hoy una sola clave kv, con default y clamp:
//
//	roam.collect.interval_sec (default 60, 15..3600) - cadencia del
//	    collector de logread por router.
//
// La RETENCIÓN del histórico NO vive aquí: reutiliza presence.retention_days
// (ajuste existente en Ajustes con su loop de poda en main). Este paquete es
// importable desde roamevents y httpapi sin ciclos.
package roamcfg

import (
	"database/sql"
	"strconv"
)

const (
	KeyCollectInterval = "roam.collect.interval_sec"

	DefaultCollectInterval = 60

	minCollectSec = 15
	maxCollectSec = 3600
)

// GetInt lee una clave kv entera con default y clamp. Valores fuera de rango
// o no numéricos devuelven el default (un valor corrupto no debe tumbar el
// collector).
func GetInt(db *sql.DB, key string, def, min, max int) int {
	if db == nil {
		return def
	}
	var raw string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&raw); err != nil {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// CollectIntervalSec: cadencia del collector de logread, en segundos.
func CollectIntervalSec(db *sql.DB) int {
	return GetInt(db, KeyCollectInterval, DefaultCollectInterval, minCollectSec, maxCollectSec)
}
