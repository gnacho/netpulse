package adapters

import "testing"

// #1791: el switch hoja detras de un switch gestionado no se inferia cuando
// ningun distnode "managed" casaba por MAC con el bridge address del switch.
// En su red el switch es SNMP (external+SNMP) y no hay LLDP que lo identifique,
// asi que la inferencia nunca arrancaba y los clientes quedaban en el fan-out.
func TestInferTopologyLeafTrasSwitchSnmpSinDistnode(t *testing.T) {
	gwMac := "94:83:C4:00:00:01"
	swMac := "78:55:36:00:00:02"
	polled := map[string]*routerPolled{
		"gw": {cfg: RouterConfig{ID: "gw", IsGateway: true}, brMac: gwMac, fdb: map[string]string{swMac: "lan2"}},
		"sw": {cfg: RouterConfig{ID: "sw", Type: "external", SnmpEnabled: true}, brMac: swMac, fdb: map[string]string{
			gwMac:               "1", // root port: el uplink no genera nodo
			"AA:BB:CC:00:00:11": "3",
			"AA:BB:CC:00:00:12": "3",
		}},
	}
	devices := []Device{
		dev("AA:BB:CC:00:00:11", "gw", "cable"),
		dev("AA:BB:CC:00:00:12", "gw", "cable"),
	}
	devices, dists := inferTopology(polled, devices)
	var leaf *DistributionNode
	for i := range dists {
		if dists[i].ID == "dist-sw-3" {
			leaf = &dists[i]
		}
	}
	if leaf == nil {
		t.Fatalf("no se infirio el switch hoja: %+v", dists)
	}
	if leaf.Kind != "inferred" || leaf.RouterID != "sw" || leaf.MacCount != 2 {
		t.Fatalf("leaf = %+v", *leaf)
	}
	// Sin distnode que case por MAC, el nodo cuelga de la unidad de flota.
	if leaf.Parent != "" {
		t.Fatalf("parent = %q, quiero vacio (cuelga de la unidad)", leaf.Parent)
	}
	for _, d := range devices {
		if d.AttachTo != "dist-sw-3" {
			t.Fatalf("%s attachTo = %q, quiero dist-sw-3", d.MAC, d.AttachTo)
		}
	}
}

// Mismo caso con el tipo managed-switch y sin LLDP: antes tambien fallaba,
// porque el ancla exigia un distnode managed con la chassis-MAC exacta.
func TestInferTopologyLeafTrasManagedSwitchSinLldp(t *testing.T) {
	gwMac := "94:83:C4:00:00:01"
	swMac := "78:55:36:00:00:02"
	polled := map[string]*routerPolled{
		"gw": {cfg: RouterConfig{ID: "gw", IsGateway: true}, brMac: gwMac, fdb: map[string]string{swMac: "lan2"}},
		"sw": {cfg: RouterConfig{ID: "sw", Type: "managed-switch"}, brMac: swMac, fdb: map[string]string{
			gwMac:               "1",
			"AA:BB:CC:00:00:11": "5",
			"AA:BB:CC:00:00:12": "5",
		}},
	}
	devices := []Device{
		dev("AA:BB:CC:00:00:11", "gw", "cable"),
		dev("AA:BB:CC:00:00:12", "gw", "cable"),
	}
	_, dists := inferTopology(polled, devices)
	found := false
	for _, dn := range dists {
		if dn.ID == "dist-sw-5" && dn.Kind == "inferred" {
			found = true
		}
	}
	if !found {
		t.Fatalf("managed-switch sin LLDP no genero el nodo hoja: %+v", dists)
	}
}

// El guardarraíl se mantiene: un AP con agente (AgentOnly) NO genera nodos
// hoja, porque su puerto multi-MAC es su uplink y no un switch local.
func TestInferTopologyApConAgenteNoGeneraHojas(t *testing.T) {
	gwMac := "94:83:C4:00:00:01"
	apMac := "8C:DE:F9:00:00:03"
	polled := map[string]*routerPolled{
		"gw": {cfg: RouterConfig{ID: "gw", IsGateway: true}, brMac: gwMac, fdb: map[string]string{apMac: "lan2"}},
		"ap": {cfg: RouterConfig{ID: "ap", Type: "openwrt", AgentOnly: true}, brMac: apMac, fdb: map[string]string{
			gwMac:               "lan1",
			"AA:BB:CC:00:00:11": "lan2",
			"AA:BB:CC:00:00:12": "lan2",
		}},
	}
	devices := []Device{
		dev("AA:BB:CC:00:00:11", "gw", "cable"),
		dev("AA:BB:CC:00:00:12", "gw", "cable"),
	}
	_, dists := inferTopology(polled, devices)
	for _, dn := range dists {
		if dn.Kind == "inferred" && dn.RouterID == "ap" {
			t.Fatalf("un AP con agente no debe generar nodos hoja: %+v", dn)
		}
	}
}
