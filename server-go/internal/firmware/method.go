// method.go — método de upgrade preferido por unidad (#1414). Persistido en
// kv (firmware.method.<routerId>): el selector de la tarjeta decide qué
// sección de la página se muestra y qué motor dispara "Upgrade now".
package firmware

import (
	"database/sql"
	"fmt"
	"strings"
)

// Métodos del selector por unidad.
const (
	// MethodAuto: comportamiento actual (secciones owut y URL visibles a la
	// vez). Es el default: no rompe a quien ya usa la página.
	MethodAuto   = "auto"
	MethodOwut   = "owut"
	MethodURL    = "url"
	MethodUpload = "upload"
)

const methodKeyPrefix = "firmware.method."

// ValidMethod reporta si m es un método aceptable.
func ValidMethod(m string) bool {
	switch m {
	case MethodAuto, MethodOwut, MethodURL, MethodUpload:
		return true
	}
	return false
}

// LoadMethod devuelve el método persistido del router; MethodAuto si no hay
// o hay valor inválido.
func LoadMethod(db *sql.DB, routerID string) string {
	if db == nil {
		return MethodAuto
	}
	var v string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", methodKeyPrefix+routerID).Scan(&v); err != nil {
		return MethodAuto
	}
	v = strings.TrimSpace(v)
	if !ValidMethod(v) {
		return MethodAuto
	}
	return v
}

// SaveMethod persiste el método del router. Valida antes de escribir.
func SaveMethod(db *sql.DB, routerID, method string) error {
	if db == nil {
		return fmt.Errorf("no database")
	}
	method = strings.TrimSpace(method)
	if !ValidMethod(method) {
		return fmt.Errorf("invalid method %q", method)
	}
	_, err := db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		methodKeyPrefix+routerID, method)
	return err
}
