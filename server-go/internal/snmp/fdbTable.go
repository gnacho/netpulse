package snmp

import (
	"fmt"
	"strings"

	"github.com/gosnmp/gosnmp"
)

type FdbEntry struct {
	MAC            string
	BridgePortIndex int
	IfIndex        int
}

// FdbPoll es el resultado de sondear el FDB de un switch. Además de las
// entradas y la fuente usada, lleva los conteos de PDUs crudos de cada walk
// para que el log pueda distinguir "walk vacío" de "walk con datos no
// utilizables" en diagnosis remotas (#948).
type FdbPoll struct {
	Entries  []FdbEntry
	Source   string // "dot1d" o "dot1q"
	RawDot1d int    // PDUs crudos devueltos por el walk dot1d
	RawDot1q int    // PDUs crudos devueltos por el walk dot1q (0 si no se intentó)
}

// PollFdbTable sondea el FDB del switch. #928: antes el error del fallback
// dot1q se tragaba y un fallo de ambos walks devolvía (vacío, nil): el caller
// no podía distinguir "switch sin clientes" de "walk fallido". #948: el
// fallback a dot1q no depende ya de que el walk dot1d devuelva cero PDUs,
// sino de que no produzca ninguna entrada utilizable; un walk dot1d con datos
// que no parsean ya no impide probar la tabla Q-BRIDGE.
func PollFdbTable(s *gosnmp.GoSNMP) (FdbPoll, error) {
	portToIfIndex := map[int]int{}
	pdus, err := walkSafe(s, OidDot1dBasePortIfIndex)
	if err == nil {
		for _, pdu := range pdus {
			idx := trailingIndex(pdu.Name, OidDot1dBasePortIfIndex)
			if idx <= 0 {
				continue
			}
			portToIfIndex[idx] = int(uint64Val(pdu))
		}
	}

	// Fuente del FDB: dot1d (RFC 1493) es la estándar, pero algunos switches
	// gestionados solo exponen la tabla Q-BRIDGE (dot1q, RFC 4363) → fallback
	// cuando la dot1d no produce entradas utilizables (#661, #948).
	d1Pdus, errD1 := walkSafe(s, OidDot1dTpFdbPort)
	res := FdbPoll{Source: "dot1d", RawDot1d: len(d1Pdus)}
	res.Entries = fdbEntries(portToIfIndex, d1Pdus, false)
	if len(res.Entries) > 0 {
		return res, nil
	}

	qPdus, errQ := walkSafe(s, OidDot1qTpFdbPort)
	res.RawDot1q = len(qPdus)
	res.Entries = fdbEntries(portToIfIndex, qPdus, true)
	if len(res.Entries) > 0 {
		res.Source = "dot1q"
		return res, nil
	}

	// Sin entradas de ninguna tabla: distinguir "FDB genuinamente vacío" de
	// "walk fallido" devolviendo el error real para que el caller lo registre
	// (#928). walkSafe devuelve (nil, err) en fallo, así que un error implica
	// que ese walk no produjo nada.
	res.Source = "dot1q"
	if errD1 != nil {
		res.Source = "dot1d"
	}
	switch {
	case errD1 != nil && errQ != nil:
		return res, fmt.Errorf("snmp fdb walk: dot1d: %v; dot1q: %w", errD1, errQ)
	case errD1 != nil:
		return res, fmt.Errorf("snmp fdb walk: %w", errD1)
	case errQ != nil:
		return res, fmt.Errorf("snmp fdb walk: dot1q: %w", errQ)
	}
	// Ambos walks respondieron pero no hay entradas utilizables: FDB vacío de
	// verdad (o datos que no parsean; los conteos Raw lo delatan en el log).
	res.Source = "dot1d"
	return res, nil
}

// fdbEntries convierte los PDUs crudos de un walk FDB en entradas. Es pura
// para probarla sin agente SNMP. dot1q indica que el índice es compuesto
// (<vlan>.M.M.M.M.M.M) y la MAC son los últimos 6 octetos.
func fdbEntries(portToIfIndex map[int]int, pdus []gosnmp.SnmpPDU, dot1q bool) []FdbEntry {
	var out []FdbEntry
	for _, pdu := range pdus {
		var mac string
		if dot1q {
			mac = extractMacFromOid(pdu.Name, OidDot1qTpFdbPort)
		} else {
			mac = extractMacFromOid(pdu.Name, OidDot1dTpFdbPort)
		}
		if mac == "" {
			continue
		}
		bridgePort := int(uint64Val(pdu))
		ifIdx := portToIfIndex[bridgePort]
		// #661: si el mapeo bridge→ifIndex no se resolvió, cae al propio número
		// de puerto de bridge (muchos switches lo usan como ifIndex), para no
		// descartar la entrada y dejar el switch con 0 dispositivos.
		if ifIdx == 0 {
			ifIdx = bridgePort
		}
		out = append(out, FdbEntry{
			MAC:             mac,
			BridgePortIndex: bridgePort,
			IfIndex:         ifIdx,
		})
	}
	return out
}

// extractMacFromOid extrae la MAC del índice de un OID de tabla FDB. El
// índice estándar dot1d (RFC 1493) es la MAC desnuda (6 octetos), pero hay
// firmwares (TP-Link Omada, verificado en #950) que lo indexan por
// <vlan>.M.M.M.M.M.M como la tabla dot1q (RFC 4363). Regla común: la MAC son
// los últimos 6 octetos; se aceptan 6 o más y se descarta cualquier índice
// con menos.
func extractMacFromOid(name, prefix string) string {
	if !strings.HasPrefix(name, prefix+".") {
		return ""
	}
	suffix := name[len(prefix)+1:]
	parts := strings.Split(suffix, ".")
	if len(parts) < 6 {
		return ""
	}
	return macFromParts(parts[len(parts)-6:])
}

// macFromParts construye la MAC en MAYÚSCULAS (#960): es la capitalización
// canónica de todo el sistema (FDB del agente, wireless, leases,
// device_attrib, device_seen). En minúscula, los mapas de merge indexados
// por MAC duplicaban cada cliente SNMP (una entrada por capitalización).
func macFromParts(parts []string) string {
	out := make([]string, 6)
	for i, p := range parts {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil || n < 0 || n > 255 {
			return ""
		}
		out[i] = fmt.Sprintf("%02X", n)
	}
	return strings.Join(out, ":")
}
