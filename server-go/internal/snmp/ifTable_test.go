package snmp

import "testing"

// #1280: un 2.5 GE reporta ifSpeed=2500000000; la división entera lo
// truncaba a "2 Gbps". Los múltiplos exactos siguen sin decimales.
func TestSpeedStringQuarterGigMultiples(t *testing.T) {
	cases := []struct {
		bps  uint64
		want string
	}{
		{1_000_000_000, "1 Gbps"},
		{2_500_000_000, "2.5 Gbps"},
		{5_000_000_000, "5 Gbps"},
		{10_000_000_000, "10 Gbps"},
		{1_000_000, "1 Mbps"},
	}
	for _, c := range cases {
		p := PortStats{OperUp: true, SpeedBps: c.bps}
		if got := p.SpeedString(); got != c.want {
			t.Errorf("SpeedString(%d) = %q, want %q", c.bps, got, c.want)
		}
	}
}
