package snmp

import (
	"math/big"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func entPDU(col, idx string, typ gosnmp.Asn1BER, value any) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: OidEntPhysicalTable + "." + col + "." + idx, Type: typ, Value: value}
}

func entClass(idx string, class int) gosnmp.SnmpPDU {
	return entPDU("5", idx, gosnmp.Integer, big.NewInt(int64(class)))
}

func entString(col, idx, value string) gosnmp.SnmpPDU {
	return entPDU(col, idx, gosnmp.OctetString, value)
}

// #1767: XS708T leaves entPhysicalFirmwareRev empty and publishes the
// firmware in entPhysicalSoftwareRev on the chassis row.
func TestParseEntitySoftwareRevOnChassis(t *testing.T) {
	info := ParseEntityTable([]gosnmp.SnmpPDU{
		entClass("1", 3),
		entString("9", "1", ""),
		entString("10", "1", "7.0.0.24"),
		entString("11", "1", "4KN3837U8009A"),
		entString("13", "1", "XS708T"),
	})
	if info.Firmware != "7.0.0.24" {
		t.Errorf("firmware = %q, want 7.0.0.24", info.Firmware)
	}
	if info.ModelShort != "XS708T" {
		t.Errorf("modelShort = %q, want XS708T", info.ModelShort)
	}
}

// #1767: S3300-28X-PoE+ publishes a stack(11) row first and the chassis
// identity on row 2. Selection is by class, not by index.
func TestParseEntityPicksChassisAfterStackRow(t *testing.T) {
	info := ParseEntityTable([]gosnmp.SnmpPDU{
		entClass("1", 11),
		entString("13", "1", "Stack row"),
		entClass("2", 3),
		entString("10", "2", "6.6.4.28"),
		entString("11", "2", "3TM1447L80041"),
		entString("13", "2", "S3300-28X-PoE+"),
	})
	if info.Firmware != "6.6.4.28" || info.ModelShort != "S3300-28X-PoE+" {
		t.Errorf("got %+v, want chassis row of S3300", info)
	}
}

// #1767: when softwareRev is empty, firmwareRev is the fallback.
func TestParseEntityFirmwareRevFallback(t *testing.T) {
	info := ParseEntityTable([]gosnmp.SnmpPDU{
		entClass("1", 3),
		entString("9", "1", "3.6.11.55242"),
		entString("10", "1", ""),
		entString("13", "1", "FN-SWITCH-8 PRO"),
	})
	if info.Firmware != "3.6.11.55242" {
		t.Errorf("firmware = %q, want firmwareRev fallback", info.Firmware)
	}
}

// #1767: a missing table must not invent identity.
func TestParseEntityEmpty(t *testing.T) {
	if info := ParseEntityTable(nil); info.Firmware != "" || info.ModelShort != "" {
		t.Errorf("empty table gave %+v", info)
	}
}

// An empty chassis row does not hide a later row that does publish identity.
func TestParseEntityEmptyChassisFallsThrough(t *testing.T) {
	info := ParseEntityTable([]gosnmp.SnmpPDU{
		entClass("1", 3),
		entClass("2", 11),
		entString("10", "2", "1.2.3"),
		entString("13", "2", "Fallback"),
	})
	if info.Firmware != "1.2.3" || info.ModelShort != "Fallback" {
		t.Errorf("got %+v, want first populated row", info)
	}
}
