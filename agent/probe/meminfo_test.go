package probe

import "testing"

func TestParseMeminfoCachedKB(t *testing.T) {
	fixture := `MemTotal:         414264 kB
MemFree:           45780 kB
MemAvailable:      88088 kB
Buffers:               0 kB
Cached:            90464 kB
SwapCached:            0 kB
SReclaimable:      14356 kB
`
	if got := parseMeminfoCachedKB(fixture); got != 90464 {
		t.Fatalf("Cached: got %d, esperado 90464", got)
	}
	if got := parseMeminfoCachedKB("MemTotal: 1 kB\n"); got != 0 {
		t.Fatalf("sin Cached debería dar 0, got %d", got)
	}
	if got := parseMeminfoCachedKB("Cached: 123 kB"); got != 123 {
		t.Fatalf("solo línea Cached: %d", got)
	}
}
