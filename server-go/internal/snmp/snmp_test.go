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
		{prefix + ".255.255.255.255.255.255", "FF:FF:FF:FF:FF:FF"},
		// #950: índice con prefijo VLAN (TP-Link Omada indexa la dot1d por
		// <vlan>.<mac>); la MAC son los últimos 6 octetos.
		{prefix + ".1.0.4.75.233.178.29", "00:04:4B:E9:B2:1D"},
		{prefix + ".1.2.3.4.5.6.7", "02:03:04:05:06:07"},
		{prefix + ".1.2.3.4.5", ""},
		{prefix + ".0.17.34.51.68.256", ""},
		{prefix + ".5.0.17.34.51.68.256", ""},
		{"other.0.17.34.51.68.85", ""},
	}
	for _, tt := range tests {
		got := extractMacFromOid(tt.name, prefix)
		if got != tt.want {
			t.Errorf("extractMacFromOid(%q) = %q; want %q", tt.name, got, tt.want)
		}
	}
}

// #661: el índice compuesto de la tabla dot1q es <vlan>.M.M.M.M.M.M; la MAC
// son los últimos 6 octetos (misma regla unificada de extractMacFromOid).
func TestExtractMacFromOidDot1q(t *testing.T) {
	prefix := OidDot1qTpFdbPort
	tests := []struct {
		name string
		want string
	}{
		{prefix + ".5.0.17.34.51.68.85", "00:11:22:33:44:55"},
		{prefix + ".0.17.34.51.68.85", "00:11:22:33:44:55"},
		{prefix + ".100.1.2.3.4.5.6", "01:02:03:04:05:06"},
		{prefix + ".5.0.17.34.51.68.256", ""},
		{prefix + ".1.2.3.4.5", ""},
		{"other.5.0.17.34.51.68.85", ""},
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
		{"2.5G highspeed", PortStats{OperUp: true, HighSpeedMbps: 2500}, "2.5 Gbps"}, // #1280: ya no trunca
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
		{PortStats{Alias: " ", Name: "gi1/0/8"}, "gi1/0/8"},
		{PortStats{Alias: "   "}, "port-0"},
		{PortStats{Alias: " Uplink ", Name: "eth0"}, "Uplink"},
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
	// #1077: un ifAlias de solo espacios se normaliza a vacío en el parseo.
	applyPortField(ps, OidIfAlias, gosnmp.SnmpPDU{Value: " "})
	if ps.Alias != "" {
		t.Errorf("Alias = %q; want empty for whitespace-only ifAlias", ps.Alias)
	}
	applyPortField(ps, OidIfAlias, gosnmp.SnmpPDU{Value: " Uplink "})
	if ps.Alias != "Uplink" {
		t.Errorf("Alias = %q; want Uplink (trimmed)", ps.Alias)
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

// #1125 (alternativa a la exclusión de la #1115): el ifTable conserva TODAS
// las interfaces y cada una se clasifica por familia; las no físicas (LAG,
// VLAN, bridge, túnel, virtual) van al grupo colapsado de la tarjeta en el
// frontend en vez de desaparecer. Verificado en un LGS310C real: po1-8
// (ieee8023adLag) y vlan1 (l3ipvlan) clasificados; un ifType no listado o no
// reportado (Type=0 por walk fallido) sigue siendo físico (defensa #1115).
func TestIfFamilyClassification(t *testing.T) {
	cases := []struct {
		typ    int
		family string
	}{
		{6, ""},          // ethernetCsmacd: física
		{117, ""},        // gigabitEthernet: física
		{161, "lag"},     // ieee8023adLag (LGS310C poN)
		{136, "vlan"},    // l3ipvlan (LGS310C vlan1)
		{135, "vlan"},    // l2vlan
		{209, "bridge"},  // bridge
		{131, "tunnel"},  // tunnel
		{24, "virtual"},  // softwareLoopback
		{53, "virtual"},  // propVirtual
		{0, ""},          // sin ifType: físico (walk fallido conserva todo)
		{9999, ""},       // desconocido: se trata como físico por exclusión
	}
	for _, c := range cases {
		if got := ifFamily(c.typ); got != c.family {
			t.Errorf("ifFamily(%d) = %q, esperaba %q", c.typ, got, c.family)
		}
	}
}
