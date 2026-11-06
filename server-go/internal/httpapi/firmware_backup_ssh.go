package httpapi

// #1899: copia de config en unidades OpenWrt que NO tienen agente (solo SSH).
// Hasta aquí el botón "Backup config" pedía al agente un sysupgrade -b por SSE
// y el agente subía el tar.gz; una unidad solo SSH no tenía camino. El server
// ya sabe hablar con esa unidad por SSH, así que el backup lo hace él y lo
// guarda en el mismo almacén que lista y descarga la UI.

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// sshConfigBackupCmd crea el tar.gz con sysupgrade, lo vuelca en base64 (el
// canal SSH del pool devuelve texto y un tar.gz crudo no sobreviviría) y borra
// el temporal SIEMPRE: en el router /tmp es tmpfs, o sea RAM (#1897).
const sshConfigBackupCmd = `tmp=/tmp/netpulse-config-backup-$$.tar.gz
sysupgrade --create-backup "$tmp" >/dev/null 2>&1 && base64 "$tmp"
rc=$?
rm -f "$tmp"
exit $rc`

// sshConfigBackupTimeout: sysupgrade empaqueta /etc y puede tardar en unidades
// lentas; el agente tiene un presupuesto parecido en su flujo.
const sshConfigBackupTimeout = 120 * time.Second

// configBackupSshCapable: mismo criterio que la página de firmware: OpenWrt
// vanilla. Un managed-switch o un external SNMP no tienen sysupgrade.
func configBackupSshCapable(rows []adapters.RouterConfig, id string) bool {
	for _, rc := range rows {
		if rc.ID == id {
			return rc.Type == "" || rc.Type == "openwrt"
		}
	}
	return false
}

// decodeSshBackup: base64 con saltos de línea de busybox -> bytes del tar.gz.
func decodeSshBackup(out string) ([]byte, error) {
	clean := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, out)
	if clean == "" {
		return nil, errors.New("la unidad no devolvió ningún backup")
	}
	data, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return nil, errors.New("la salida de la unidad no es un backup válido")
	}
	return data, nil
}

func (s *server) registerFirmwareBackupSshRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/firmware-backups/{routerId}", auth.RequireAdmin(http.HandlerFunc(s.handleFirmwareBackupSsh)))
}

func (s *server) handleFirmwareBackupSsh(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("routerId")
	if s.db == nil || s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "ssh_unavailable", "el servidor no tiene pool SSH")
		return
	}
	if !configBackupSshCapable(routerstore.ListRouters(s.db.DB), id) {
		writeError(w, http.StatusUnprocessableEntity, "not_openwrt",
			"esta unidad no es un OpenWrt gestionable por SSH: su copia se hace con su propio mecanismo")
		return
	}
	host := s.hostOfRouter(id)
	if host == "" {
		writeError(w, http.StatusNotFound, "not_found", "no hay router con ese id")
		return
	}
	out, err := s.pool.Run(host, sshConfigBackupCmd, sshConfigBackupTimeout)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": "ssh_backup_failed", "message": err.Error(),
		})
		return
	}
	data, err := decodeSshBackup(out)
	if err != nil {
		writeError(w, http.StatusBadGateway, "backup_invalid", err.Error())
		return
	}
	if len(data) > maxFirmwareUpload {
		writeError(w, http.StatusRequestEntityTooLarge, "backup_too_large", "backup mayor del tope")
		return
	}
	dir := s.firmwareBackupDir()
	if err := ensureBackupDir(dir); err != nil {
		writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
		return
	}
	name := s.firmwareBackupName(context.Background(), id)
	dest := filepath.Join(dir, name)
	if err := os.WriteFile(dest, data, backupFileMode); err != nil {
		writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
		return
	}
	pruneFirmwareBackups(dir, id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "size": len(data)})
}
