package snmp

import (
	"fmt"
	"math/big"

	"github.com/gosnmp/gosnmp"
)

type SystemInfo struct {
	Descr    string
	Name     string
	UpTimeSec uint64
}

func PollSystem(s *gosnmp.GoSNMP) (*SystemInfo, error) {
	oids := []string{OidSysDescr, OidSysName, OidSysUpTime}
	result, err := s.Get(oids)
	if err != nil {
		return nil, fmt.Errorf("snmp system get: %w", err)
	}
	info := &SystemInfo{}
	for _, v := range result.Variables {
		switch v.Name {
		case OidSysDescr:
			info.Descr = stringVal(v)
		case OidSysName:
			info.Name = stringVal(v)
		case OidSysUpTime:
			info.UpTimeSec = uint64Val(v) / 100
		}
	}
	return info, nil
}

func stringVal(v gosnmp.SnmpPDU) string {
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return ""
	}
	switch val := v.Value.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func uint64Val(v gosnmp.SnmpPDU) uint64 {
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return 0
	}
	if bi, ok := v.Value.(*big.Int); ok {
		return bi.Uint64()
	}
	return gosnmp.ToBigInt(v.Value).Uint64()
}
