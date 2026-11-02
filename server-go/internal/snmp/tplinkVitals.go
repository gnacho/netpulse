// tplinkVitals.go — identity and vitals of TP-Link switches through their
// private enterprise MIB (enterprise 11863). Their firmware answers neither
// ENTITY-MIB nor HOST-RESOURCES-MIB, so the generic readers leave model,
// firmware, CPU and memory empty even though the data is available.
//
//   - tpSysInfoHwVersion.0   1.3.6.1.4.1.11863.6.1.1.5.0  "SG3210X-M2 1.0"
//   - tpSysInfoSwVersion.0   1.3.6.1.4.1.11863.6.1.1.6.0  "1.0.21 Build ..."
//   - tpSysMonitorCpu5Minutes.1
//     1.3.6.1.4.1.11863.6.4.1.1.1.1.4.1  INTEGER, percentage
//   - tpSysMonitorMemoryUtilization.1
//     1.3.6.1.4.1.11863.6.4.1.2.1.1.2.1  INTEGER, percentage
//
// The table entry is read with Get instead of a walk: the columns are known
// and walking the monitoring subtree can be expensive on these agents. A
// device without the MIB answers NoSuchObject and every field stays empty.
package snmp

import (
	"math/big"
	"strings"

	"github.com/gosnmp/gosnmp"
)

const (
	// EnterpriseTpLink: TP-Link enterprise number in sysObjectID.
	EnterpriseTpLink = 11863

	OidTpLinkHwVersion = ".1.3.6.1.4.1.11863.6.1.1.5.0"
	OidTpLinkSwVersion = ".1.3.6.1.4.1.11863.6.1.1.6.0"
	OidTpLinkCpu5Min   = ".1.3.6.1.4.1.11863.6.4.1.1.1.1.4.1"
	OidTpLinkMemUtil   = ".1.3.6.1.4.1.11863.6.4.1.2.1.1.2.1"
)

// TpLinkInfo carries the vendor fields that map onto the router contract.
// Empty strings and false flags mean the agent did not answer that OID.
type TpLinkInfo struct {
	Hardware  string
	Firmware  string
	CPU       int
	HasCPU    bool
	Memory    int
	HasMemory bool
}

// IsTpLink reports whether sysObjectID belongs to the TP-Link enterprise
// subtree. The MIB-2 object ID is the stable vendor signal: sysDescr varies
// between firmware families and is not reliable enough to choose a quirk.
func IsTpLink(sysObjectID string) bool {
	id := strings.TrimSpace(sysObjectID)
	if id != "" && !strings.HasPrefix(id, ".") {
		id = "." + id
	}
	return strings.HasPrefix(id, ".1.3.6.1.4.1.11863.")
}

// PollTpLinkInfo reads the TP-Link identity and vitals in one Get. Failures
// are silent by design: a switch that does not publish these OIDs must keep
// its generic or empty values, never a fabricated zero.
func PollTpLinkInfo(s *gosnmp.GoSNMP) TpLinkInfo {
	if s == nil {
		return TpLinkInfo{}
	}
	var info TpLinkInfo
	result, err := s.Get([]string{
		OidTpLinkHwVersion,
		OidTpLinkSwVersion,
		OidTpLinkCpu5Min,
		OidTpLinkMemUtil,
	})
	if err != nil {
		return info
	}
	for _, v := range result.Variables {
		switch v.Name {
		case OidTpLinkHwVersion:
			info.Hardware = strings.TrimSpace(stringVal(v))
		case OidTpLinkSwVersion:
			info.Firmware = strings.TrimSpace(stringVal(v))
		case OidTpLinkCpu5Min:
			info.CPU, info.HasCPU = tpLinkPercent(v)
		case OidTpLinkMemUtil:
			info.Memory, info.HasMemory = tpLinkPercent(v)
		}
	}
	return info
}

// tpLinkPercent accepts only numeric PDUs within 0..100. Anything else is
// unknown: a malformed value is not a measurement.
func tpLinkPercent(v gosnmp.SnmpPDU) (int, bool) {
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return 0, false
	}
	switch v.Type {
	case gosnmp.Gauge32, gosnmp.Integer, gosnmp.Counter32, gosnmp.Counter64:
	default:
		return 0, false
	}
	bi, ok := v.Value.(*big.Int)
	if !ok {
		bi = gosnmp.ToBigInt(v.Value)
	}
	if bi == nil || !bi.IsInt64() {
		return 0, false
	}
	n := bi.Int64()
	if n < 0 || n > 100 {
		return 0, false
	}
	return int(n), true
}
