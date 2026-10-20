package runtime

// Handler del comando config_backup (#1462): snapshot standalone de la
// config, independiente del flujo de firmware. El servidor envía el evento
// SSE cuando alguien pulsa "Backup config" en la tarjeta de la unidad; el
// agente genera el tar.gz con sysupgrade -b y lo sube con el mismo contrato
// que el backup del flujo de firmware (#1414), así la entrada aparece donde
// la UI ya lista backups (GET /api/firmware-backups/{routerId}).
// Best-effort: los fallos van a log, la UI descubre el resultado viendo
// aparecer (o no) la entrada nueva.

import (
	"fmt"
	"net/http"
	"path/filepath"
	"time"
)

func handleConfigBackup(opts Options, transport http.RoundTripper) {
	log := opts.logger()
	backupPath := filepath.Join(firmwareTmpDir, fmt.Sprintf("netpulse-config-backup-%d.tar.gz", time.Now().Unix()))
	if err := backupConfig(backupPath); err != nil {
		log.Error("[netpulse-agent] config_backup: snapshot falló", "err", err)
		return
	}
	if err := uploadBackup(opts, transport, backupPath); err != nil {
		log.Error("[netpulse-agent] config_backup: subida falló", "err", err)
		return
	}
	log.Info("[netpulse-agent] config_backup: backup subido", "path", backupPath)
}
