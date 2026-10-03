package snmp

import (
	"reflect"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// Fixture grabada del Linksys LGS310C real (3-Oct-2026, #931): un vecino
// (PC con lldpd) en el puerto local 5. Los OIDs son los literales del
// snmpwalk para que el test pinae el formato exacto del índice.
func lgs310cRemPdus() []gosnmp.SnmpPDU {
	const base = OidLldpRemTable
	return []gosnmp.SnmpPDU{
		{Name: base + ".4.9900.5.1", Type: gosnmp.Integer, Value: 4},                            // chassisIdSubtype = macAddress
		{Name: base + ".5.9900.5.1", Type: gosnmp.OctetString, Value: []byte{0x78, 0x55, 0x36, 0x02, 0x58, 0xBA}},
		{Name: base + ".6.9900.5.1", Type: gosnmp.Integer, Value: 3},                            // portIdSubtype = macAddress
		{Name: base + ".7.9900.5.1", Type: gosnmp.OctetString, Value: []byte{0x78, 0x55, 0x36, 0x02, 0x58, 0xBA}},
		{Name: base + ".8.9900.5.1", Type: gosnmp.OctetString, Value: []byte("enp195s0")},       // portDesc
		{Name: base + ".9.9900.5.1", Type: gosnmp.OctetString, Value: []byte("ryzen-ai")},       // sysName
		{Name: base + ".10.9900.5.1", Type: gosnmp.OctetString, Value: []byte("CachyOS Linux")}, // sysDesc (ignorado)
		{Name: base + ".11.9900.5.1", Type: gosnmp.OctetString, Value: []byte{0x39, 0x00}},      // capsSupported
		{Name: base + ".12.9900.5.1", Type: gosnmp.OctetString, Value: []byte{0x10, 0x00}},      // capsEnabled
	}
}

// Fixture del lldpRemManAddrTable del mismo switch: IPv4 e IPv6 del vecino
// (5.1), la dirección viaja en el índice.
func lgs310cManAddrPdus() []gosnmp.SnmpPDU {
	const base = OidLldpRemManAddr
	return []gosnmp.SnmpPDU{
		{Name: base + ".3.9900.5.1.1.4.192.168.1.80", Type: gosnmp.Integer, Value: 2},
		{Name: base + ".3.9900.5.1.2.16.254.128.0.0.0.0.0.0.220.231.67.22.10.131.233.175", Type: gosnmp.Integer, Value: 2},
		{Name: base + ".5.9900.5.1.1.4.192.168.1.80", Type: gosnmp.ObjectIdentifier, Value: ".0.0"},
	}
}

func TestLldpRemEntriesLgs310c(t *testing.T) {
	portMap := map[int]int{5: 5}
	mgmt := lldpMgmtAddrs(lgs310cManAddrPdus())
	got := lldpRemEntries(lgs310cRemPdus(), portMap, mgmt)
	if len(got) != 1 {
		t.Fatalf("esperaba 1 vecino, obtuve %d: %+v", len(got), got)
	}
	e := got[0]
	want := LldpRemEntry{
		LocalPortNum: 5,
		IfIndex:      5,
		ChassisMac:   "78:55:36:02:58:BA",
		Chassis:      "ryzen-ai",
		Mgmt:         "192.168.1.80",
		Caps:         []string{"Wlan"},
		PortDesc:     "enp195s0",
	}
	if !reflect.DeepEqual(e, want) {
		t.Fatalf("vecino inesperado:\n got %+v\nwant %+v", e, want)
	}
}

// Sin mapeo dot1dBasePortIfIndex: el IfIndex cae al número de puerto de
// bridge (paridad con el FDB, #661).
func TestLldpRemEntriesIfIndexFallback(t *testing.T) {
	got := lldpRemEntries(lgs310cRemPdus(), map[int]int{7: 70}, nil)
	if len(got) != 1 {
		t.Fatalf("esperaba 1 vecino, obtuve %d", len(got))
	}
	if got[0].IfIndex != 5 {
		t.Fatalf("IfIndex fallback: %d, esperaba 5", got[0].IfIndex)
	}
	if got[0].Mgmt != "" {
		t.Fatalf("Mgmt sin tabla manaddr: %q, esperaba vacío", got[0].Mgmt)
	}
}

// Chasis NO-MAC (subtype 7 = local) sin sysName: el id viaja a Chassis y
// ChassisMac queda vacío; con sysName este gana al id.
func TestLldpRemEntriesChassisNoMac(t *testing.T) {
	const base = OidLldpRemTable
	pdus := []gosnmp.SnmpPDU{
		{Name: base + ".4.100.2.1", Type: gosnmp.Integer, Value: 7},
		{Name: base + ".5.100.2.1", Type: gosnmp.OctetString, Value: []byte("switch-sotano")},
	}
	got := lldpRemEntries(pdus, nil, nil)
	if len(got) != 1 {
		t.Fatalf("esperaba 1 vecino, obtuve %d", len(got))
	}
	if got[0].Chassis != "switch-sotano" || got[0].ChassisMac != "" {
		t.Fatalf("chasis inesperado: %+v", got[0])
	}

	pdus = append(pdus,
		gosnmp.SnmpPDU{Name: base + ".9.100.2.1", Type: gosnmp.OctetString, Value: []byte("SW1.lan")},
	)
	got = lldpRemEntries(pdus, nil, nil)
	if got[0].Chassis != "SW1.lan" {
		t.Fatalf("sysName debe ganar al id: %+v", got[0])
	}
}

// PDUs basura (OID de columna sin instancia, índices cortos/largos,
// columnas fuera de rango, valores no enteros) no producen entradas ni
// pánico.
func TestLldpRemEntriesSkipsUnusablePdus(t *testing.T) {
	const base = OidLldpRemTable
	pdus := []gosnmp.SnmpPDU{
		{Name: base, Type: gosnmp.Integer, Value: 4},
		{Name: base + ".4.9900.5", Type: gosnmp.Integer, Value: 4},          // índice corto
		{Name: base + ".4.9900.5.1.9", Type: gosnmp.Integer, Value: 4},      // índice largo
		{Name: base + ".3.9900.5.1", Type: gosnmp.Integer, Value: 4},        // columna 3 (índice, not-accessible)
		{Name: base + ".13.9900.5.1", Type: gosnmp.Integer, Value: 4},       // columna inexistente
		{Name: base + ".4.9900.0.1", Type: gosnmp.Integer, Value: 4},        // puerto 0
		{Name: base + ".4.9900.x.1", Type: gosnmp.Integer, Value: 4},        // componente no entero
		{Name: ".1.0.8802.1.1.2.1.4.1.2.4.9900.5.1", Type: gosnmp.Integer, Value: 4}, // prefijo distinto
	}
	if got := lldpRemEntries(pdus, nil, nil); len(got) != 0 {
		t.Fatalf("esperaba 0 entradas, obtuve %d: %+v", len(got), got)
	}
}

// Dos vecinos en puertos distintos conservan orden y campos independientes.
func TestLldpRemEntriesDosVecinos(t *testing.T) {
	const base = OidLldpRemTable
	pdus := []gosnmp.SnmpPDU{
		{Name: base + ".4.9900.5.1", Type: gosnmp.Integer, Value: 4},
		{Name: base + ".5.9900.5.1", Type: gosnmp.OctetString, Value: []byte{0x78, 0x55, 0x36, 0x02, 0x58, 0xBA}},
		{Name: base + ".9.9900.5.1", Type: gosnmp.OctetString, Value: []byte("ryzen-ai")},
		{Name: base + ".4.9900.8.1", Type: gosnmp.Integer, Value: 4},
		{Name: base + ".5.9900.8.1", Type: gosnmp.OctetString, Value: []byte{0xD8, 0xEC, 0x5E, 0x60, 0xC7, 0x82}},
		{Name: base + ".9.9900.8.1", Type: gosnmp.OctetString, Value: []byte("LGS310C-2")},
	}
	got := lldpRemEntries(pdus, map[int]int{5: 5, 8: 8}, nil)
	if len(got) != 2 {
		t.Fatalf("esperaba 2 vecinos, obtuve %d", len(got))
	}
	if got[0].LocalPortNum != 5 || got[0].Chassis != "ryzen-ai" {
		t.Fatalf("vecino 0 inesperado: %+v", got[0])
	}
	if got[1].LocalPortNum != 8 || got[1].ChassisMac != "D8:EC:5E:60:C7:82" {
		t.Fatalf("vecino 1 inesperado: %+v", got[1])
	}
}

func TestLldpMgmtAddrs(t *testing.T) {
	got := lldpMgmtAddrs(lgs310cManAddrPdus())
	if got["5.1"] != "192.168.1.80" {
		t.Fatalf("mgmt 5.1 inesperada: %q", got["5.1"])
	}
	if len(got) != 1 {
		t.Fatalf("solo IPv4 debe indexarse: %+v", got)
	}
}

func TestLldpCapsFromBits(t *testing.T) {
	cases := []struct {
		b    []byte
		want []string
	}{
		{[]byte{0x10, 0x00}, []string{"Wlan"}},                  // LGS310C real (lldpd PC)
		{[]byte{0x28, 0x00}, []string{"Bridge", "Router"}},      // switch/router típico
		{[]byte{0x01, 0x00}, []string{"Station"}},
		{[]byte{0x39, 0x00}, []string{"Bridge", "Wlan", "Router", "Station"}},
		{nil, nil},
		{[]byte{0x00, 0x00}, nil},
	}
	for i, c := range cases {
		if got := lldpCapsFromBits(c.b); !reflect.DeepEqual(got, c.want) {
			t.Errorf("caso %d: got %v, want %v", i, got, c.want)
		}
	}
}
