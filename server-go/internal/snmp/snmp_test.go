package snmp

import (
	"math/big"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestTrailingIndex(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   int
	}{
		{".1.3.6.1.2.1.2.2.1.1.5", OidIfIndex, 5},
		{".1.3.6.1.2.1.2.2.1.1.42", OidIfIndex, 42},
		{".1.3.6.1.2.1.2.2.1.1", OidIfIndex, 0},
		{".1.3.6.1.2.1.31.1.1.1.1.7", OidIfName, 7},
		{".9.9.9", OidIfIndex, 0},
	}
	for _, tt := range tests {
		got := trailingIndex(tt.name, tt.prefix)
		if got != tt.want {
			t.Errorf("trailingIndex(%q, %q) = %d; want %d", tt.name, tt.prefix, got, tt.want)
		}
	}
}

func TestExtractMacFromOid(t *testing.T) {
	prefix := OidDot1dTpFdbPort
	tests := []struct {
		name string
		want string
	}{
		{prefix + ".0.17.34.51.68.85", "00:11:22:33:44:55"},
		{prefix + ".255.255.255.255.255.255", "ff:ff:ff:ff:ff:ff"},
		{prefix + ".1.2.3.4.5", ""},
		{prefix + ".1.2.3.4.5.6.7", ""},
		{prefix + ".0.17.34.51.68.256", ""},
		{"other.0.17.34.51.68.85", ""},
	}
	for _, tt := range tests {
		got := extractMacFromOid(tt.name, prefix)
		if got != tt.want {
			t.Errorf("extractMacFromOid(%q) = %q; want %q", tt.name, got, tt.want)
		}
	}
}

func TestPortStatsSpeedString(t *testing.T) {
	tests := []struct {
		name string
		ps   PortStats
		want string
	}{
		{"down", PortStats{OperUp: false}, ""},
		{"1G highspeed", PortStats{OperUp: true, HighSpeedMbps: 1000}, "1 Gbps"},
		{"10G highspeed", PortStats{OperUp: true, HighSpeedMbps: 10000}, "10 Gbps"},
		{"100M ifSpeed", PortStats{OperUp: true, SpeedBps: 100_000_000}, "100 Mbps"},
		{"2.5G highspeed", PortStats{OperUp: true, HighSpeedMbps: 2500}, "2 Gbps"},
		{"zero speed", PortStats{OperUp: true}, ""},
	}
	for _, tt := range tests {
		got := tt.ps.SpeedString()
		if got != tt.want {
			t.Errorf("SpeedString(%s) = %q; want %q", tt.name, got, tt.want)
		}
	}
}

func TestPortStatsDisplayName(t *testing.T) {
	tests := []struct {
		ps   PortStats
		want string
	}{
		{PortStats{Alias: "Uplink"}, "Uplink"},
		{PortStats{Name: "eth0"}, "eth0"},
		{PortStats{Descr: "GigabitEthernet0/1"}, "GigabitEthernet0/1"},
		{PortStats{Index: 5}, "port-5"},
		{PortStats{Alias: "Uplink", Name: "eth0"}, "Uplink"},
	}
	for _, tt := range tests {
		got := tt.ps.DisplayName()
		if got != tt.want {
			t.Errorf("DisplayName(%+v) = %q; want %q", tt.ps, got, tt.want)
		}
	}
}

func TestApplyPortField(t *testing.T) {
	ps := &PortStats{Index: 1}
	applyPortField(ps, OidIfOperStatus, gosnmp.SnmpPDU{Value: 1})
	if !ps.OperUp {
		t.Error("expected OperUp=true for value 1")
	}
	applyPortField(ps, OidIfOperStatus, gosnmp.SnmpPDU{Value: 2})
	if ps.OperUp {
		t.Error("expected OperUp=false for value 2")
	}
	applyPortField(ps, OidIfSpeed, gosnmp.SnmpPDU{Value: uint(1000000000)})
	if ps.SpeedBps != 1_000_000_000 {
		t.Errorf("SpeedBps = %d; want 1000000000", ps.SpeedBps)
	}
	applyPortField(ps, OidIfName, gosnmp.SnmpPDU{Value: "eth0"})
	if ps.Name != "eth0" {
		t.Errorf("Name = %q; want eth0", ps.Name)
	}
	applyPortField(ps, OidIfHighSpeed, gosnmp.SnmpPDU{Value: uint(10000)})
	if ps.HighSpeedMbps != 10000 {
		t.Errorf("HighSpeedMbps = %d; want 10000", ps.HighSpeedMbps)
	}
	applyPortField(ps, OidIfInOctets, gosnmp.SnmpPDU{Value: big.NewInt(123456789)})
	if ps.RxBytes != 123456789 {
		t.Errorf("RxBytes = %d; want 123456789", ps.RxBytes)
	}
}

func TestSystemInfoParsing(t *testing.T) {
	pdus := []gosnmp.SnmpPDU{
		{Name: OidSysDescr, Value: "Linux switch 5.15"},
		{Name: OidSysName, Value: "core-switch"},
		{Name: OidSysUpTime, Value: uint(360000)},
	}
	info := &SystemInfo{}
	for _, v := range pdus {
		switch v.Name {
		case OidSysDescr:
			info.Descr = stringVal(v)
		case OidSysName:
			info.Name = stringVal(v)
		case OidSysUpTime:
			info.UpTimeSec = uint64Val(v) / 100
		}
	}
	if info.Descr != "Linux switch 5.15" {
		t.Errorf("Descr = %q", info.Descr)
	}
	if info.Name != "core-switch" {
		t.Errorf("Name = %q", info.Name)
	}
	if info.UpTimeSec != 3600 {
		t.Errorf("UpTimeSec = %d; want 3600", info.UpTimeSec)
	}
}

func TestStringVal(t *testing.T) {
	if got := stringVal(gosnmp.SnmpPDU{Value: "hello", Type: gosnmp.OctetString}); got != "hello" {
		t.Errorf("stringVal string = %q", got)
	}
	if got := stringVal(gosnmp.SnmpPDU{Value: []byte("bytes"), Type: gosnmp.OctetString}); got != "bytes" {
		t.Errorf("stringVal bytes = %q", got)
	}
	if got := stringVal(gosnmp.SnmpPDU{Type: gosnmp.NoSuchObject}); got != "" {
		t.Errorf("stringVal NoSuchObject = %q", got)
	}
}

func TestUint64Val(t *testing.T) {
	if got := uint64Val(gosnmp.SnmpPDU{Value: uint(42), Type: gosnmp.Gauge32}); got != 42 {
		t.Errorf("uint64Val = %d", got)
	}
	if got := uint64Val(gosnmp.SnmpPDU{Value: big.NewInt(999999), Type: gosnmp.Counter64}); got != 999999 {
		t.Errorf("uint64Val big = %d", got)
	}
	if got := uint64Val(gosnmp.SnmpPDU{Type: gosnmp.NoSuchInstance}); got != 0 {
		t.Errorf("uint64Val NoSuchInstance = %d", got)
	}
}

func TestSortByIndex(t *testing.T) {
	ps := []PortStats{{Index: 3}, {Index: 1}, {Index: 2}}
	sortByIndex(ps)
	for i, p := range ps {
		if p.Index != i+1 {
			t.Errorf("sortByIndex: ps[%d].Index = %d; want %d", i, p.Index, i+1)
		}
	}
}
