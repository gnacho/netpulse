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

	// OidMikrotikTemp: temperatura del chasis en switches MikroTik SwOS
	// (OID empresarial 14988, health table). Gauge32 en GRADOS ENTEROS
	// (#1256: un CSS610-8G-2S+ SwOS v2.21 reporta 60 = 60 °C; la convención
	// de décimas es de RouterOS, no asumirla en SwOS). Best-effort: un
	// switch que no lo expone responde NoSuchInstance y la vital queda a
	// null como antes (#441).
	OidMikrotikTemp = ".1.3.6.1.4.1.14988.1.1.3.100.1.3.52.0"
)
