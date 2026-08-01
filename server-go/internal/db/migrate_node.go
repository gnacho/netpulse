// migrate_node.go — MIGRACIÓN desde el backend Node (SPEC §12, corazón del
// refactor). El esquema Go es idéntico al de Node (mismo SQL literal), así
// que la "importación" es preservación in situ:
//
//  1. Detección: el fichero existe y tiene las tablas firma de Node
//     (users, sessions, kv) sin el marcador de migración ya aplicada.
//  2. Backup atómico (checkpoint TRUNCATE + copia a fichero temporal +
//     rename) ANTES de tocar nada: netpulse.db.bak-<timestamp>.
//  3. Importación preservando: users (hashes bcrypt $2a/$2b portables),
//     kv COMPLETO (session_secret incluido → las sesiones vivas siguen
//     válidas, SIN forzar re-login), sessions vivas, routers (routerstore
//     persiste en la propia tabla routers de SQLite — nada externo que
//     importar), device_attrib, metrics/adguard_stats (epochs en MS, no se
//     tocan), y login_attempts RESETEADO (efímero).
//  4. Log claro de qué se importó y cuántas filas.
//
// El marcador kv `go_migration` evita re-migrar en cada arranque (repetir el
// reset de login_attempts rompería la persistencia del rate-limit, que en
// Node sobrevive a reinicios). Es un detalle interno no observable por la API.
package db

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

// migrationMarkerKey es la clave kv que marca la migración ya hecha.
const migrationMarkerKey = "go_migration"

// MigrationReport resume lo importado (para el log de arranque).
type MigrationReport struct {
	BackupPath         string
	Users              int
	Sessions           int // vivas (expires_at > ahora)
	KV                 int
	Routers            int
	DeviceAttrib       int
	Metrics            int
	AdguardStats       int
	LoginAttemptsReset int
}

// Log emite el resumen de migración (formato [netpulse] …).
func (r *MigrationReport) Log() {
	log.Printf("[netpulse] migración Node→Go: backup en %s", r.BackupPath)
	log.Printf("[netpulse] migración Node→Go: importados users=%d sessions_vivas=%d kv=%d routers=%d device_attrib=%d metrics=%d adguard_stats=%d · login_attempts reseteado (%d filas)",
		r.Users, r.Sessions, r.KV, r.Routers, r.DeviceAttrib, r.Metrics, r.AdguardStats, r.LoginAttemptsReset)
}

// MigrateNodeDB inspecciona dbPath y, si es una DB Node sin migrar, hace
// backup y la importa. Devuelve nil si no hay nada que migrar (instalación
// nueva o migración ya aplicada).
func MigrateNodeDB(dbPath string) (*MigrationReport, error) {
	st, err := os.Stat(dbPath)
	if err != nil || st.Size() == 0 {
		return nil, nil // instalación nueva
	}

	sqldb, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	defer sqldb.Close()
	sqldb.SetMaxOpenConns(1)

	// Detección del esquema Node: tablas firma presentes.
	if !hasTable(sqldb, "users") || !hasTable(sqldb, "sessions") || !hasTable(sqldb, "kv") {
		log.Printf("[netpulse] aviso: %s existe pero no parece una DB de NetPulse; se continúa sin migración", dbPath)
		return nil, nil
	}
	// ¿Ya migrada?
	var marker string
	if err := sqldb.QueryRow("SELECT value FROM kv WHERE key = ?", migrationMarkerKey).Scan(&marker); err == nil && marker != "" {
		return nil, nil
	}

	// 1) Consolida el WAL en el fichero principal y cierra antes de copiar.
	if _, err := sqldb.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return nil, fmt.Errorf("checkpoint pre-backup: %w", err)
	}

	// 2) Backup atómico: copia a temporal + rename, ANTES de modificar la DB.
	ts := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.bak-%s", dbPath, ts)
	if err := atomicCopy(dbPath, backupPath); err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}

	rep := &MigrationReport{BackupPath: backupPath}

	// 3) Conteos para el log (qué se importa y cuántas filas).
	rep.Users = countRows(sqldb, "users")
	rep.Sessions = countWhere(sqldb, "sessions", "expires_at > ?", NowMS())
	rep.KV = countRows(sqldb, "kv")
	rep.Routers = countRows(sqldb, "routers")
	rep.DeviceAttrib = countRows(sqldb, "device_attrib")
	rep.Metrics = countRows(sqldb, "metrics")
	rep.AdguardStats = countRows(sqldb, "adguard_stats")

	// 4) login_attempts es efímero: se resetea.
	res, err := sqldb.Exec("DELETE FROM login_attempts")
	if err == nil {
		n, _ := res.RowsAffected()
		rep.LoginAttemptsReset = int(n)
	}

	// 5) Marcador de migración aplicada (interno, no observable por la API).
	if _, err := sqldb.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		migrationMarkerKey, time.Now().Format(time.RFC3339),
	); err != nil {
		return nil, fmt.Errorf("marcador de migración: %w", err)
	}

	return rep, nil
}

// hasTable comprueba existencia en sqlite_master.
func hasTable(db *sql.DB, name string) bool {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

func countRows(db *sql.DB, table string) int {
	if !hasTable(db, table) {
		return 0
	}
	var n int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n); err != nil {
		return 0
	}
	return n
}

func countWhere(db *sql.DB, table, where string, args ...any) int {
	if !hasTable(db, table) {
		return 0
	}
	var n int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", table, where), args...).Scan(&n); err != nil {
		return 0
	}
	return n
}

// atomicCopy copia src→dst vía fichero temporal + rename (misma partición).
func atomicCopy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
