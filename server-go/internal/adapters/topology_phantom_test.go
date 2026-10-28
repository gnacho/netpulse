package adapters

// topology_phantom_test.go — #1372: guarda contra vecinos LLDP fantasma
// (switch intermedio que inunda la multicast reservada 01:80:c2:00:00:0e,
// no 802.1AB-compliant). Varios candidatos de flota llegan por el MISMO
// puerto raíz; la corroboración FDB por bridge-MAC decide.

import "testing"

const (
	macGw    = "94:83:C4:00:00:01"
	macRT2   = "22:22:22:22:22:02"
	macRT3   = "33:33:33:33:33:03"
	macSwCor = "44:44:44:44:44:04"
	macSwFar = "55:55:55:55:55:05"
)

// phantomPolled: rt4 oye en lan1 a los vecinos dados (en ese orden, como
// los devuelve lldpcli) y su FDB aprende al gateway por lan1 (root port).
func phantomPolled(neighbors []LldpNeighbor, fdb map[string]string) map[string]*routerPolled {
	return map[string]*routerPolled{
		"gw":     {cfg: RouterConfig{ID: "gw", IsGateway: true}, brMac: macGw},
		"rt2":    {cfg: RouterConfig{ID: "rt2", Type: "openwrt"}, brMac: macRT2},
		"rt3":    {cfg: RouterConfig{ID: "rt3", Type: "openwrt"}, brMac: macRT3},
		"swCore": {cfg: RouterConfig{ID: "swCore", Type: "managed-switch"}, brMac: macSwCor},
		"swFar":  {cfg: RouterConfig{ID: "swFar", Type: "managed-switch"}, brMac: macSwFar},
		"rt4": {cfg: RouterConfig{ID: "rt4", Type: "openwrt"}, brMac: "44:00:00:00:00:44",
			fdb:  fdb,
			lldp: neighbors,
		},
	}
}

// Phantom puro (sin tráfico directo: su MAC no está en la FDB) llega PRIMERO
// en la lista pero no debe ganar al candidato corroborado.
func TestPhantomFilteredByFdbCorroboration(t *testing.T) {
	neighbors := []LldpNeighbor{
		{Port: "lan1", Chassis: "rt3", ChassisMac: macRT3, PortDesc: "lan1"}, // phantom primero
		{Port: "lan1", Chassis: "rt2", ChassisMac: macRT2, PortDesc: "lan2"}, // peer real
	}
	fdb := map[string]string{
		macGw:  "lan1", // root port
		macRT2: "lan1", // rt2 corroborado (tráfico real vía lan1)
		// macRT3 ausente: phantom de 2º salto sin tráfico directo
	}
	out := fleetLldpEvidence(phantomPolled(neighbors, fdb), "gw")
	if out["rt4"].parent != "rt2" {
		t.Fatalf("phantom sin corroboración no debe ganar: parent=%q", out["rt4"].parent)
	}
	if out["rt4"].port != "lan2" {
		t.Fatalf("puerto del padre: %q", out["rt4"].port)
	}
}

// Dos switches gestionados oídos en el mismo puerto (cadena vista a través
// de flooding): gana el corroborado, no el primero de la lista.
func TestPhantomTwoSwitchesCorroboratedWins(t *testing.T) {
	neighbors := []LldpNeighbor{
		{Port: "lan1", Chassis: "swFar", ChassisMac: macSwFar, PortDesc: "ge9"},  // phantom primero
		{Port: "lan1", Chassis: "swCore", ChassisMac: macSwCor, PortDesc: "ge5"}, // peer real
	}
	fdb := map[string]string{
		macGw:    "lan1", // root port: el guard corre
		macSwCor: "lan1", // swCore corroborado
	}
	out := fleetLldpEvidence(phantomPolled(neighbors, fdb), "gw")
	if out["rt4"].parent != "swCore" {
		t.Fatalf("dos switches: gana el corroborado; parent=%q", out["rt4"].parent)
	}
}

// LAN plana: switch y AP corroborados a la vez en el root port → el switch
// gestionado manda entre los corroborados (hacia el core), sea cual sea el
// orden del scan.
func TestPhantomCorroboratedTiePrefersSwitch(t *testing.T) {
	neighbors := []LldpNeighbor{
		{Port: "lan1", Chassis: "rt3", ChassisMac: macRT3, PortDesc: "lan1"},     // AP primero
		{Port: "lan1", Chassis: "swCore", ChassisMac: macSwCor, PortDesc: "ge5"}, // switch
	}
	fdb := map[string]string{
		macGw:    "lan1",
		macRT3:   "lan1", // ambos corroborados
		macSwCor: "lan1",
	}
	out := fleetLldpEvidence(phantomPolled(neighbors, fdb), "gw")
	if out["rt4"].parent != "swCore" {
		t.Fatalf("empate corroborado: switch preferido; parent=%q", out["rt4"].parent)
	}
}
