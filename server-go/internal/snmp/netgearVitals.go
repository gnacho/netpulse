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
package snmp

import (
	"regexp"
	"strconv"

	g "github.com/gosnmp/gosnmp"
)

const (
	// OidNetgearCpuUtil: columna de utilización por proceso (string 5/60/300s).
	OidNetgearCpuUtil = ".1.3.6.1.4.1.4526.11.1.1.4.8.1.3"
	// OidNetgearTempSensor: columna de temperatura por sensor (°C).
	OidNetgearTempSensor = ".1.3.6.1.4.1.4526.11.43.1.8.1.5"
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
