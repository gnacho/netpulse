package httpapi

// #1899: copia de config en unidades OpenWrt que NO tienen agente (solo SSH).
// Hasta aquí el botón "Backup config" pedía al agente un sysupgrade -b por SSE
// y el agente subía el tar.gz; una unidad solo SSH no tenía camino. El server
// ya sabe hablar con esa unidad por SSH, así que el backup lo hace él y lo
// guarda en el mismo almacén que lista y descarga la UI.

import (
	"context"
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

// sshBackupMarker separa la salida del script de los bytes del archivo: si
// algo se cuela por stdout antes, el servidor toma lo que viene detrás.
const sshBackupMarker = "__netpulse_backup__"

// sshConfigBackupCmd crea el tar.gz con sysupgrade, lo vuelca en crudo tras el
// marcador y borra el temporal SIEMPRE: en el router /tmp es tmpfs, o sea RAM
// (#1897). Nada de base64: la imagen de fábrica de OpenWrt no trae ese applet
// ni instala coreutils-base64, y con él el backup fallaba con "base64: not
// found" (#1935). El pool lee stdout con session.Output, que devuelve bytes sin
// tocar, así que el tar.gz sobrevive al canal.
const sshConfigBackupCmd = `tmp=/tmp/netpulse-config-backup-$$.tar.gz
sysupgrade --create-backup "$tmp" >/dev/null 2>&1
rc=$?
if [ $rc -eq 0 ] && [ -s "$tmp" ]; then
  echo "` + sshBackupMarker + `"
  cat "$tmp"
fi
rm -f "$tmp"
exit $rc`

// sshConfigBackupTimeout: sysupgrade empaqueta /etc y puede tardar en unidades
// lentas; el agente tiene un presupuesto parecido en su flujo.
const sshConfigBackupTimeout = 120 * time.Second

// routerConfigBackupSshCapable: mismo criterio que la página de firmware:
// OpenWrt vanilla. Un managed-switch o un external SNMP no tienen sysupgrade.
func routerConfigBackupSshCapable(rc adapters.RouterConfig) bool {
	return rc.Type == "" || rc.Type == "openwrt"
}

func configBackupSshCapable(rows []adapters.RouterConfig, id string) bool {
	for _, rc := range rows {
		if rc.ID == id {
			return routerConfigBackupSshCapable(rc)
		}
	}
	return false
}

// sshBackupError lleva el código HTTP de cada fallo del camino SSH, para que
// el handler manual (#1899) y el backup programado (#1924) compartan la misma
// implementación sin depender del texto del error.
type sshBackupError struct {
	status int
	code   string
	msg    string
}

func (e *sshBackupError) Error() string { return e.msg }

// backupConfigOverSsh crea la copia en la unidad y la guarda en el almacén que
// lista y descarga la UI. Es el mismo camino para el botón manual y para el
// tick semanal: una sola implementación, un solo almacén.
func (s *server) backupConfigOverSsh(ctx context.Context, id, host string) (string, int, error) {
	out, err := s.pool.Run(host, sshConfigBackupCmd, sshConfigBackupTimeout)
	if err != nil {
		return "", 0, &sshBackupError{http.StatusBadGateway, "ssh_backup_failed", err.Error()}
	}
	data, err := decodeSshBackup(out)
	if err != nil {
		return "", 0, &sshBackupError{http.StatusBadGateway, "backup_invalid", err.Error()}
	}
	if len(data) > maxFirmwareUpload {
		return "", 0, &sshBackupError{http.StatusRequestEntityTooLarge, "backup_too_large", "backup mayor del tope"}
	}
	dir := s.firmwareBackupDir()
	if err := ensureBackupDir(dir); err != nil {
		return "", 0, &sshBackupError{http.StatusInternalServerError, "firmware_error", err.Error()}
	}
	name := s.firmwareBackupName(ctx, id)
	if err := os.WriteFile(filepath.Join(dir, name), data, backupFileMode); err != nil {
		return "", 0, &sshBackupError{http.StatusInternalServerError, "firmware_error", err.Error()}
	}
	pruneFirmwareBackups(dir, id)
	return name, len(data), nil
}

// decodeSshBackup: bytes del tar.gz que la unidad volcó tras el marcador. No
// hay decodificador de por medio (#1935), así que el único control posible es
// que lo recibido sea un gzip: cabecera 1f 8b.
func decodeSshBackup(out string) ([]byte, error) {
	i := strings.Index(out, sshBackupMarker)
	if i < 0 {
		return nil, errors.New("la unidad no devolvió ningún backup")
	}
	rest := out[i+len(sshBackupMarker):]
	if j := strings.IndexByte(rest, '\n'); j >= 0 {
		rest = rest[j+1:]
	} else {
		rest = ""
	}
	if rest == "" {
		return nil, errors.New("la unidad no devolvió ningún backup")
	}
	if len(rest) < 2 || rest[0] != 0x1f || rest[1] != 0x8b {
		return nil, errors.New("la salida de la unidad no es un backup válido")
	}
	return []byte(rest), nil
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
	name, size, err := s.backupConfigOverSsh(r.Context(), id, host)
	if err != nil {
		var be *sshBackupError
		if errors.As(err, &be) {
			writeJSON(w, be.status, map[string]any{"error": be.code, "message": be.msg})
			return
		}
		writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "size": size})
}
