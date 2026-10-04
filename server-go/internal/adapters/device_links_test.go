package adapters

import "testing"

// #1151: las MACs enlazadas se funden en la entrada canónica: online = OR,
// datos combinados, tráfico sumado y AliasMacs con las alias fundidas.
func TestMergeLinkedDevices(t *testing.T) {
	links := map[string]string{"AA:AA:AA:00:00:02": "BB:BB:BB:00:00:01"}
	devs := []Device{
		{MAC: "BB:BB:BB:00:00:01", Online: true, Hostname: "watch", IP: "192.168.1.50", TrafficMbps: 1.2},
		{MAC: "AA:AA:AA:00:00:02", Online: false, Manufacturer: "Google", TrafficMbps: 0.3},
	}
	out := mergeLinkedDevices(devs, links)
	if len(out) != 1 {
		t.Fatalf("devices = %d, want 1 (fundidas)", len(out))
	}
	g := out[0]
	if g.MAC != "BB:BB:BB:00:00:01" || !g.Online {
		t.Fatalf("canon = %+v, want MAC canonico online", g)
	}
	if len(g.AliasMacs) != 1 || g.AliasMacs[0] != "AA:AA:AA:00:00:02" {
		t.Fatalf("aliasMacs = %v", g.AliasMacs)
	}
	if g.TrafficMbps < 1.49 || g.TrafficMbps > 1.51 {
		t.Fatalf("traffic = %v, want 1.5 (suma)", g.TrafficMbps)
	}
	if g.Hostname != "watch" || g.Manufacturer != "Google" || g.IP != "192.168.1.50" {
		t.Fatalf("merge de datos = %+v", g)
	}
}

// El canónico puede no estar en la lista (solo se ve la alias): la salida
// conserva la identidad canónica y toma online/datos de la alias.
func TestMergeLinkedDevicesCanonicalNotSeen(t *testing.T) {
	links := map[string]string{"AA:AA:AA:00:00:02": "BB:BB:BB:00:00:01"}
	devs := []Device{{MAC: "AA:AA:AA:00:00:02", Online: true, IP: "192.168.1.51"}}
	out := mergeLinkedDevices(devs, links)
	if len(out) != 1 {
		t.Fatalf("devices = %d, want 1", len(out))
	}
	if out[0].MAC != "BB:BB:BB:00:00:01" || !out[0].Online || out[0].IP != "192.168.1.51" {
		t.Fatalf("canon = %+v, want identidad canonica con datos de la alias", out[0])
	}
}

// Sin enlaces la lista sale intacta.
func TestMergeLinkedDevicesNoLinks(t *testing.T) {
	devs := []Device{{MAC: "AA:AA:AA:00:00:01"}, {MAC: "BB:BB:BB:00:00:02"}}
	if out := mergeLinkedDevices(devs, map[string]string{}); len(out) != 2 {
		t.Fatalf("devices = %d, want 2", len(out))
	}
}

// Sugerencias: hostname+IP compartidos y no simultáneos sugieren; ya
// enlazadas o ambas online no.
func TestLinkSuggestions(t *testing.T) {
	devs := []Device{
		{MAC: "AA:AA:AA:00:00:01", Hostname: "watch", IP: "192.168.1.50", Online: true},
		{MAC: "AA:AA:AA:00:00:02", Hostname: "watch", IP: "192.168.1.50", Online: false},
		{MAC: "AA:AA:AA:00:00:03", Hostname: "other", IP: "192.168.1.60", Online: false},
	}
	if got := LinkSuggestions(devs, map[string]string{}); len(got) != 1 {
		t.Fatalf("suggestions = %v, want 1 par", got)
	}
	// Ambas online: no sugiere.
	both := []Device{devs[0], devs[1], devs[1]}
	both[1].Online = true
	both[2].Online = true
	if got := LinkSuggestions(both, map[string]string{}); len(got) != 0 {
		t.Fatalf("suggestions = %v, want 0 con las dos online", got)
	}
	// Ya enlazada: no sugiere.
	if got := LinkSuggestions(devs, map[string]string{"AA:AA:AA:00:00:02": "AA:AA:AA:00:00:01"}); len(got) != 0 {
		t.Fatalf("suggestions = %v, want 0 si ya enlazada", got)
	}
}
