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

// #1725: la health table de RouterOS. Muestra real de un CRS305 (RouterOS
// 7.25), que es el caso que #1256 no cubría:
//
//	.100.1.2.17 = STRING: "cpu-temperature"
//	.100.1.3.17 = INTEGER: 48
//	.100.1.4.17 = INTEGER: 1
//
// El Get de 100.1.3.52.0 no contestaba nunca aquí (NoSuchObject), así que la
// temperatura se quedaba en null con el dato puesto en la tabla de al lado.
func TestMikrotikHealthRowsPareaNombreYValor(t *testing.T) {
	names := []gosnmp.SnmpPDU{
		{Name: OidMikrotikHealthName + ".17", Type: gosnmp.OctetString, Value: "cpu-temperature"},
		{Name: OidMikrotikHealthName + ".18", Type: gosnmp.OctetString, Value: "switch-fan-state"},
		{Name: OidMikrotikHealthName + ".19", Type: gosnmp.OctetString, Value: "board-temperature"},
	}
	values := []gosnmp.SnmpPDU{
		{Name: OidMikrotikHealthValue + ".17", Type: gosnmp.Integer, Value: big.NewInt(48)},
		{Name: OidMikrotikHealthValue + ".18", Type: gosnmp.Integer, Value: big.NewInt(1)},
		{Name: OidMikrotikHealthValue + ".19", Type: gosnmp.Integer, Value: big.NewInt(51)},
		{Name: OidMikrotikHealthValue + ".52.0", Type: gosnmp.Integer, Value: big.NewInt(60)},
	}
	rows := mikrotikHealthRows(names, values)
	if len(rows) != 3 {
		t.Fatalf("filas = %d, want 3 (el scalar .52.0 no es una fila de la tabla): %v", len(rows), rows)
	}
	if rows["cpu-temperature"] != 48 || rows["board-temperature"] != 51 {
		t.Fatalf("apareadas mal: %v", rows)
	}
	if temp, ok := ParseMikrotikHealth(rows); !ok || temp != 48 {
		t.Fatalf("temp = %d/%v, want 48/true (cpu-temperature manda)", temp, ok)
	}
}

func TestParseMikrotikHealthFallbackYDescartes(t *testing.T) {
	// Equipo que no nombra la sonda "cpu-temperature": el punto caliente de
	// las filas de temperatura, no la primera que responde el walk.
	rows := map[string]int{"board-temperature": 47, "switch-temperature": 53, "fan-state": 1}
	if temp, ok := ParseMikrotikHealth(rows); !ok || temp != 53 {
		t.Errorf("temp = %d/%v, want 53 (máximo de las filas de temperatura)", temp, ok)
	}
	// cpu-temperature gana aunque haya otra fila más caliente: es la lectura
	// que /system health imprime como CPU.
	conCpu := map[string]int{"cpu-temperature": 48, "switch-temperature": 61}
	if temp, ok := ParseMikrotikHealth(conCpu); !ok || temp != 48 {
		t.Errorf("temp = %d/%v, want 48 (la sonda nombrada manda)", temp, ok)
	}
	if _, ok := ParseMikrotikHealth(map[string]int{"fan-state": 1, "pcie-power": 12}); ok {
		t.Error("una tabla sin ninguna fila de temperatura no da temperatura")
	}
	if _, ok := ParseMikrotikHealth(nil); ok {
		t.Error("tabla vacía ok=false: la vital se queda en null (#441)")
	}
}

// Las descripciones reales de los cuatro equipos de la flota que sirvieron de
// muestra: dos MikroTik (un firmware y otro) y dos que no, para los que
// preguntar la tabla costaría un timeout por poll.
func TestDeclaresMikrotik(t *testing.T) {
	for _, descr := range []string{
		"RouterOS CRS305-1G-4S+ 7.25",
		"RouterOS hEX",
		"CSS610-8G-2S+ SwOS v2.21",
	} {
		if !declaresMikrotik(descr) {
			t.Errorf("%q es MikroTik y se descartó", descr)
		}
	}
	for _, descr := range []string{
		"FN SWITCH 8 PRO",
		"Netgear XS708T 8-Port 10-Gigabit Copper Smart Managed Pro Switch",
		"Linux ds1821plus 5.2",
		"",
	} {
		if declaresMikrotik(descr) {
			t.Errorf("%q no es MikroTik y se caminaría su tabla", descr)
		}
	}
}
