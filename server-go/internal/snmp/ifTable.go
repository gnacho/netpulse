package snmp

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/gosnmp/gosnmp"
)

type PortStats struct {
	Index         int
	Name          string
	Descr         string
	Alias         string
	IfType        int
	SpeedBps      uint64
	HighSpeedMbps uint32
	OperUp        bool
	RxBytes       uint64
	TxBytes       uint64
	RxErrors      uint64
	TxErrors      uint64
}

func (p PortStats) SpeedString() string {
	if !p.OperUp {
		return ""
	}
	var bps uint64
	if p.HighSpeedMbps > 0 {
		bps = uint64(p.HighSpeedMbps) * 1_000_000
	} else {
		bps = p.SpeedBps
	}
	if bps == 0 {
		return ""
	}
	if bps >= 1_000_000_000 {
		return fmt.Sprintf("%d Gbps", bps/1_000_000_000)
	}
	if bps >= 1_000_000 {
		return fmt.Sprintf("%d Mbps", bps/1_000_000)
	}
	return fmt.Sprintf("%d Kbps", bps/1_000)
}

func (p PortStats) DisplayName() string {
	if p.Alias != "" {
		return p.Alias
	}
	if p.Name != "" {
		return p.Name
	}
	if p.Descr != "" {
		return p.Descr
	}
	return fmt.Sprintf("port-%d", p.Index)
}

func PollIfTable(s *gosnmp.GoSNMP) ([]PortStats, error) {
	oids := []string{
		OidIfIndex, OidIfDescr, OidIfType, OidIfSpeed, OidIfOperStatus,
		OidIfInOctets, OidIfInErrors, OidIfOutOctets, OidIfOutErrors,
		OidIfName, OidIfHighSpeed, OidIfAlias,
	}
	byIdx := map[int]*PortStats{}
	for _, oid := range oids {
		pdus, err := walkSafe(s, oid)
		if err != nil {
			continue
		}
		for _, pdu := range pdus {
			idx := trailingIndex(pdu.Name, oid)
			if idx <= 0 {
				continue
			}
			ps, ok := byIdx[idx]
			if !ok {
				ps = &PortStats{Index: idx}
				byIdx[idx] = ps
			}
			applyPortField(ps, oid, pdu)
		}
	}
	out := make([]PortStats, 0, len(byIdx))
	for _, ps := range byIdx {
		// Solo interfaces Ethernet fisicas (ifType 6 = ethernetCsmacd).
		// Excluye VLANs (l2vlan), LAGs (ieee8023adLag), tunnel, etc.
		if ps.IfType == 6 {
			out = append(out, *ps)
		}
	}
	log.Printf("[netpulse:snmp] PollIfTable: %d raw -> %d filtered (ethernetCsmacd)", len(byIdx), len(out))
	sortByIndex(out)
	return out, nil
}

func applyPortField(ps *PortStats, oid string, pdu gosnmp.SnmpPDU) {
	switch oid {
	case OidIfIndex:
		ps.Index = int(uint64Val(pdu))
	case OidIfDescr:
		ps.Descr = stringVal(pdu)
	case OidIfType:
		ps.IfType = int(uint64Val(pdu))
	case OidIfSpeed:
		ps.SpeedBps = uint64Val(pdu)
	case OidIfOperStatus:
		ps.OperUp = uint64Val(pdu) == 1
	case OidIfInOctets:
		ps.RxBytes = uint64Val(pdu)
	case OidIfInErrors:
		ps.RxErrors = uint64Val(pdu)
	case OidIfOutOctets:
		ps.TxBytes = uint64Val(pdu)
	case OidIfOutErrors:
		ps.TxErrors = uint64Val(pdu)
	case OidIfName:
		ps.Name = stringVal(pdu)
	case OidIfHighSpeed:
		v := uint64Val(pdu)
		if v <= 0xFFFFFFFF {
			ps.HighSpeedMbps = uint32(v)
		}
	case OidIfAlias:
		ps.Alias = stringVal(pdu)
	}
}

func walkSafe(s *gosnmp.GoSNMP, oid string) ([]gosnmp.SnmpPDU, error) {
	out, err := s.BulkWalkAll(oid)
	if err != nil {
		out2, err2 := s.WalkAll(oid)
		if err2 != nil {
			return nil, err
		}
		return out2, nil
	}
	return out, nil
}

func trailingIndex(name, prefix string) int {
	if !strings.HasPrefix(name, prefix+".") {
		return 0
	}
	suffix := name[len(prefix)+1:]
	if strings.Contains(suffix, ".") {
		return 0
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return 0
	}
	return n
}

func sortByIndex(ps []PortStats) {
	for i := 1; i < len(ps); i++ {
		for j := i; j > 0 && ps[j-1].Index > ps[j].Index; j-- {
			ps[j-1], ps[j] = ps[j], ps[j-1]
		}
	}
}
