// netgearVitals.go — vitales (CPU y temperatura) de switches Netgear
// FastPath (XS708T, GS728TXP/S3300 y familia) vía su MIB privada
// (NETGEAR enterprise 4526, ng700smartswitch = 4526.11):
//
//   - agentSwitchCpuProcessPercentageUtilization
//     1.3.6.1.4.1.4526.11.1.1.4.8.1.3.<idx>: STRING por proceso con tres
//     ventanas, "    5 Sec (  0.19%)   60 Sec (  0.16%)  300 Sec (  0.15%)".
//     El total del equipo es la suma por proceso (no hay tarea idle en la
//     tabla ni escalar de total).
//   - boxServicesTempSensorTemperature
//     1.3.6.1.4.1.4526.11.43.1.8.1.5.<idx>: Integer32 en °C por sensor; con
//     varios sensores se reporta el máximo (el punto caliente).
//
// No hay escalar de memoria TOTAL en la MIB (solo MemFree/MemAvailable), así
// que el RAM queda a null antes que inventar un porcentaje.
//
// El mismo subtree publica la identidad que ENTITY-MIB no da (#1802):
//
//   - 1.3.6.1.4.1.4526.11.1.1.1.3.0   modelo corto  "XS708T"
//   - 1.3.6.1.4.1.4526.11.1.1.1.7.0   chip          "BCM53412"
//   - 1.3.6.1.4.1.4526.11.1.1.1.13.0  firmware      "7.0.0.24"
//
// El chip no existe en ninguna otra fuente SNMP: las unidades OpenWrt lo
// leen de /proc/cpuinfo por agente, y un switch gestionado sin agente no
// tiene dónde decirlo.
package snmp

import (
	"regexp"
	"strconv"
	"strings"

	g "github.com/gosnmp/gosnmp"
)

const (
	// OidNetgearCpuUtil: columna de utilización por proceso (string 5/60/300s).
	OidNetgearCpuUtil = ".1.3.6.1.4.1.4526.11.1.1.4.8.1.3"
	// OidNetgearTempSensor: columna de temperatura por sensor (°C).
	OidNetgearTempSensor = ".1.3.6.1.4.1.4526.11.43.1.8.1.5"

	// EnterpriseNetgear: número enterprise de NETGEAR en sysObjectID.
	EnterpriseNetgear = 4526

	// OidNetgearModelName: modelo corto declarado por el firmware.
	OidNetgearModelName = ".1.3.6.1.4.1.4526.11.1.1.1.3.0"
	// OidNetgearSoC: chip del switch (BCM53412 en XS708T, BCM53346 en S3300).
	OidNetgearSoC = ".1.3.6.1.4.1.4526.11.1.1.1.7.0"
	// OidNetgearFirmware: versión de firmware, sin el resto del sysDescr.
	OidNetgearFirmware = ".1.3.6.1.4.1.4526.11.1.1.1.13.0"
)

// cpuUtilRe casa la primera ventana ("5 Sec (  0.19%)") del string por
// proceso; es la misma que pinta el panel web del switch como instantánea.
var cpuUtilRe = regexp.MustCompile(`5\s+Sec\s*\(\s*([0-9]+(?:\.[0-9]+)?)\s*%\)`)

// ParseNetgearCpuUtil suma la utilización de 5s de cada proceso: sin tarea
// idle en la tabla, la suma es el total del equipo. Strings inparseables se
// ignoran (firmwares que cambian el formato no deben romper el sondeo).
func ParseNetgearCpuUtil(vals []string) (cpu int, ok bool) {
	total := 0.0
	seen := 0
	for _, v := range vals {
		m := cpuUtilRe.FindStringSubmatch(v)
		if m == nil {
			continue
		}
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		total += f
		seen++
	}
	if seen == 0 {
		return 0, false
	}
	pct := int(total + 0.5)
	if pct > 100 {
		pct = 100
	}
	return pct, true
}

// ParseNetgearTemps devuelve el máximo de los sensores (punto caliente):
// con dos sensores (CPU y chasis) el que manda para alertas es el mayor.
func ParseNetgearTemps(vals []int) (temp int, ok bool) {
	max := 0
	for _, v := range vals {
		if v > max {
			max = v
		}
		ok = true
	}
	return max, ok
}

// PollNetgearVitals lee CPU y temperatura del agente FastPath. Un switch sin
// estos árboles (otra familia) devuelve ok=false en ambos y el caller
// conserva sus vitales a null, como hoy.
func PollNetgearVitals(s *g.GoSNMP) (cpu int, okCpu bool, temp int, okTemp bool) {
	if s == nil {
		return 0, false, 0, false
	}
	if pdus, err := walkSafe(s, OidNetgearCpuUtil); err == nil {
		vals := make([]string, 0, len(pdus))
		for _, p := range pdus {
			vals = append(vals, stringVal(p))
		}
		cpu, okCpu = ParseNetgearCpuUtil(vals)
	}
	if pdus, err := walkSafe(s, OidNetgearTempSensor); err == nil {
		vals := make([]int, 0, len(pdus))
		for _, p := range pdus {
			// mikrotikTempFromPDU es el conversor genérico PDU numérica→int
			// del paquete (tipos + NoSuchObject); el nombre viene de su
			// primer usuario, no de su alcance.
			if v, ok := mikrotikTempFromPDU(p); ok {
				vals = append(vals, v)
			}
		}
		temp, okTemp = ParseNetgearTemps(vals)
	}
	return cpu, okCpu, temp, okTemp
}

// IsNetgear reports whether sysObjectID belongs to the NETGEAR enterprise
// subtree, by the same rule as IsTpLink: the MIB-2 object ID is the stable
// vendor signal, sysDescr is not.
func IsNetgear(sysObjectID string) bool {
	id := strings.TrimSpace(sysObjectID)
	if id != "" && !strings.HasPrefix(id, ".") {
		id = "." + id
	}
	return strings.HasPrefix(id, ".1.3.6.1.4.1.4526.")
}

// NetgearIdentity carries the vendor-declared identity of a FastPath switch.
// Empty fields mean the agent did not answer that scalar.
type NetgearIdentity struct {
	ModelShort string
	SoC        string
	Firmware   string
}

// PollNetgearIdentity reads the three identity scalars in one Get. A family
// without them (or a unit that answers NoSuchObject) leaves the fields empty
// and the caller keeps whatever the generic readers found.
func PollNetgearIdentity(s *g.GoSNMP) NetgearIdentity {
	if s == nil {
		return NetgearIdentity{}
	}
	var id NetgearIdentity
	result, err := s.Get([]string{OidNetgearModelName, OidNetgearSoC, OidNetgearFirmware})
	if err != nil {
		return id
	}
	for _, v := range result.Variables {
		switch v.Name {
		case OidNetgearModelName:
			id.ModelShort = strings.TrimSpace(stringVal(v))
		case OidNetgearSoC:
			id.SoC = strings.TrimSpace(stringVal(v))
		case OidNetgearFirmware:
			id.Firmware = strings.TrimSpace(stringVal(v))
		}
	}
	return id
}
