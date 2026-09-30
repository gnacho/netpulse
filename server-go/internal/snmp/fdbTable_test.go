package snmp

import (
	"testing"

	"github.com/gosnmp/gosnmp"
)

func fdbPdu(name string, port uint) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: name, Type: gosnmp.Integer, Value: port}
}

// #948: el walk dot1d puede devolver PDUs que no producen ninguna entrada
// utilizable (p.ej. el propio OID de la columna sin instancia, o índices con
// un número de octetos distinto de 6). fdbEntries debe descartarlos sin
// inventar entradas.
func TestFdbEntriesDot1dSkipsUnusablePdus(t *testing.T) {
	portMap := map[int]int{3: 103}
	pdus := []gosnmp.SnmpPDU{
		// OID de la columna sin índice (caso leaf del fallback GetRequest de gosnmp).
		fdbPdu(OidDot1dTpFdbPort, 3),
		// Índice de 5 octetos: no es una MAC.
		fdbPdu(OidDot1dTpFdbPort+".1.2.3.4.5", 3),
		// Octeto fuera de rango.
		fdbPdu(OidDot1dTpFdbPort+".1.2.3.4.5.999", 3),
		// Prefijo distinto (otra columna de la tabla).
		fdbPdu(".1.3.6.1.2.1.17.4.3.1.3.1.2.3.4.5.6", 3),
	}
	if got := fdbEntries(portMap, pdus, false); len(got) != 0 {
		t.Fatalf("esperaba 0 entradas, obtuve %d: %+v", len(got), got)
	}
}

func TestFdbEntriesDot1d(t *testing.T) {
	portMap := map[int]int{3: 103}
	pdus := []gosnmp.SnmpPDU{
		fdbPdu(OidDot1dTpFdbPort+".140.22.24.187.30.12", 3),
		// bridgePort sin mapeo a ifIndex: cae al propio bridgePort (#661).
		fdbPdu(OidDot1dTpFdbPort+".0.12.34.56.78.90", 7),
		// #950: índice real de TP-Link Omada (SG3428X-M2, issue #928):
		// <vlan=1>.<mac> en la tabla dot1d.
		fdbPdu(OidDot1dTpFdbPort+".1.0.4.75.233.178.29", 49167),
	}
	got := fdbEntries(portMap, pdus, false)
	if len(got) != 3 {
		t.Fatalf("esperaba 3 entradas, obtuve %d: %+v", len(got), got)
	}
	if got[0].MAC != "8c:16:18:bb:1e:0c" || got[0].BridgePortIndex != 3 || got[0].IfIndex != 103 {
		t.Errorf("entrada 0 inesperada: %+v", got[0])
	}
	if got[1].MAC != "00:0c:22:38:4e:5a" || got[1].BridgePortIndex != 7 || got[1].IfIndex != 7 {
		t.Errorf("entrada 1 inesperada: %+v", got[1])
	}
	if got[2].MAC != "00:04:4b:e9:b2:1d" || got[2].BridgePortIndex != 49167 || got[2].IfIndex != 49167 {
		t.Errorf("entrada 2 inesperada: %+v", got[2])
	}
}

func TestFdbEntriesDot1q(t *testing.T) {
	portMap := map[int]int{5: 205}
	pdus := []gosnmp.SnmpPDU{
		// Índice compuesto <vlan>.M.M.M.M.M.M: la MAC son los últimos 6 octetos.
		fdbPdu(OidDot1qTpFdbPort+".100.140.22.24.187.30.12", 5),
		// Prefijo distinto: descartado.
		fdbPdu(".1.3.6.1.2.1.17.7.1.2.2.1.3.100.140.22.24.187.30.12", 5),
	}
	got := fdbEntries(portMap, pdus, true)
	if len(got) != 1 {
		t.Fatalf("esperaba 1 entrada, obtuve %d: %+v", len(got), got)
	}
	if got[0].MAC != "8c:16:18:bb:1e:0c" || got[0].BridgePortIndex != 5 || got[0].IfIndex != 205 {
		t.Errorf("entrada inesperada: %+v", got[0])
	}
}
