package snmp

const (
	OidSysDescr  = ".1.3.6.1.2.1.1.1.0"
	OidSysName   = ".1.3.6.1.2.1.1.5.0"
	OidSysUpTime = ".1.3.6.1.2.1.1.3.0"

	OidIfNumber     = ".1.3.6.1.2.1.2.1.0"
	OidIfTable      = ".1.3.6.1.2.1.2.2.1"
	OidIfIndex      = ".1.3.6.1.2.1.2.2.1.1"
	OidIfDescr      = ".1.3.6.1.2.1.2.2.1.2"
	OidIfType       = ".1.3.6.1.2.1.2.2.1.3"
	OidIfSpeed      = ".1.3.6.1.2.1.2.2.1.5"
	OidIfOperStatus = ".1.3.6.1.2.1.2.2.1.8"
	OidIfInOctets   = ".1.3.6.1.2.1.2.2.1.10"
	OidIfInErrors   = ".1.3.6.1.2.1.2.2.1.14"

	// OidDot1dBaseBridgeAddress (BRIDGE-MIB): MAC base del bridge del
	// switch. Fuente para la MAC propia de un switch sondeado por SNMP
	// (#1036): sin ella el equipo no casaba por MAC en topología.
	OidDot1dBaseBridgeAddress = ".1.3.6.1.2.1.17.1.1.0"
	OidIfOutOctets            = ".1.3.6.1.2.1.2.2.1.16"
	OidIfOutErrors            = ".1.3.6.1.2.1.2.2.1.20"

	OidIfXTable    = ".1.3.6.1.2.1.31.1.1.1"
	OidIfName      = ".1.3.6.1.2.1.31.1.1.1.1"
	OidIfHighSpeed = ".1.3.6.1.2.1.31.1.1.1.15"
	OidIfAlias     = ".1.3.6.1.2.1.31.1.1.1.18"

	OidDot1dTpFdbPort       = ".1.3.6.1.2.1.17.4.3.1.2"
	OidDot1dBasePortIfIndex = ".1.3.6.1.2.1.17.1.4.1.2"
	// OidDot1qTpFdbPort: tabla FDB de Q-BRIDGE MIB (RFC 4363). Algunos
	// switches gestionados solo la exponen (y no la dot1d) → fallback (#661).
	OidDot1qTpFdbPort = ".1.3.6.1.2.1.17.7.1.2.2.1.2"

	// LLDP-MIB (IEEE 802.1AB): tabla de vecinos remotos y sus direcciones de
	// gestión (#931). Los switches SNMP que la exponen anuncian qué equipo
	// cuelga de cada puerto; alimenta routerPolled.lldp con el mismo
	// contrato que la sonda lldpd de los routers (#300).
	OidLldpRemTable   = ".1.0.8802.1.1.2.1.4.1.1"
	OidLldpRemManAddr = ".1.0.8802.1.1.2.1.4.2.1"
	// OidLldpLocPortTable: tabla de puertos locales LLDP (#1279). Los Omada
	// (SG3428X-M2/SG3210X-M2) exponen la rem table MUTILADA (solo
	// chassisIdSubtype, sin chassis/port/sysName) pero la local COMPLETA:
	// lldpLocPortId = nombre del puerto y lldpLocPortDesc = la descripción
	// que el admin le puso al puerto (en redes bien llevadas, el nombre del
	// vecino). Es la única identidad del vecino que estos switches dan.
	OidLldpLocPortTable = ".1.0.8802.1.1.2.1.3.7.1"

	// OidMikrotikTemp: la fila que #1256 fijó a mano (SwOS, índice 52 y
	// scalar .0, Gauge32 en GRADOS ENTEROS: un CSS610-8G-2S+ v2.21 reporta
	// 60 = 60 °C; las décimas son convención de RouterOS, no asumirlas).
	// RouterOS publica la MISMA tabla con la sonda en otro índice y sin .0,
	// así que el lector pasó a caminar la tabla (#1725) y esto queda como
	// último intento para equipos cuya tabla no exponga los nombres.
	OidMikrotikTemp = ".1.3.6.1.4.1.14988.1.1.3.100.1.3.52.0"
	// OidMikrotikHealthName/OidMikrotikHealthValue: columnas de la health
	// table de MikroTik (14988.1.1.3.100.1). La fila 17 de un CRS305 real:
	//
	//	.100.1.2.17 = STRING: "cpu-temperature"
	//	.100.1.3.17 = INTEGER: 48
	//
	// Se caminan las dos y se emparejan por índice (#1725): el valor sin su
	// nombre no dice qué sonda es, y el nombre sin valor no da la lectura.
	OidMikrotikHealthName  = ".1.3.6.1.4.1.14988.1.1.3.100.1.2"
	OidMikrotikHealthValue = ".1.3.6.1.4.1.14988.1.1.3.100.1.3"

	// HOST-RESOURCES-MIB (#1725): el árbol genérico que muchos agentes —
	// RouterOS incluido— rellenan cuando no existe ninguna MIB privada. Son
	// el respaldo cuando el fabricante no contesta, nunca por delante de él.
	//
	//	hrProcessorLoad.1 = 1     (cpu0)      → %, Gauge32 0..100
	//	hrStorageDescr.65536       = "main memory"
	//	hrStorageSize.65536 = 524288  hrStorageUsed.65536 = 89840
	//
	// Size y Used vienen en las MISMAS hrStorageAllocationUnits (1024 B aquí),
	// así que la proporción es directa y no hace falta leer allocationUnits.
	OidHrSystemUptime  = ".1.3.6.1.2.1.25.1.1.0"
	OidHrProcessorLoad = ".1.3.6.1.2.1.25.3.3.1.2"
	OidHrStorageDescr  = ".1.3.6.1.2.1.25.2.3.1.3"
	OidHrStorageSize   = ".1.3.6.1.2.1.25.2.3.1.5"
	OidHrStorageUsed   = ".1.3.6.1.2.1.25.2.3.1.6"
)
