package snmp

import (
	"math/big"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// #1036: la MAC base del bridge se normaliza a MAYÚSCULAS (#960) o "" si
// el dato no es usable (OID ausente, payload corto).
func TestBridgeMacFromPDU(t *testing.T) {
	good := gosnmp.SnmpPDU{Name: OidDot1dBaseBridgeAddress, Type: gosnmp.OctetString, Value: []byte{0x04, 0x95, 0xe6, 0x76, 0x55, 0xa1}}
	if got := bridgeMacFromPDU(good); got != "04:95:E6:76:55:A1" {
		t.Errorf("bytes: got %q", got)
	}
	str := gosnmp.SnmpPDU{Name: OidDot1dBaseBridgeAddress, Type: gosnmp.OctetString, Value: string([]byte{0xa4, 0x7e, 0xfa, 0x65, 0x0c, 0xaa})}
	if got := bridgeMacFromPDU(str); got != "A4:7E:FA:65:0C:AA" {
		t.Errorf("string: got %q", got)
	}
	short := gosnmp.SnmpPDU{Name: OidDot1dBaseBridgeAddress, Type: gosnmp.OctetString, Value: []byte{0x01, 0x02}}
	if got := bridgeMacFromPDU(short); got != "" {
		t.Errorf("short: got %q, want vacío", got)
	}
	noObj := gosnmp.SnmpPDU{Name: OidDot1dBaseBridgeAddress, Type: gosnmp.NoSuchObject, Value: nil}
	if got := bridgeMacFromPDU(noObj); got != "" {
		t.Errorf("nosuchobject: got %q, want vacío", got)
	}
}

// #1256: el Gauge32 del OID MikroTik da grados enteros; NoSuchInstance u
// otro tipo dejan la vital a null (ok=false), y un valor fuera de rango
// físico se descarta como basura.
func TestMikrotikTempFromPDU(t *testing.T) {
	good := gosnmp.SnmpPDU{Name: OidMikrotikTemp, Type: gosnmp.Gauge32, Value: big.NewInt(60)}
	if n, ok := mikrotikTempFromPDU(good); !ok || n != 60 {
		t.Errorf("gauge 60: got (%d,%v)", n, ok)
	}
	noObj := gosnmp.SnmpPDU{Name: OidMikrotikTemp, Type: gosnmp.NoSuchInstance, Value: nil}
	if _, ok := mikrotikTempFromPDU(noObj); ok {
		t.Error("nosuchinstance debe dar ok=false")
	}
	str := gosnmp.SnmpPDU{Name: OidMikrotikTemp, Type: gosnmp.OctetString, Value: "60"}
	if _, ok := mikrotikTempFromPDU(str); ok {
		t.Error("octetstring debe dar ok=false")
	}
	hot := gosnmp.SnmpPDU{Name: OidMikrotikTemp, Type: gosnmp.Gauge32, Value: big.NewInt(900)}
	if _, ok := mikrotikTempFromPDU(hot); ok {
		t.Error("900 fuera de rango debe dar ok=false")
	}
}
