package httpapi

import (
	"net/http"
	"time"
)

// #2112: el servidor fija un WriteTimeout global de 15 s (cmd/netpulse/main.go)
// y solo los streams SSE lo extienden. Los handlers que corren un script por
// SSH tardan minutos, así que cuando escriben el JSON el deadline de la
// conexión ya pasó, la escritura falla y el navegador ve una conexión cerrada,
// que pinta como "no se alcanza el servidor". No es SSH ni la unidad: el
// handler nunca llega a responder.
//
// Los handlers que esperan a una unidad llaman a setLongWriteDeadline con su
// propio techo antes de hacer nada. El techo incluye el tope de cada comando
// más margen, y queda por encima del timeout del cliente para que, si algo se
// cuelga de verdad, el que corte sea el navegador y no el servidor.
const (
	// 300 s del script de instalación + 40 s de espera al primer push.
	reinstallWriteCeiling = 400 * time.Second
	// 60 s de script + margen.
	uninstallWriteCeiling = 150 * time.Second
	// 60 s de push del token + margen (alta y rearm).
	rearmWriteCeiling = 90 * time.Second
	// 300 s del instalador de integraciones sobre SSH + margen.
	integrationWriteCeiling = 360 * time.Second
	// 120 s de la copia de configuración + margen.
	configBackupWriteCeiling = 180 * time.Second
	// 10 s de `uci show` + 20 s de commit + 20 s de restart + margen.
	deviceActionWriteCeiling = 70 * time.Second
	// 10 min del build remoto y la descarga owut + margen.
	owutDownloadWriteCeiling = 11 * time.Minute
	// Reemit recorre la flota entera, un SSH por agente.
	reemitPerAgentCeiling = 150 * time.Second
	reemitMaxCeiling      = time.Hour
)

// setLongWriteDeadline sustituye el deadline de escritura de ESTA petición.
// Sin el ResponseController no hay forma de llegar a la conexión (y en tests
// con un writer que no lo soporta simplemente no hace nada).
func setLongWriteDeadline(w http.ResponseWriter, d time.Duration) {
	if d <= 0 {
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(d))
}

// reemitWriteCeiling dimensiona el techo del reemit por número de agentes: el
// handler recorre todos los slugs en serie.
func reemitWriteCeiling(agents int) time.Duration {
	d := time.Duration(agents)*reemitPerAgentCeiling + time.Minute
	if d > reemitMaxCeiling {
		return reemitMaxCeiling
	}
	return d
}
