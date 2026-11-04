// backup.go — backups automáticos de la BD (#741): config persistida en kv,
// run manual (POST /api/backup/run), descarga completa (#218) y, desde el fix
// del scheduler, un bucle periódico que ejecuta el backup cuando vence el
// intervalo (o a la hora fija configurada). El vencimiento se deriva SIEMPRE
// del last_run persistido, así los reinicios y el auto-updater no resetean
// el reloj (el bug original: la config existía pero nada la ejecutaba).
package httpapi

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

const (
	kvBackupEnabled       = "backup.enabled"
	kvBackupFrequencyH    = "backup.frequency_h"
	kvBackupRetentionDays = "backup.retention_days"
	kvBackupLastRun       = "backup.last_run"
	kvBackupTime          = "backup.time"

	// backupTickEvery: cadencia del bucle. Corta a propósito: un cambio de
	// ajustes aplica en menos de un minuto sin canales ni reinicios (mismo
	// patrón que el scheduler de speedtest #511).
	backupTickEvery = 1 * time.Minute
)

// BackupConfig es la configuración de backups automáticos (kv backup.*).
// Time es opcional ("HH:MM", hora local): con valor, el backup es diario a
// esa hora (ventana de mantenimiento); vacío, cada FrequencyH horas.
type BackupConfig struct {
	Enabled       bool   `json:"enabled"`
	FrequencyH    int    `json:"frequency_h"`
	RetentionDays int    `json:"retention_days"`
	LastRun       string `json:"last_run"`
	Time          string `json:"time"`
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

func getBackupConfig(d *db.DB) BackupConfig {
	return BackupConfig{
		Enabled:       kvGetBool(d.DB, kvBackupEnabled),
		FrequencyH:    kvGetInt(d.DB, kvBackupFrequencyH, 24),
		RetentionDays: kvGetInt(d.DB, kvBackupRetentionDays, 3),
		LastRun:       kvGet(d.DB, kvBackupLastRun),
		Time:          kvGet(d.DB, kvBackupTime),
	}
}

// BackupDue decide si toca backup. Con Time vacío: intervalo FrequencyH desde
// lastRun (lastRun zero = ya). Con Time "HH:MM": diario a esa hora local;
// toca si ya pasó la hora de hoy y el último backup es anterior a la de hoy.
func BackupDue(cfg BackupConfig, lastRun time.Time, now time.Time) bool {
	if !cfg.Enabled {
		return false
	}
	if t, err := time.Parse("15:04", cfg.Time); err == nil && cfg.Time != "" {
		today := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		return !now.Before(today) && lastRun.Before(today)
	}
	if lastRun.IsZero() {
		return true
	}
	return now.Sub(lastRun) >= time.Duration(cfg.FrequencyH)*time.Hour
}

// backupLastRun lee el último run persistido (RFC3339 UTC; zero si nunca).
func (s *server) backupLastRun() time.Time {
	v := kvGet(s.db.DB, kvBackupLastRun)
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// runBackup copia la BD, marca last_run y aplica la retención. Single-flight
// con mutex: el run manual y el programado nunca se pisan.
func (s *server) runBackup() (string, error) {
	s.backupMu.Lock()
	defer s.backupMu.Unlock()

	cfg := getBackupConfig(s.db)
	backupDir := filepath.Join(filepath.Dir(s.db.Path), "backups")
	if err := ensureBackupDir(backupDir); err != nil {
		return "", err
	}

	ts := time.Now().UTC().Format("20060102-150405")
	// #1864: un backup es más que la BD. Una instalación nueva restaurada solo
	// con el .db tiene datos y ajustes pero no puede entrar por SSH en los
	// routers (la clave vive en .ssh) ni sirve TLS sin volver a generarlo, así
	// que el archivo lleva el conjunto: netpulse.db, .ssh y tls. Va en tar.gz
	// para que sea un único fichero, con la purga tratándolo como uno solo.
	dst := filepath.Join(backupDir, "netpulse-"+ts+".tar.gz")
	if err := writeBackupArchive(s.db.DB, filepath.Dir(s.db.Path), s.db.Path, dst); err != nil {
		return "", err
	}

	upsert := "INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
	if _, err := s.db.Exec(upsert, kvBackupLastRun, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return dst, err
	}

	if cfg.RetentionDays > 0 {
		purgeOldBackups(backupDir, cfg.RetentionDays)
	}
	return dst, nil
}

// backupLoop es el bucle periódico de backups automáticos (#741).
func (s *server) backupLoop(ctx context.Context, tickEvery time.Duration) {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.backupTick()
		}
	}
}

func (s *server) backupTick() {
	cfg := getBackupConfig(s.db)
	if !BackupDue(cfg, s.backupLastRun(), time.Now()) {
		return
	}
	dst, err := s.runBackup()
	if err != nil {
		log.Printf("[netpulse] backup automático falló: %v", err)
		return
	}
	log.Printf("[netpulse] backup automático completado: %s", dst)
}

func validBackupTime(v string) bool {
	if v == "" {
		return true
	}
	_, err := time.Parse("15:04", v)
	return err == nil
}

