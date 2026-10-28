// firmware_upload.go — selector de método por unidad e imagen subida desde
// local (#1414). La imagen se guarda en el server y el flujo clásico la sirve
// al agente para su descarga (el agente ya descarga por URL con sha256, sin
// cambios de protocolo).
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
)

// maxFirmwareUpload: tope de una imagen subida (las de OpenWrt rondan 8-25 MB;
// 100 MB da margen para perfiles con muchos paquetes empaquetados).
const maxFirmwareUpload = 100 << 20

func validFirmwareID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return false
	}
	return filepath.Base(id) == id
}

func firmwareUploadPath(dir, id string) (string, bool) {
	if !validFirmwareID(id) {
		return "", false
	}
	dest := filepath.Join(dir, id+".bin")
	rel, err := filepath.Rel(dir, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return dest, true
}

func (s *server) registerFirmwareUploadRoutes(mux *http.ServeMux) {
	if s.firmware == nil {
		return
	}

	// PUT method: método preferido de la tarjeta (auto/owut/url/upload).
	mux.Handle("PUT /api/firmware-upgrades/{routerId}/method", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		var body struct {
			Method string `json:"method"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			return
		}
		if !firmware.ValidMethod(body.Method) {
			writeError(w, http.StatusBadRequest, "invalid_method", "method debe ser auto, owut, url o upload")
			return
		}
		if err := firmware.SaveMethod(s.db.DB, id, body.Method); err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))

	// POST image: sube una imagen (multipart, campo "image") y la deja como
	// target del flujo clásico: URL servida por este mismo server (alcanzable
	// por el agente vía NETPULSE_AGENT_URL/PublicURL) + checksum calculado.
	mux.Handle("POST /api/firmware-upgrades/{routerId}/image", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		if !validFirmwareID(id) {
			writeError(w, http.StatusBadRequest, "invalid_router_id")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFirmwareUpload+(1<<20))
		if err := r.ParseMultipartForm(maxFirmwareUpload); err != nil {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "image_too_large", fmt.Sprintf("imagen mayor de %d MB", maxFirmwareUpload>>20))
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_body")
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, hdr, err := r.FormFile("image")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "campo multipart 'image' requerido")
			return
		}
		defer f.Close()

		dir := filepath.Join(filepath.Dir(s.db.Path), "firmware-uploads")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
			return
		}
		dest, valid := firmwareUploadPath(dir, id)
		if !valid {
			writeError(w, http.StatusBadRequest, "invalid_router_id")
			return
		}
		tmp := dest + ".tmp"
		df, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
			return
		}
		h := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(df, h), io.LimitReader(f, maxFirmwareUpload+1))
		closeErr := df.Close()
		if copyErr != nil || closeErr != nil {
			os.Remove(tmp)
			writeError(w, http.StatusInternalServerError, "firmware_error", "no se pudo guardar la imagen")
			return
		}
		if n == 0 || n > maxFirmwareUpload {
			os.Remove(tmp)
			writeError(w, http.StatusRequestEntityTooLarge, "image_too_large", "imagen vacía o mayor del tope")
			return
		}
		if err := os.Rename(tmp, dest); err != nil {
			os.Remove(tmp)
			writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
			return
		}
		sum := hex.EncodeToString(h.Sum(nil))

		// La URL con la que los AGENTES deben alcanzar al server (#1335):
		// la misma cadena de precedencia que el install line.
		imgURL := s.agentBaseURL(r) + "/api/firmware-image/" + id
		t, _ := s.firmware.GetTarget(id)
		if t == nil {
			t = &firmware.Target{RouterID: id}
		}
		t.TargetURL = imgURL
		t.Checksum = sum
		if err := s.firmware.SetTarget(*t); err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "checksum": sum, "size": n, "name": hdr.Filename, "url": imgURL,
		})
	})))

	// GET image: sirve la imagen subida. Sin auth de admin a propósito: el
	// AGENTE la descarga con su transporte pinned (mismo caso que
	// GET /api/agents/{slug}/binary, #1367) y no puede añadir cabeceras.
	// La imagen tampoco es secreta (es la que el admin subió para flashear).
	mux.HandleFunc("GET /api/firmware-image/{routerId}", s.handleFirmwareImage)
}

// firmwareBackupDir resuelve el directorio de backups de config subidos por
// los agentes (#1414), junto a la BD (misma política que los backups #741).
func (s *server) firmwareBackupDir() string {
	return filepath.Join(filepath.Dir(s.db.Path), "backups", "firmware")
}

// maxFirmwareBackupsPerRouter: retención de backups de config por unidad.
const maxFirmwareBackupsPerRouter = 5

// registerFirmwareBackupRoutes: el agente sube el backup de config hecho con
// sysupgrade -b (persistencia real frente al /tmp local, que muere en el
// reboot del flash) y el admin lista/descarga.
func (s *server) registerFirmwareBackupRoutes(mux *http.ServeMux) {
	if s.firmware == nil {
		return
	}

	// POST: subida del agente (Bearer, misma auth que firmware-progress).
	// Body crudo: el .tar.gz tal cual.
	mux.HandleFunc("POST /api/agents/{slug}/firmware-backup", func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		token := bearerToken(r)
		if !s.checkAgentToken(slug, token) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if s.db == nil {
			writeError(w, http.StatusServiceUnavailable, "no_db")
			return
		}
		dir := s.firmwareBackupDir()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
			return
		}
		name := fmt.Sprintf("%s-%d.tar.gz", slug, time.Now().Unix())
		dest := filepath.Join(dir, name)
		df, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
			return
		}
		n, copyErr := io.Copy(df, io.LimitReader(r.Body, maxFirmwareUpload+1))
		closeErr := df.Close()
		if copyErr != nil || closeErr != nil {
			os.Remove(dest)
			writeError(w, http.StatusInternalServerError, "firmware_error", "no se pudo guardar el backup")
			return
		}
		if n == 0 || n > maxFirmwareUpload {
			os.Remove(dest)
			writeError(w, http.StatusRequestEntityTooLarge, "backup_too_large", "backup vacío o mayor del tope")
			return
		}
		pruneFirmwareBackups(dir, slug)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "size": n})
	})

	// GET list: backups de config de una unidad (admin).
	mux.Handle("GET /api/firmware-backups/{routerId}", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		if s.db == nil {
			writeError(w, http.StatusServiceUnavailable, "no_db")
			return
		}
		entries, err := os.ReadDir(s.firmwareBackupDir())
		if err != nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		type info struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
			Ts   int64  `json:"ts"`
		}
		out := []info{}
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), id+"-") || !strings.HasSuffix(e.Name(), ".tar.gz") {
				continue
			}
			if fi, err := e.Info(); err == nil {
				// ts = parte numérica del nombre (<slug>-<ts>.tar.gz)
				ts := int64(0)
				if n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(e.Name(), id+"-"), ".tar.gz"), 10, 64); err == nil {
					ts = n
				}
				out = append(out, info{Name: e.Name(), Size: fi.Size(), Ts: ts})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Ts > out[j].Ts })
		writeJSON(w, http.StatusOK, out)
	})))

	// GET file: descarga de un backup (admin).
	mux.Handle("GET /api/firmware-backups/{routerId}/file", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		name := filepath.Base(r.URL.Query().Get("name"))
		if name == "." || name == "" || !strings.HasPrefix(name, id+"-") || !strings.HasSuffix(name, ".tar.gz") {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		if s.db == nil {
			writeError(w, http.StatusServiceUnavailable, "no_db")
			return
		}
		path := filepath.Join(s.firmwareBackupDir(), name)
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		http.ServeFile(w, r, path)
	})))
}

// pruneFirmwareBackups deja los maxFirmwareBackupsPerRouter más recientes de
// una unidad (el nombre lleva ts, ordenar por nombre = ordenar por ts).
func pruneFirmwareBackups(dir, slug string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var mine []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), slug+"-") && strings.HasSuffix(e.Name(), ".tar.gz") {
			mine = append(mine, e)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].Name() > mine[j].Name() })
	for i := maxFirmwareBackupsPerRouter; i < len(mine); i++ {
		os.Remove(filepath.Join(dir, mine[i].Name()))
	}
}

// handleFirmwareImage sirve la imagen subida de un router. El id se sanea:
// solo nombre base (sin separadores ni puntos dobles) dentro del directorio
// de uploads.
func (s *server) handleFirmwareImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("routerId")
	if !validFirmwareID(id) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "no_db")
		return
	}
	dir := filepath.Join(filepath.Dir(s.db.Path), "firmware-uploads")
	path, valid := firmwareUploadPath(dir, id)
	if !valid {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", st.Size()))
	http.ServeFile(w, r, path)
}
