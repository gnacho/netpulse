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

	fdbPdus, err := walkSafe(s, OidDot1dTpFdbPort)
	if err != nil {
		return nil, fmt.Errorf("snmp fdb walk: %w", err)
	}
	var out []FdbEntry
	for _, pdu := range fdbPdus {
		mac := extractMacFromOid(pdu.Name, OidDot1dTpFdbPort)
		if mac == "" {
			continue
		}
		bridgePort := int(uint64Val(pdu))
		ifIdx := portToIfIndex[bridgePort]
		out = append(out, FdbEntry{
			MAC:            mac,
			BridgePortIndex: bridgePort,
			IfIndex:        ifIdx,
		})
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
