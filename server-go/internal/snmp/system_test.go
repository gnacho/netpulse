package snmp

import (
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
