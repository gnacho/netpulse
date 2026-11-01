// hostResources.go: HOST-RESOURCES-MIB (#1725). CPU y RAM del árbol genérico,
// para los equipos que no tienen ninguna MIB privada de vitales (RouterOS por
// SNMP, agentes genéricos). Es RESPALDO: el caller solo lo usa cuando la
// lectura del fabricante no dijo nada, porque la tabla privada es lo que el
// propio firmware muestra en su panel.
package snmp

import (
	"math"
	"math/big"
	"strings"

	"github.com/gosnmp/gosnmp"
)

// HostResourcesPresent: un Get barato para saber si el agente publica
// HOST-RESOURCES antes de caminar sus tablas. Lo devuelven los equipos que sí
// lo tienen (RouterOS: TimeTicks) y NoSuchObject en los que no (FN SWITCH 8
// PRO: 1 ms), así que la pregunta cuesta un RTT; lo que cuesta caro es
// caminar un árbol que el equipo va a descartar en silencio, por eso se
// pregunta primero y el resultado se cachea arriba (#1725).
func HostResourcesPresent(s *gosnmp.GoSNMP) bool {
	if s == nil {
		return false
	}
	result, err := s.Get([]string{OidHrSystemUptime})
	if err != nil || result == nil || len(result.Variables) == 0 {
		return false
	}
	v := result.Variables[0]
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return false
	}
	return v.Value != nil
}

// HrStorageRow: una fila de hrStorageTable con lo que hace falta para saber
// cuánta memoria es y cuánta está usada. Size y Used vienen en las mismas
// hrStorageAllocationUnits de la fila, así que la proporción es directa y no
// hace falta leer allocationUnits para el porcentaje.
type HrStorageRow struct {
	Descr string
	Size  uint64
	Used  uint64
}

// ParseHrProcessorLoads: utilización de CPU = el MÁXIMO de hrProcessorLoad por
// núcleo. Un promedio escondería un núcleo saturado en un equipo de dos, y la
// pregunta que contesta el número en la tarjeta es "¿está ocupado este
// equipo?". Sin filas válidas → (0, false); un valor fuera de 0..100 % no es
// una utilización, se descarta (algunos agentes publican ahí otra magnitud).
func ParseHrProcessorLoads(loads []int) (cpu int, ok bool) {
	for _, v := range loads {
		if v < 0 || v > 100 {
			continue
		}
		if !ok || v > cpu {
			cpu, ok = v, true
		}
	}
	return cpu, ok
}

// ParseHrStorageRAM: porcentaje usado de la memoria principal. La fila se
// reconoce por su descripción ("main memory", case-insensitive y aceptando
// el sufijo de tamaño que algunos agentes añaden), NO por su índice:
// hrStorageDescr varía por fabricante y elegir la primera fila que venga es
// inventarse una lectura de RAM que puede ser un disco. Sin fila que case →
// (0, false) y la vital se queda en null.
func ParseHrStorageRAM(rows []HrStorageRow) (pct int, ok bool) {
	for _, r := range rows {
		if !isMainMemory(r.Descr) || r.Size == 0 || r.Used > r.Size {
			continue
		}
		pct = int((r.Used*100 + r.Size/2) / r.Size) // redondeo, sin float
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		return pct, true
	}
	return 0, false
}

// isMainMemory: la descripción de la fila de RAM. Se compara en minúsculas y
// sin el prefijo "system memory" que otros agentes usan para el mismo hueco.
func isMainMemory(descr string) bool {
	d := strings.ToLower(strings.TrimSpace(descr))
	return d == "main memory" || strings.HasPrefix(d, "main memory")
}

// PollHostResourcesVitals lee CPU (hrProcessorTable) y RAM (hrStorageTable,
// fila "main memory"). Best-effort como el resto del paquete: un walk que
// falla o que no devuelve filas deja su vital sin bandera, y el poll sigue.
func PollHostResourcesVitals(s *gosnmp.GoSNMP) (cpu int, okCpu bool, ramPct int, okRam bool) {
	if s == nil {
		return 0, false, 0, false
	}
	if pdus, err := walkSafe(s, OidHrProcessorLoad); err == nil {
		loads := make([]int, 0, len(pdus))
		for _, p := range pdus {
			if v, ok := pduInt(p); ok {
				loads = append(loads, v)
			}
		}
		cpu, okCpu = ParseHrProcessorLoads(loads)
	}
	if rows, ok := pollHrStorage(s); ok {
		ramPct, okRam = ParseHrStorageRAM(rows)
	}
	return cpu, okCpu, ramPct, okRam
}

// pduInt: entera de una PDU numérica. Rechaza NoSuchObject/Instance y
// cualquier otro tipo (un string no es una utilización). A diferencia de
// mikrotikTempFromPDU no aplica rango: lo decide el parser de cada vital, que
// es el que sabe qué magnitud está leyendo.
func pduInt(v gosnmp.SnmpPDU) (int, bool) {
	switch v.Type {
	case gosnmp.Integer, gosnmp.Gauge32, gosnmp.Counter32, gosnmp.Counter64, gosnmp.Uinteger32:
	default:
		return 0, false
	}
	// gosnmp deja el número como *big.Int en unos tipos y como entero nativo
	// en otros (ToBigInt no convierte un *big.Int, devuelve 0).
	bi, okBig := v.Value.(*big.Int)
	if !okBig {
		bi = gosnmp.ToBigInt(v.Value)
	}
	if !bi.IsInt64() {
		return 0, false
	}
	n := bi.Int64()
	if n < 0 || n > math.MaxInt32 {
		return 0, false
	}
	return int(n), true
}

// pollHrStorage aparecea descr/size/used por índice de hrStorageTable. Como
// mikrotikHealthRows con la tabla de salud: una fila a la que falte una
// columna no es utilizable.
//
// La tabla se pide primero por su descripción y se aborta si no tiene filas:
// un agente sin HOST-RESOURCES responde el walk vacío en un viaje, y seguir
// con size/used serían dos RTTs más por cada poll de un equipo que nunca va
// a dar RAM (#661 enseñó a mirar el coste del path SNMP).
func pollHrStorage(s *gosnmp.GoSNMP) ([]HrStorageRow, bool) {
	descr, errD := walkSafe(s, OidHrStorageDescr)
	if errD != nil || len(descr) == 0 {
		return nil, false
	}
	size, errS := walkSafe(s, OidHrStorageSize)
	if errS != nil {
		return nil, false
	}
	used, errU := walkSafe(s, OidHrStorageUsed)
	if errU != nil {
		return nil, false
	}
	type pair struct {
		descr string
		size  uint64
	}
	byIdx := map[int]*pair{}
	for _, p := range descr {
		if idx := trailingIndex(p.Name, OidHrStorageDescr); idx != 0 {
			byIdx[idx] = &pair{descr: stringVal(p)}
		}
	}
	for _, p := range size {
		if idx := trailingIndex(p.Name, OidHrStorageSize); idx != 0 {
			if row := byIdx[idx]; row != nil {
				row.size = uint64Val(p)
			}
		}
	}
	out := make([]HrStorageRow, 0, len(byIdx))
	for idx, row := range byIdx {
		if row.size == 0 {
			continue
		}
		usedVal := uint64(0)
		for _, p := range used {
			if trailingIndex(p.Name, OidHrStorageUsed) == idx {
				usedVal = uint64Val(p)
				break
			}
		}
		out = append(out, HrStorageRow{Descr: row.descr, Size: row.size, Used: usedVal})
	}
	return out, len(out) > 0
}
