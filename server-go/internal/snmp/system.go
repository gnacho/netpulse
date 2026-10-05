package snmp

import (
	"fmt"
	"math/big"

	"github.com/gosnmp/gosnmp"
)

type SystemInfo struct {
	Descr     string
	Name      string
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

// PollMikrotikTemp lee la temperatura del chasis de un switch MikroTik SwOS
// (#1256, OidMikrotikTemp, grados enteros). Best-effort: cualquier fallo
// (OID ausente, equipo no MikroTik, timeout) devuelve ok=false y NUNCA
// tumba el poll, igual que PollBridgeAddress (#1036).
func PollMikrotikTemp(s *gosnmp.GoSNMP) (temp int, ok bool) {
	result, err := s.Get([]string{OidMikrotikTemp})
	if err != nil {
		return 0, false
	}
	for _, v := range result.Variables {
		if v.Name != OidMikrotikTemp {
			continue
		}
		return mikrotikTempFromPDU(v)
	}
	return 0, false
}

// mikrotikTempFromPDU convierte el Gauge32 del OID de temperatura a (°C,
// true). NoSuchObject/Instance u otro tipo de dato → (0, false): la vital
// queda a null en la UI (#441), no a cero falso.
func mikrotikTempFromPDU(v gosnmp.SnmpPDU) (int, bool) {
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return 0, false
	}
	// Solo tipos numéricos: ToBigInt parsea strings y octet strings, que
	// aquí serían un dato del equipo mal formado, no una temperatura.
	switch v.Type {
	case gosnmp.Gauge32, gosnmp.Integer, gosnmp.Counter32, gosnmp.Counter64:
	default:
		return 0, false
	}
	bi, okBig := v.Value.(*big.Int)
	if !okBig {
		bi = gosnmp.ToBigInt(v.Value)
	}
	if !bi.IsInt64() {
		return 0, false
	}
	n := bi.Int64()
	if n < -40 || n > 125 {
		return 0, false // fuera de rango físico: dato basura, no una temp
	}
	return int(n), true
}
