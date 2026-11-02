package snmp

import (
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestIsTpLink(t *testing.T) {
	tests := []struct {
		oid  string
		want bool
	}{
		{".1.3.6.1.4.1.11863.5.214", true},
		{"1.3.6.1.4.1.11863.5.214", true},
		{".1.3.6.1.4.1.4526.11.1", false},
		{".1.3.6.1.4.1.118630.1", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsTpLink(tt.oid); got != tt.want {
			t.Errorf("IsTpLink(%q) = %v; want %v", tt.oid, got, tt.want)
		}
	}
}

func TestTpLinkPercent(t *testing.T) {
	tests := []struct {
		name string
		pdu  gosnmp.SnmpPDU
		want int
		ok   bool
	}{
		{"integer", gosnmp.SnmpPDU{Type: gosnmp.Integer, Value: 7}, 7, true},
		{"gauge", gosnmp.SnmpPDU{Type: gosnmp.Gauge32, Value: uint(29)}, 29, true},
		{"zero is a measurement", gosnmp.SnmpPDU{Type: gosnmp.Integer, Value: 0}, 0, true},
		{"above one hundred", gosnmp.SnmpPDU{Type: gosnmp.Integer, Value: 101}, 0, false},
		{"negative", gosnmp.SnmpPDU{Type: gosnmp.Integer, Value: -1}, 0, false},
		{"string", gosnmp.SnmpPDU{Type: gosnmp.OctetString, Value: "7"}, 0, false},
		{"no such object", gosnmp.SnmpPDU{Type: gosnmp.NoSuchObject}, 0, false},
	}
	for _, tt := range tests {
		got, ok := tpLinkPercent(tt.pdu)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: tpLinkPercent() = (%d, %v); want (%d, %v)", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}
