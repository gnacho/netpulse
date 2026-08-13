// history.go — historial de actualizaciones (issue #159): tabla SQLite
// update_history con un registro por apply. Al lanzar el update se inserta
// una entrada 'running'; la goroutine que vigila el script la finaliza a
// success/failed con duración. Si el servicio se reinicia a mitad (el script
// de update toca .restart-me), la entrada queda 'running' y el siguiente
// arranque la marca como fallida (finalizeInterrupted).
package updater

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// HistoryEntry es un registro de update_history. En el canal rolling main
// version_from/to son SHAs cortos de commit; en estable, tags semver.
type HistoryEntry struct {
	ID          int64   `json:"id"`
	EventID     string  `json:"eventId"`
	TS          int64   `json:"ts"`
	Action      string  `json:"action"`
	Channel     string  `json:"channel"`
	VersionFrom *string `json:"versionFrom,omitempty"`
	VersionTo   *string `json:"versionTo,omitempty"`
	InitiatedBy string  `json:"initiatedBy"`
	Status      string  `json:"status"` // running | success | failed
	DurationMS  *int64  `json:"durationMs,omitempty"`
	BackupPath  *string `json:"backupPath,omitempty"`
	Error       *string `json:"error,omitempty"`
}

// newEventID genera un event_id único para el historial.
func newEventID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("upd-%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

// recordStart inserta la entrada 'running' del apply y devuelve su id.
// Devuelve -1 si no hay BD (historial deshabilitado, p. ej. en tests).
func (u *Updater) recordStart(from string, to *string, initiatedBy string) int64 {
	if u.db == nil {
		return -1
	}
	res, err := u.db.Exec(
		`INSERT INTO update_history
		   (event_id, ts, action, channel, version_from, version_to, initiated_by, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'running')`,
		newEventID(), time.Now().UnixMilli(), "apply", u.mode, from, to, initiatedBy)
	if err != nil {
		fmt.Printf("[netpulse] no se pudo registrar el historial de update: %v\n", err)
		return -1
	}
	id, _ := res.LastInsertId()
	return id
}

// finishHistory finaliza una entrada (success/failed) con duración y error.
func (u *Updater) finishHistory(id int64, status, errStr string, dur time.Duration) {
	if u.db == nil || id < 0 {
		return
	}
	ms := dur.Milliseconds()
	var errVal any
	if errStr != "" {
		errVal = errStr
	}
	if _, err := u.db.Exec(
		`UPDATE update_history SET status = ?, duration_ms = ?, error = ? WHERE id = ?`,
		status, ms, errVal, id); err != nil {
		fmt.Printf("[netpulse] no se pudo finalizar el historial de update: %v\n", err)
	}
}

// finalizeInterrupted marca como fallidas las entradas 'running' que quedaron
// de un apply interrumpido por el reinicio del servicio.
func (u *Updater) finalizeInterrupted() {
	if u.db == nil {
		return
	}
	msg := "interrupted_by_restart"
	if _, err := u.db.Exec(
		`UPDATE update_history SET status = 'failed', error = ? WHERE status = 'running'`, msg); err != nil {
		fmt.Printf("[netpulse] no se pudo finalizar historial interrumpido: %v\n", err)
	}
}

// ListHistory devuelve los últimos `limit` registros de update_history
// (issue #159), más reciente primero. Cota de 200 por petición.
func ListHistory(db *sql.DB, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.Query(`
		SELECT id, event_id, ts, action, channel, version_from, version_to,
		       initiated_by, status, duration_ms, backup_path, error
		FROM update_history
		ORDER BY ts DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var e HistoryEntry
		var from, to, backup, errStr sql.NullString
		var dur sql.NullInt64
		if err := rows.Scan(&e.ID, &e.EventID, &e.TS, &e.Action, &e.Channel,
			&from, &to, &e.InitiatedBy, &e.Status, &dur, &backup, &errStr); err != nil {
			return nil, err
		}
		if from.Valid {
			e.VersionFrom = &from.String
		}
		if to.Valid {
			e.VersionTo = &to.String
		}
		if dur.Valid {
			ms := dur.Int64
			e.DurationMS = &ms
		}
		if backup.Valid {
			e.BackupPath = &backup.String
		}
		if errStr.Valid {
			e.Error = &errStr.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
