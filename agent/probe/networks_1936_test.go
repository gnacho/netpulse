package probe

// #1936: la vista de redes (#1889) salia vacia en toda unidad. El filtro de
// CmdUciBridgeVlan tiraba las lineas de tipo de las secciones con nombre
// (`network.lan=interface`) y ParseUciNetworks solo acepta @type "interface",
// asi que descartaba todas. Las pruebas existentes no lo veian porque sus
// fixtures se escribian a mano con esas lineas dentro.
//
// Esta prueba corre el filtro REAL del comando sobre la salida REAL de
// `uci show network`, de modo que tocar el grep vuelve a romper algo.

import (
	"regexp"
	"strings"
	"testing"
)

// uciBridgeVlanFilter extrae el patron de grep del propio comando: la prueba
// tiene que validar el comando, no una copia del comando.
func uciBridgeVlanFilter(t *testing.T) *regexp.Regexp {
	t.Helper()
	const opener = `grep -E "`
	i := strings.Index(CmdUciBridgeVlan, opener)
	if i < 0 {
		t.Fatalf("CmdUciBridgeVlan ya no filtra con grep -E: %s", CmdUciBridgeVlan)
	}
	rest := CmdUciBridgeVlan[i+len(opener):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		t.Fatalf("patron de grep sin cerrar: %s", CmdUciBridgeVlan)
	}
	re, err := regexp.Compile(rest[:j])
	if err != nil {
		t.Fatalf("patron de grep invalido: %v", err)
	}
	return re
}

func applyUciFilter(re *regexp.Regexp, out string) string {
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		if re.MatchString(strings.TrimSpace(line)) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Captura real de una unidad OpenWrt 25.12.6 sin agente. `uci show network`
// imprime una linea de tipo por seccion, tambien para las que tienen nombre.
const uciShowNetworkCapture = `network.loopback=interface
network.loopback.device='lo'
network.loopback.proto='static'
network.loopback.ipaddr='127.0.0.1'
network.loopback.netmask='255.0.0.0'
network.globals=globals
network.globals.ula_prefix='fdc3:25a9:0cdb::/48'
network.@device[0]=device
network.@device[0].name='br-lan'
network.@device[0].type='bridge'
network.@device[0].ports='lan1 lan2 lan3 wan'
network.lan=interface
network.lan.device='br-lan'
network.lan.proto='dhcp'
`

func TestUciBridgeVlanCommandKeepsSectionTypes(t *testing.T) {
	filtered := applyUciFilter(uciBridgeVlanFilter(t), uciShowNetworkCapture)
	if !strings.Contains(filtered, "network.lan=interface") {
		t.Fatalf("el filtro tira la linea de tipo de la seccion lan:\n%s", filtered)
	}
	nets := ParseUciNetworks(filtered)
	if len(nets) != 2 {
		t.Fatalf("redes = %d (%+v), se esperaban 2", len(nets), nets)
	}
	byName := map[string]UciNetwork{}
	for _, n := range nets {
		byName[n.Name] = n
	}
	lan, ok := byName["lan"]
	if !ok {
		t.Fatalf("sin red lan: %+v", nets)
	}
	if lan.Device != "br-lan" || lan.Proto != "dhcp" {
		t.Fatalf("lan = %+v", lan)
	}
	if strings.Join(lan.Ports, " ") != "lan1 lan2 lan3 wan" {
		t.Fatalf("puertos de lan = %v", lan.Ports)
	}
	if lo, ok := byName["loopback"]; !ok || lo.Subnet != "127.0.0.1/8" {
		t.Fatalf("loopback = %+v", nets)
	}
}

// La VLAN leida de la configuracion toma el nombre de su red: sin la linea de
// tipo de la seccion, la fila salia con el nombre vacio.
func TestUciBridgeVlanCommandNamesTheVlan(t *testing.T) {
	const capture = `network.@device[0]=device
network.@device[0].name='br-lan'
network.@device[0].type='bridge'
network.@bridge-vlan[0]=bridge-vlan
network.@bridge-vlan[0].device='br-lan'
network.@bridge-vlan[0].vlan='20'
network.@bridge-vlan[0].ports='lan1:u* lan2:t'
network.guest=interface
network.guest.device='br-lan.20'
network.guest.proto='static'
network.guest.ipaddr='10.20.0.1'
network.guest.netmask='255.255.255.0'
`
	filtered := applyUciFilter(uciBridgeVlanFilter(t), capture)
	vlans := ParseUciNetworkVlans(filtered)
	var got *VlanEntry
	for _, group := range vlans {
		for _, v := range group.Vlans {
			if v.ID == 20 {
				vv := v
				got = &vv
			}
		}
	}
	if got == nil {
		t.Fatalf("sin VLAN 20: %+v", vlans)
	}
	if got.Name != "guest" {
		t.Fatalf("la VLAN 20 sale sin el nombre de su red: %+v", *got)
	}
}
