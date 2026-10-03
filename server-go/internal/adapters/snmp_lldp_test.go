package adapters

import (
	"reflect"
	"testing"

	npsnmp "github.com/gnacho/netpulse/server-go/internal/snmp"
)

// #931: la conversión LLDP-MIB → LldpNeighbor nombra el puerto como el FDB
// (DisplayName del ifTable) para que inferLldpLinks case el vecino con las
// MACs aprendidas; sin mapeo cae al bridgePort y a un nombre generado
// (paridad con snmpFdbMap, #661).
func TestSnmpLldpNeighborsNaming(t *testing.T) {
	rem := []npsnmp.LldpRemEntry{
		{LocalPortNum: 5, IfIndex: 5, ChassisMac: "78:55:36:02:58:BA", Chassis: "ryzen-ai", Mgmt: "192.168.1.80", Caps: []string{"Wlan"}, PortDesc: "enp195s0"},
		// Sin DisplayName para el ifIndex: cae al LocalPortNum si tiene nombre.
		{LocalPortNum: 7, IfIndex: 70, ChassisMac: "D8:EC:5E:60:C7:82", Chassis: "LGS310C-2"},
		// Sin nombre en ninguno de los dos: nombre generado.
		{LocalPortNum: 9, IfIndex: 90, Chassis: "otro"},
	}
	names := map[int]string{5: "Slot0/5", 7: "Slot0/7"}
	got := snmpLldpNeighbors(rem, names)
	if len(got) != 3 {
		t.Fatalf("esperaba 3 vecinos, obtuve %d", len(got))
	}
	want0 := LldpNeighbor{Port: "Slot0/5", ChassisMac: "78:55:36:02:58:BA", Chassis: "ryzen-ai", Mgmt: "192.168.1.80", Caps: []string{"Wlan"}, PortDesc: "enp195s0"}
	if !reflect.DeepEqual(got[0], want0) {
		t.Fatalf("vecino 0 inesperado:\n got %+v\nwant %+v", got[0], want0)
	}
	if got[1].Port != "Slot0/7" {
		t.Fatalf("vecino 1 puerto: %q, esperaba Slot0/7 (fallback a LocalPortNum)", got[1].Port)
	}
	if got[2].Port != "port-9" {
		t.Fatalf("vecino 2 puerto: %q, esperaba port-9 (nombre generado)", got[2].Port)
	}
}

func TestSnmpLldpNeighborsVacio(t *testing.T) {
	if got := snmpLldpNeighbors(nil, map[int]string{5: "Slot0/5"}); got != nil {
		t.Fatalf("sin vecinos debe devolver nil, obtuve %+v", got)
	}
}
