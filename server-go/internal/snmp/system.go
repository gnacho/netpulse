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

// PollBridgeAddress lee la MAC base del bridge (dot1dBaseBridgeAddress).
// Devuelve "" (sin error) si el equipo no contesta el OID: es opcional y
// NO debe tumbar el poll (#1036). La MAC se normaliza a MAYÚSCULAS, la
// capitalización canónica del sistema (#960).
func PollBridgeAddress(s *gosnmp.GoSNMP) (string, error) {
	result, err := s.Get([]string{OidDot1dBaseBridgeAddress})
	if err != nil {
		return "", nil
	}
	for _, v := range result.Variables {
		if v.Name != OidDot1dBaseBridgeAddress {
			continue
		}
		return bridgeMacFromPDU(v), nil
	}
	return "", nil
}

// bridgeMacFromPDU convierte el OCTET STRING de dot1dBaseBridgeAddress a
// MAC MAYÚSCULAS canónica ("" si el dato no es usable).
func bridgeMacFromPDU(v gosnmp.SnmpPDU) string {
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return ""
	}
	var b []byte
	switch val := v.Value.(type) {
	case []byte:
		b = val
	case string:
		b = []byte(val)
	default:
		return ""
	}
	if len(b) < 6 {
		return ""
	}
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", b[0], b[1], b[2], b[3], b[4], b[5])
}
