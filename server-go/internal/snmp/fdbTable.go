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

func PollFdbTable(s *gosnmp.GoSNMP) ([]FdbEntry, error) {
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
	// cuando la dot1d no devuelve entradas (#661).
	fdbPdus, errD1 := walkSafe(s, OidDot1dTpFdbPort)
	useDot1q := len(fdbPdus) == 0
	if useDot1q {
		fdbPdus, _ = walkSafe(s, OidDot1qTpFdbPort)
	}

	var out []FdbEntry
	for _, pdu := range fdbPdus {
		var mac string
		if useDot1q {
			mac = extractMacFromOidLast6(pdu.Name, OidDot1qTpFdbPort)
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
	if len(out) == 0 && errD1 != nil {
		return nil, fmt.Errorf("snmp fdb walk: %w", errD1)
	}
	return out, nil
}

func extractMacFromOid(name, prefix string) string {
	if !strings.HasPrefix(name, prefix+".") {
		return ""
	}
	suffix := name[len(prefix)+1:]
	parts := strings.Split(suffix, ".")
	if len(parts) != 6 {
		return ""
	}
	return macFromParts(parts)
}

// extractMacFromOidLast6: variante para el índice compuesto de la tabla
// dot1q, que es <vlan>.M.M.M.M.M.M (la MAC son los últimos 6 octetos).
func extractMacFromOidLast6(name, prefix string) string {
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

func macFromParts(parts []string) string {
	out := make([]string, 6)
	for i, p := range parts {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil || n < 0 || n > 255 {
			return ""
		}
		out[i] = fmt.Sprintf("%02x", n)
	}
	return strings.Join(out, ":")
}