func (s *server) registerBackupRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/settings/backup", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, getBackupConfig(s.db))
	})))

	mux.Handle("PUT /api/settings/backup", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled       *bool   `json:"enabled"`
			FrequencyH    *int    `json:"frequency_h"`
			RetentionDays *int    `json:"retention_days"`
			Time          *string `json:"time"`
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
		if body.Time != nil && !validBackupTime(*body.Time) {
			writeError(w, http.StatusBadRequest, "invalid_time")
			return
		}
		if body.Time != nil {
			if *body.Time == "" {
				if _, err := s.db.Exec("DELETE FROM kv WHERE key = ?", kvBackupTime); err != nil {
					writeError(w, http.StatusInternalServerError, "kv_error")
					return
				}
			} else {
				upsert := "INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
				if _, err := s.db.Exec(upsert, kvBackupTime, *body.Time); err != nil {
					writeError(w, http.StatusInternalServerError, "kv_error")
					return
				}
			}
		}

		writeJSON(w, http.StatusOK, getBackupConfig(s.db))
	})))

	mux.Handle("POST /api/backup/run", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dst, err := s.runBackup()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "backup_error")
			return
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
		_ = ensureBackupDir(backupDir)
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

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, backupFileMode)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	if err := dstFile.Chmod(backupFileMode); err != nil {
		return err
	}

	_, err = dstFile.ReadFrom(srcFile)
	return err
}

// writeBackupArchive escribe el backup completo (#1864) en un único tar.gz:
// la BD más .ssh y tls, que son lo que hace falta para que una restauración en
// otro host pueda entrar por SSH en los routers y servir TLS sin regenerar
// nada. Un directorio que no exista (instalación sin clave todavía) se omite
// sin fallar: el backup sigue siendo válido.
func writeBackupArchive(d *sql.DB, dataDir, dbPath, dst string) error {
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, backupFileMode)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Chmod(backupFileMode); err != nil {
		return fail(err)
	}

	// #1866: la copia se hace con VACUUM INTO, no copiando el fichero vivo.
	// Leer el fichero mientras SQLite escribe daba una cabecera con un tamaño
	// que ya no cuadraba (archive/tar: write too long) y, aunque cuadrara, el
	// .db solo puede venir sin transacciones que aún viven en el WAL. El
	// snapshot tiene tamaño fijo y es una base válida por sí sola.
	snapshot := tmp + ".db"
	if err := os.Remove(snapshot); err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	if _, err := d.Exec("VACUUM INTO ?", snapshot); err != nil {
		return fail(err)
	}
	defer os.Remove(snapshot)

	zw := gzip.NewWriter(out)
	tw := tar.NewWriter(zw)

	if err := addTarFile(tw, snapshot, filepath.Base(dbPath)); err != nil {
		return fail(err)
	}
	for _, name := range []string{".ssh", "tls"} {
		p := filepath.Join(dataDir, name)
		if _, statErr := os.Stat(p); statErr != nil {
			continue
		}
		if err := addTarTree(tw, p, name); err != nil {
			return fail(err)
		}
	}

	if err := tw.Close(); err != nil {
		return fail(err)
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// addTarFile mete un fichero suelto en el archivo con su modo (la clave SSH va
// en 0600 y tiene que seguir así al restaurar).
func addTarFile(tw *tar.Writer, src, name string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	// #1866: el tamaño sale del descriptor abierto y la copia se corta en él.
	// Con os.Stat antes de abrir, un fichero que crece entre las dos llamadas
	// desborda la cabecera y archive/tar aborta con "write too long".
	info, err := f.Stat()
	if err != nil {
		return err
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, hdr.Size)
	return err
}

// addTarTree mete un directorio entero (permisos incluidos) con rutas
// relativas a root.
func addTarTree(tw *tar.Writer, src, root string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, p)
		if relErr != nil {
			return relErr
		}
		name := root
		if rel != "." {
			name = filepath.ToSlash(filepath.Join(root, rel))
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		hdr, hdrErr := tar.FileInfoHeader(info, "")
		if hdrErr != nil {
			return hdrErr
		}
		hdr.Name = name
		if d.IsDir() {
			hdr.Name = name + "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, openErr := os.Open(p)
		if openErr != nil {
			return openErr
		}
		defer f.Close()
		_, copyErr := io.Copy(tw, f)
		return copyErr
	})
}

// isBackupFile decide qué ficheros de backups/ son backups. Reconoce el
// archivo completo de hoy (.tar.gz) y los formatos anteriores (#818, #741)
// para que la retención siga limpiando lo que dejaron versiones viejas.
func isBackupFile(name string) bool {
	return strings.HasSuffix(name, ".tar.gz") ||
		strings.HasSuffix(name, ".db.gz") ||
		strings.HasSuffix(name, ".db")
}

func purgeOldBackups(dir string, retentionDays int) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// #818: reconoce backups comprimidos (.db.gz) y los antiguos sin
		// comprimir (.db); #1864 añade el archivo completo (.tar.gz).
		// Cualquier otro fichero se ignora.
		if !isBackupFile(e.Name()) {
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
