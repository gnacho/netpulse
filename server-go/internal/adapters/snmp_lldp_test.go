package adapters

import (
	"context"
	"reflect"
	"testing"
	"time"

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

// #1279 (reapertura): el flag ChassisFromLocDesc viaja del LLDP-MIB al
// contrato LldpNeighbor para que la resolución de uplinks pueda excluir la
// identidad sintetizada de la anotación local del admin.
func TestSnmpLldpNeighborsPropagaLocDesc(t *testing.T) {
	rem := []npsnmp.LldpRemEntry{
		{LocalPortNum: 21, IfIndex: 21, Chassis: "ap1", ChassisFromLocDesc: true},
		{LocalPortNum: 22, IfIndex: 22, Chassis: "sw2"},
	}
	got := snmpLldpNeighbors(rem, map[int]string{21: "1/0/21", 22: "1/0/22"})
	if !got[0].ChassisFromLocDesc {
		t.Fatalf("vecino 0: el flag ChassisFromLocDesc debe propagarse: %+v", got[0])
	}
	if got[1].ChassisFromLocDesc {
		t.Fatalf("vecino 1: identidad remota real, flag a false: %+v", got[1])
	}
}

// #931: en una boca con UN solo equipo aprendido y sin hostname DHCP ni
// alias, el sysName anunciado por LLDP es la mejor etiqueta (antes salía la
// MAC o la label del puerto). La mgmt-ip viaja al detalle.
func TestGetRouterDetailLldpNombraEquipoSolitario(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "sw1", Name: "LGS310C", Host: "192.168.1.217", Type: "managed-switch", SnmpEnabled: true}
	l.routers = []RouterConfig{cfg}
	l.lastPolled["sw1"] = &routerPolled{
		cfg: cfg,
		ports: []EthPort{
			{ID: "snmp-5", Label: "Slot0/5", Iface: "Slot0/5", Up: true, Snmp: true},
			{ID: "snmp-6", Label: "Slot0/6", Iface: "Slot0/6", Up: true, Snmp: true},
		},
		fdb: map[string]string{
			"78:55:36:02:58:BA": "Slot0/5",
			"AA:BB:CC:DD:EE:FF": "Slot0/6",
		},
		lldp: []LldpNeighbor{
			{Port: "Slot0/5", Chassis: "ryzen-ai", ChassisMac: "78:55:36:02:58:BA", Mgmt: "192.168.1.80", Caps: []string{"Wlan"}},
		},
		polledAt: time.Now().UnixMilli(),
	}

	det, err := l.GetRouterDetail(context.Background(), "sw1")
	if err != nil || det == nil {
		t.Fatalf("detalle: %v %v", det, err)
	}
	ports := map[string]EthPort{}
	for _, p := range det.Ports {
		ports[p.ID] = p
	}
	p5 := ports["snmp-5"]
	if p5.ConnectedTo != "ryzen-ai" {
		t.Fatalf("snmp-5 connectedTo: %q, esperaba ryzen-ai (LLDP sysName)", p5.ConnectedTo)
	}
	if p5.Detail != "192.168.1.80 · LLDP" {
		t.Fatalf("snmp-5 detail: %q, esperaba mgmt-ip · LLDP", p5.Detail)
	}
	if p5.DeviceMac != "78:55:36:02:58:BA" {
		t.Fatalf("snmp-5 deviceMac: %q", p5.DeviceMac)
	}
	// Puerto sin LLDP: comportamiento previo intacto (cae a la label).
	p6 := ports["snmp-6"]
	if p6.ConnectedTo != "Slot0/6" {
		t.Fatalf("snmp-6 connectedTo: %q, esperaba Slot0/6 (sin LLDP, sin cambios)", p6.ConnectedTo)
	}
}

// #931: un hostname DHCP sigue ganando al sysName LLDP (precedencia
// lease > LLDP > MAC).
func TestGetRouterDetailLldpNoPisaHostnameDhcp(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "sw1", Name: "LGS310C", Host: "192.168.1.217", Type: "managed-switch", SnmpEnabled: true}
	l.routers = []RouterConfig{cfg}
	l.lastPolled["sw1"] = &routerPolled{
		cfg: cfg,
		ports: []EthPort{
			{ID: "snmp-5", Label: "Slot0/5", Iface: "Slot0/5", Up: true, Snmp: true},
		},
		fdb:      map[string]string{"78:55:36:02:58:BA": "Slot0/5"},
		lldp:     []LldpNeighbor{{Port: "Slot0/5", Chassis: "ryzen-ai", Mgmt: "192.168.1.80"}},
		leases:   []DhcpLease{{MAC: "78:55:36:02:58:BA", Hostname: "ryzen9AI", IP: "192.168.1.80"}},
		polledAt: time.Now().UnixMilli(),
	}

	det, err := l.GetRouterDetail(context.Background(), "sw1")
	if err != nil || det == nil {
		t.Fatalf("detalle: %v %v", det, err)
	}
	for _, p := range det.Ports {
		if p.ID == "snmp-5" && p.ConnectedTo != "ryzen9AI" {
			t.Fatalf("connectedTo: %q, esperaba ryzen9AI (hostname DHCP gana a LLDP)", p.ConnectedTo)
		}
	}
}
