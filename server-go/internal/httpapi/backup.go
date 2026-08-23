package httpapi

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

const (
	kvBackupEnabled       = "backup.enabled"
	kvBackupFrequencyH    = "backup.frequency_h"
	kvBackupRetentionDays = "backup.retention_days"
	kvBackupLastRun       = "backup.last_run"
)

type backupConfig struct {
	Enabled       bool   `json:"enabled"`
	FrequencyH    int    `json:"frequency_h"`
	RetentionDays int    `json:"retention_days"`
	LastRun       string `json:"last_run"`
}

func kvGetInt(db *sql.DB, key string, defaultVal int) int {
	v := kvGet(db, key)
	if v == "" {
		return defaultVal
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return defaultVal
	}
	return n
}

func getBackupConfig(d *db.DB) backupConfig {
	return backupConfig{
		Enabled:       kvGetBool(d.DB, kvBackupEnabled),
		FrequencyH:    kvGetInt(d.DB, kvBackupFrequencyH, 24),
		RetentionDays: kvGetInt(d.DB, kvBackupRetentionDays, 3),
		LastRun:       kvGet(d.DB, kvBackupLastRun),
	}
}

func (s *server) registerBackupRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/settings/backup", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, getBackupConfig(s.db))
	})))

	mux.Handle("PUT /api/settings/backup", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled       *bool `json:"enabled"`
			FrequencyH    *int  `json:"frequency_h"`
			RetentionDays *int  `json:"retention_days"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "")
			return
		}

		if body.Enabled != nil {
			if err := kvSetBool(s.db.DB, kvBackupEnabled, *body.Enabled); err != nil {
				writeError(w, http.StatusInternalServerError, "kv_error")
				return
			}
		}
		if body.FrequencyH != nil {
			if *body.FrequencyH < 1 || *body.FrequencyH > 72 {
				writeError(w, http.StatusBadRequest, "invalid_frequency")
				return
			}
			upsert := "INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
			if _, err := s.db.Exec(upsert, kvBackupFrequencyH, fmt.Sprintf("%d", *body.FrequencyH)); err != nil {
				writeError(w, http.StatusInternalServerError, "kv_error")
				return
			}
		}
		if body.RetentionDays != nil {
			if *body.RetentionDays < 1 || *body.RetentionDays > 30 {
				writeError(w, http.StatusBadRequest, "invalid_retention")
				return
			}
			upsert := "INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
			if _, err := s.db.Exec(upsert, kvBackupRetentionDays, fmt.Sprintf("%d", *body.RetentionDays)); err != nil {
				writeError(w, http.StatusInternalServerError, "kv_error")
				return
			}
		}

		writeJSON(w, http.StatusOK, getBackupConfig(s.db))
	})))

	mux.Handle("POST /api/backup/run", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := getBackupConfig(s.db)
		backupDir := filepath.Join(filepath.Dir(s.db.Path), "backups")
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			writeError(w, http.StatusInternalServerError, "backup_dir_error")
			return
		}

		ts := time.Now().UTC().Format("20060102-150405")
		dst := filepath.Join(backupDir, "netpulse-"+ts+".db")
		if err := copyFile(s.db.Path, dst); err != nil {
			writeError(w, http.StatusInternalServerError, "backup_error")
			return
		}

		upsert := "INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
		s.db.Exec(upsert, kvBackupLastRun, time.Now().UTC().Format(time.RFC3339))

		if cfg.RetentionDays > 0 {
			purgeOldBackups(backupDir, cfg.RetentionDays)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":   true,
			"file": dst,
			"size": fileSizeHuman(dst),
		})
	})))

	// GET /api/backup/download (admin) — descarga la BD COMPLETA, incluidos
	// los secrets del kv en claro (session_secret, adguard_pass, tokens de
	// agente/pairing) y webhook_events. Es un backup fiel para restaurar en
	// otro host: NO se redacta nada (una redacción rompería la restauración).
	// El riesgo queda acotado a (issue #218): RequireAdmin; el admin ya puede
	// leer la DB en disco (el mismo acceso); aviso explícito en el header
	// X-Netpulse-Backup-Contains-Credentials; y una línea de auditoría en el
	// log on-box con el admin que descargó.
	mux.Handle("GET /api/backup/download", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupDir := filepath.Join(filepath.Dir(s.db.Path), "backups")
		os.MkdirAll(backupDir, 0755)
		tmpFile := filepath.Join(backupDir, "export-temp.db")
		if err := copyFile(s.db.Path, tmpFile); err != nil {
			writeError(w, http.StatusInternalServerError, "backup_error")
			return
		}
		defer os.Remove(tmpFile)

		if u := auth.UserFromContext(r.Context()); u != nil {
			log.Printf("[netpulse] backup: descarga completa de la BD (incluye secrets kv) por admin %s", u.Username)
		}
		w.Header().Set("X-Netpulse-Backup-Contains-Credentials", "true")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="netpulse-%s.db"`, time.Now().UTC().Format("20060102-150405")))
		http.ServeFile(w, r, tmpFile)
	})))
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = dstFile.ReadFrom(srcFile)
	return err
}

func purgeOldBackups(dir string, retentionDays int) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".db" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func fileSizeHuman(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	size := info.Size()
	switch {
	case size >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(size)/(1<<30))
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(size)/(1<<10))
	default:
		return fmt.Sprintf("%d B", size)
	}
}
