// topo_semantics_hub_chain_2124_test.go — #2124: un device-hub colgado de otro
// device-hub emitía su cable dos veces (una por el bucle de hijos de hubs y
// otra por el bucle del cable propio del hub). El mapa no debe recibir un
// enlace repetido para ninguna cadena de hubs.
package adapters

import "testing"

func TestTopoSemanticsNestedHubChainHasNoDuplicateLinks(t *testing.T) {
	routers := []Router{{ID: "unit", Name: "unit", RoleBadge: "Principal", Status: "online"}}
	devices := []Device{
		{ID: "A", MAC: "AA:BB:CC:00:00:0A", RouterID: "unit", Band: "cable", Port: "lan1", AttachTo: "unit", Online: true},
		{ID: "B", MAC: "AA:BB:CC:00:00:0B", RouterID: "unit", Band: "cable", AttachTo: "A", Online: true},
		{ID: "C", MAC: "AA:BB:CC:00:00:0C", RouterID: "unit", Band: "cable", AttachTo: "B", Online: true},
		{ID: "D", MAC: "AA:BB:CC:00:00:0D", RouterID: "unit", Band: "cable", AttachTo: "C", Online: true},
	}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", nil, nil, nil)

	seen := map[string]int{}
	for _, l := range sem.Links {
		seen[l.From+"|"+l.To+"|"+l.Kind]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("enlace duplicado %s x%d: %+v", k, n, sem.Links)
		}
	}
	// La cadena entera existe: unit→A, A→B, B→C, C→D.
	for _, want := range [][2]string{{"unit", "A"}, {"A", "B"}, {"B", "C"}, {"C", "D"}} {
		found := false
		for _, l := range sem.Links {
			if l.From == want[0] && l.To == want[1] && l.Kind == "wired" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("falta el cable %s→%s en %+v", want[0], want[1], sem.Links)
		}
	}
}
