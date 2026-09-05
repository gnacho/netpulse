// clientbw.go — tráfico por cliente desde nlbwmon (issue #551). El comando y
// el parser viven aquí (patrón Cmd*/Parse* de probe) para que los use TANTO el
// agente local (sección clientbw del payload) como el sondeo SSH del server.
//
// Contrato de disponibilidad (#551):
//   - nlbwmon NO instalado → ClientBwData{Available:false} (hint "instala
//     nlbwmon"; el server usa entonces los contadores hostapd por estación).
//   - instalado pero la sonda falla esta ronda (daemon caído/socket) →
//     Available:true con Hosts nil (el server conserva el último dato bueno).
//   - instalado y sin hosts → Available:true con Hosts {} (vacío honesto).
//
// Los contadores son ACUMULADOS del periodo de nlbwmon (por defecto mes
// natural): el server calcula el delta entre muestras para obtener bytes del
// intervalo, como hace con NetDev.
package probe

import (
	"encoding/json"
	"strings"
)

// CmdNlbwmonCheck: ¿está nlbw instalado? (exit != 0 → no instalado).
const CmdNlbwmonCheck = "command -v nlbw"

// CmdNlbwmonJSON: contadores por MAC del daemon nlbwmon en JSON (igual que la
// sonda SSH del server). Agrupación por MAC; columnas: mac, conns, rx_bytes,
// rx_pkts, tx_bytes, tx_pkts (acumulados del periodo).
const CmdNlbwmonJSON = "nlbw -c json -g mac 2>/dev/null"

// ClientBwData: sección clientbw del payload del agente (#551).
type ClientBwData struct {
	// Available: false cuando nlbw no está instalado en el equipo.
	Available bool `json:"available"`
	// Hosts: contadores por MAC (mayúsculas). nil = sonda fallida esta ronda
	// (el server conserva el último dato bueno); {} = cero hosts honesto.
	// SIN omitempty a propósito: hay que distinguir nil de vacío.
	Hosts map[string]NlbwCounter `json:"hosts"`
}

// NlbwCounter: bytes acumulados de un host en el periodo de nlbwmon.
type NlbwCounter struct {
	RxBytes uint64 `json:"rxBytes"`
	TxBytes uint64 `json:"txBytes"`
}

// ParseNlbwJSON parsea `nlbw -c json -g mac`:
//
//	{"columns":["mac","conns","rx_bytes","rx_pkts","tx_bytes","tx_pkts"],
//	 "data":[[<mac>,"conns",<rx_bytes>,<rx_pkts>,<tx_bytes>,<tx_pkts>], ...]}
//
// Defensivo: el parser mapea por NOMBRE de columna (no por posición), de forma
// que un cambio de orden u omisión de columnas del CLI no rompa el parseo.
// MAC normalizada a mayúsculas (contrato de claves del resto del payload).
func ParseNlbwJSON(data []byte) (map[string]NlbwCounter, error) {
	var root struct {
		Columns []string          `json:"columns"`
		Data    []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Columns) == 0 {
		return map[string]NlbwCounter{}, nil
	}
	// Índice de las columnas que nos interesan.
	macIdx, rxIdx, txIdx := -1, -1, -1
	for i, c := range root.Columns {
		switch c {
		case "mac":
			macIdx = i
		case "rx_bytes":
			rxIdx = i
		case "tx_bytes":
			txIdx = i
		}
	}
	if macIdx < 0 {
		return nil, nil // sin columna mac: formato inesperado, no error
	}
	hosts := make(map[string]NlbwCounter, len(root.Data))
	for _, raw := range root.Data {
		// Best-effort por fila: una fila con forma inesperada no invalida
		// el resto (patrón lldp.go). null/42/objeto → se saltan.
		var row []json.RawMessage
		if err := json.Unmarshal(raw, &row); err != nil {
			continue
		}
		if macIdx >= len(row) {
			continue
		}
		var mac string
		if err := json.Unmarshal(row[macIdx], &mac); err != nil || mac == "" {
			continue
		}
		var h NlbwCounter
		if rxIdx >= 0 && rxIdx < len(row) {
			_ = json.Unmarshal(row[rxIdx], &h.RxBytes) // best-effort
		}
		if txIdx >= 0 && txIdx < len(row) {
			_ = json.Unmarshal(row[txIdx], &h.TxBytes) // best-effort
		}
		hosts[strings.ToUpper(mac)] = h
	}
	return hosts, nil
}
