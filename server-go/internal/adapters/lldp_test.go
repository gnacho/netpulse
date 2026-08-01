// lldp_test.go — fixture realista de `lldpcli -f json show neighbors`
// (contrato C2): switch gestionado GS308E con mgmt-ip y caps Bridge, un AP
// anunciándose (mgmt-ip como string suelto), un equipo sin mgmt-ip ni
// descr de puerto, y formas degeneradas que NUNCA deben romper el parseo.
package adapters

import (
	"errors"
	"fmt"
	"testing"
)

// Fixture realista (lldpd 1.0.x en OpenWrt).
const lldpcliFixture = `{
  "lldp": {
    "interface": [
      {
        "name": "lan3",
        "via": "LLDP",
        "rid": "2",
        "age": "0 day, 00:01:12",
        "chassis": {
          "GS308E": {
            "id": {"type": "mac", "value": "28:c6:8e:1d:90:44"},
            "descr": "Netgear GS308E 8-Port Gigabit Smart Managed Plus Switch",
            "mgmt-ip": ["192.168.8.13", "fe80::2ac6:8eff:fe1d:9044"],
            "capability": [
              {"type": "Bridge", "enabled": true},
              {"type": "Router", "enabled": false}
            ]
          }
        },
        "port": {
          "id": {"type": "ifname", "value": "ge5"},
          "descr": "ge5",
          "ttl": "120"
        }
      },
      {
        "name": "lan1",
        "via": "LLDP",
        "rid": "1",
        "age": "0 day, 00:00:45",
        "chassis": {
          "EAP225": {
            "id": {"type": "mac", "value": "B0:4A:39:2E:77:10"},
            "descr": "TP-Link EAP225 (Outdoor)",
            "mgmt-ip": "192.168.8.4",
            "capability": [
              {"type": "Bridge", "enabled": true},
              {"type": "Wlan", "enabled": true}
            ]
          }
        },
        "port": {
          "id": {"type": "mac", "value": "B0:4A:39:2E:77:11"},
          "descr": "eth0",
          "ttl": "120"
        }
      },
      {
        "name": "lan2",
        "via": "LLDP",
        "rid": "3",
        "age": "0 day, 00:02:03",
        "chassis": {
          "mini-pc": {
            "id": {"type": "mac", "value": "3C:52:82:10:20:30"},
            "descr": "Debian GNU/Linux",
            "capability": [
              {"type": "Station", "enabled": true}
            ]
          }
        },
        "port": {
          "id": {"type": "ifname", "value": "eno1"},
          "ttl": "120"
        }
      }
    ]
  }
}`

func TestParseLldpNeighborsFixture(t *testing.T) {
	neighbors, err := parseLldpNeighbors([]byte(lldpcliFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 3 {
		t.Fatalf("neighbors: %d", len(neighbors))
	}
	// Switch GS308E en lan3: mgmt-ip array (se coge la primera), caps enabled
	sw := neighbors[0]
	if sw.Port != "lan3" || sw.Chassis != "GS308E" || sw.ChassisMac != "28:C6:8E:1D:90:44" {
		t.Fatalf("gs308e: %+v", sw)
	}
	if sw.Mgmt != "192.168.8.13" {
		t.Fatalf("mgmt: %q", sw.Mgmt)
	}
	if len(sw.Caps) != 1 || sw.Caps[0] != "Bridge" {
		t.Fatalf("caps (Router disabled no cuenta): %v", sw.Caps)
	}
	if sw.PortDesc != "ge5" {
		t.Fatalf("portDesc: %q", sw.PortDesc)
	}
	if info := sw.info(); info.Chassis != "GS308E" || info.Mgmt != "192.168.8.13" || info.Caps != "Bridge" || info.PortDesc != "ge5" {
		t.Fatalf("info: %+v", info)
	}
	// AP anunciándose: mgmt-ip como STRING suelto, dos caps enabled
	ap := neighbors[1]
	if ap.Port != "lan1" || ap.Chassis != "EAP225" || ap.ChassisMac != "B0:4A:39:2E:77:10" {
		t.Fatalf("ap: %+v", ap)
	}
	if ap.Mgmt != "192.168.8.4" {
		t.Fatalf("mgmt string suelto: %q", ap.Mgmt)
	}
	if len(ap.Caps) != 2 || ap.Caps[0] != "Bridge" || ap.Caps[1] != "Wlan" {
		t.Fatalf("caps ap: %v", ap.Caps)
	}
	if ap.info().Caps != "Bridge, Wlan" {
		t.Fatalf("caps unidas: %q", ap.info().Caps)
	}
	// Equipo sin mgmt-ip ni descr de puerto: PortDesc cae al id
	pc := neighbors[2]
	if pc.Port != "lan2" || pc.Mgmt != "" {
		t.Fatalf("mini-pc: %+v", pc)
	}
	if pc.PortDesc != "eno1" {
		t.Fatalf("portDesc fallback id: %q", pc.PortDesc)
	}
}

func TestParseLldpNeighborsDefensivo(t *testing.T) {
	// Raíz no JSON → error (el caller reintenta; NO es lldpd ausente)
	if _, err := parseLldpNeighbors([]byte("lldpd is not running")); err == nil {
		t.Fatal("no JSON debería dar error")
	}
	// Formas vacías → sin vecinos, sin error
	for _, in := range []string{`{}`, `{"lldp":{}}`, `{"lldp":{"interface":null}}`, `{"lldp":{"interface":[]}}`, `null`} {
		neighbors, err := parseLldpNeighbors([]byte(in))
		if err != nil || len(neighbors) != 0 {
			t.Fatalf("%s → %v %v", in, neighbors, err)
		}
	}
	// Forma mapa (lldpd viejo): {"interface": {"lan3": {...}}}
	mapForm := `{"lldp":{"interface":{"lan3":{"chassis":{"sw1":{"id":{"type":"mac","value":"AA:BB:CC:DD:EE:FF"}}}}}}}`
	neighbors, err := parseLldpNeighbors([]byte(mapForm))
	if err != nil || len(neighbors) != 1 {
		t.Fatalf("forma mapa: %v %v", neighbors, err)
	}
	if neighbors[0].Port != "lan3" || neighbors[0].ChassisMac != "AA:BB:CC:DD:EE:FF" || neighbors[0].Chassis != "sw1" {
		t.Fatalf("forma mapa parseada: %+v", neighbors[0])
	}
	// Tipos inesperados por todas partes: nunca panic, campos vacíos
	weird := `{"lldp":{"interface":[
		{"name":"lan1","chassis":"una cadena","port":123},
		{"name":"lan2","chassis":{"x":{"id":"no-objeto","mgmt-ip":42,"capability":"no-array"}},"port":{"id":{}}},
		42,
		null
	]}}`
	neighbors, err = parseLldpNeighbors([]byte(weird))
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 4 {
		t.Fatalf("entradas raras: %d", len(neighbors))
	}
	if neighbors[0].Port != "lan1" || neighbors[0].Chassis != "" || neighbors[0].PortDesc != "" {
		t.Fatalf("chassis cadena: %+v", neighbors[0])
	}
	if neighbors[1].Port != "lan2" || neighbors[1].ChassisMac != "" || neighbors[1].Mgmt != "" {
		t.Fatalf("subcampos raros: %+v", neighbors[1])
	}
	if neighbors[2].Port != "" || neighbors[2].Chassis != "" || neighbors[2].Caps != nil {
		t.Fatalf("entrada no-objeto: %+v", neighbors[2])
	}
	if neighbors[3].Port != "" || neighbors[3].ChassisMac != "" {
		t.Fatalf("entrada null: %+v", neighbors[3])
	}
	// interface con forma ni array ni mapa → sin datos, no error
	neighbors, err = parseLldpNeighbors([]byte(`{"lldp":{"interface":42}}`))
	if err != nil || len(neighbors) != 0 {
		t.Fatalf("interface escalar: %v %v", neighbors, err)
	}
}

func TestIsLldpUnavailable(t *testing.T) {
	// lldpcli no existe: exit 127 del shell (mensaje de ssh.ExitError envuelto)
	err127 := fmt.Errorf("ssh 192.168.8.2: %w", errors.New("Process exited with status 127"))
	if !isLldpUnavailable(err127) {
		t.Fatalf("exit 127: %v", err127)
	}
	if !isLldpUnavailable(errors.New("ssh h: ash: lldpcli: not found")) {
		t.Fatal("not found")
	}
	// Otros fallos NO son indisponibilidad cacheable
	for _, e := range []error{
		errors.New("ssh h: timeout (5s)"),
		errors.New("ssh h: Process exited with status 1"),
		nil,
	} {
		if isLldpUnavailable(e) {
			t.Fatalf("no debería ser unavailable: %v", e)
		}
	}
}
