// lldp_test.go — parser de `lldpcli -f json show neighbors` (fixture
// portado de adapters/lldp_test.go al mover el parser a probe, #489) y
// comportamiento de la sonda probeLldp con runner fake.
package probe

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Fixture realista (lldpd 1.0.x en OpenWrt): switch gestionado GS308E con
// mgmt-ip (array) y caps Bridge, un AP anunciándose (mgmt-ip como string
// suelto), un equipo sin mgmt-ip ni descr de puerto.
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
        "chassis": {
          "AP-patio": {
            "id": {"type": "mac", "value": "aa:bb:cc:dd:ee:ff"},
            "mgmt-ip": "192.168.8.20"
          }
        },
        "port": {"id": {"type": "local", "value": "0.2"}}
      },
      {
        "name": "lan4",
        "chassis": {
          " Equipo-sin-nombre ": {
            "id": {"type": "mac", "value": "11:22:33:44:55:66"}
          }
        }
      }
    ]
  }
}`

func TestParseLldpNeighborsFixture(t *testing.T) {
	nbs, err := ParseLldpNeighbors([]byte(lldpcliFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(nbs) != 3 {
		t.Fatalf("vecinos: %d", len(nbs))
	}
	// Orden estable: el array llega lan3, lan1, lan4
	got := nbs[0]
	if got.Port != "lan3" || got.Chassis != "GS308E" || got.ChassisMac != "28:C6:8E:1D:90:44" {
		t.Fatalf("n[0]: %+v", got)
	}
	if got.Mgmt != "192.168.8.13" {
		t.Fatalf("mgmt: %q", got.Mgmt)
	}
	if !reflect.DeepEqual(got.Caps, []string{"Bridge"}) {
		t.Fatalf("caps: %v", got.Caps)
	}
	if got.PortDesc != "ge5" {
		t.Fatalf("portDesc: %q", got.PortDesc)
	}
	// mgmt-ip como string suelto + port descr ausente → id del puerto
	if nbs[1].Mgmt != "192.168.8.20" || nbs[1].PortDesc != "0.2" {
		t.Fatalf("n[1]: %+v", nbs[1])
	}
	// sin mgmt-ip ni port
	if nbs[2].Mgmt != "" || nbs[2].PortDesc != "" {
		t.Fatalf("n[2]: %+v", nbs[2])
	}
}

func TestParseLldpNeighborsDefensivo(t *testing.T) {
	// Raíz no JSON → error (el caller reintenta; NO es lldpd ausente)
	if _, err := ParseLldpNeighbors([]byte("lldpd is not running")); err == nil {
		t.Fatal("no JSON debería dar error")
	}
	// Formas vacías → sin vecinos, sin error
	for _, in := range []string{`{}`, `{"lldp":{}}`, `{"lldp":{"interface":null}}`, `{"lldp":{"interface":[]}}`, `null`} {
		neighbors, err := ParseLldpNeighbors([]byte(in))
		if err != nil || len(neighbors) != 0 {
			t.Fatalf("%s → %v %v", in, neighbors, err)
		}
	}
	// Forma mapa (lldpd viejo): {"interface": {"lan3": {...}}}; la CLAVE es
	// el puerto local.
	mapForm := `{"lldp":{"interface":{"lan3":{"chassis":{"sw1":{"id":{"type":"mac","value":"AA:BB:CC:DD:EE:FF"}}}}}}}`
	neighbors, err := ParseLldpNeighbors([]byte(mapForm))
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
	neighbors, err = ParseLldpNeighbors([]byte(weird))
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
	neighbors, err = ParseLldpNeighbors([]byte(`{"lldp":{"interface":42}}`))
	if err != nil || len(neighbors) != 0 {
		t.Fatalf("interface escalar: %v %v", neighbors, err)
	}
}

// TestProberLldp: contrato de disponibilidad de la sonda (#489).
func TestProberLldp(t *testing.T) {
	ctx := context.Background()

	// lldpd NO instalado: command -v falla → Available=false.
	p := NewProber(fakeRunner{outs: map[string]string{}}, Options{})
	ld := p.probeLldp(ctx)
	if ld == nil || ld.Available || ld.Neighbors != nil {
		t.Fatalf("sin lldpd: %+v", ld)
	}

	// Instalado con vecinos: fixture completo.
	p = NewProber(fakeRunner{outs: map[string]string{
		CmdLldpCheck:     "/usr/sbin/lldpcli\n",
		CmdLldpNeighbors: lldpcliFixture,
	}}, Options{})
	ld = p.probeLldp(ctx)
	if ld == nil || !ld.Available || len(ld.Neighbors) != 3 {
		t.Fatalf("con vecinos: %+v", ld)
	}

	// Instalado, lldpd parado: check OK pero lldpcli sin salida →
	// Available=true con Neighbors nil (sonda fallida, no "no instalado").
	p = NewProber(fakeRunner{outs: map[string]string{
		CmdLldpCheck: "/usr/sbin/lldpcli\n",
	}}, Options{})
	ld = p.probeLldp(ctx)
	if ld == nil || !ld.Available || ld.Neighbors != nil {
		t.Fatalf("parado: %+v", ld)
	}

	// Instalado, cero vecinos: interface [] → vacío honesto, no nil.
	p = NewProber(fakeRunner{outs: map[string]string{
		CmdLldpCheck:     "/usr/sbin/lldpcli\n",
		CmdLldpNeighbors: `{"lldp":{"interface":[]}}`,
	}}, Options{})
	ld = p.probeLldp(ctx)
	if ld == nil || !ld.Available || ld.Neighbors == nil || len(ld.Neighbors) != 0 {
		t.Fatalf("cero vecinos: %+v", ld)
	}
}

// TestPayloadLldpWire: nil vs vacío viaja distinto en el JSON (contrato del
// anti-parpadeo del server). Sin omitempty en Neighbors a propósito.
func TestPayloadLldpWire(t *testing.T) {
	mustJSON := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	// Sección presente con vecinos
	if s := mustJSON(PayloadData{Lldp: &LldpData{Available: true, Neighbors: []LldpNeighbor{{Port: "lan3"}}}}); !strings.Contains(s, `"neighbors":[{`) {
		t.Fatalf("wire con vecinos: %s", s)
	}
	// Sección presente, sonda fallida: neighbors null (no ausente)
	if s := mustJSON(PayloadData{Lldp: &LldpData{Available: true}}); !strings.Contains(s, `"neighbors":null`) {
		t.Fatalf("wire sonda fallida: %s", s)
	}
	// Sección ausente (push event-driven): nada de lldp en el JSON
	if s := mustJSON(PayloadData{}); strings.Contains(s, "lldp") {
		t.Fatalf("wire sin sección: %s", s)
	}
	// Disponible sin vecinos (vacío honesto): "neighbors":[]
	if s := mustJSON(PayloadData{Lldp: &LldpData{Available: true, Neighbors: []LldpNeighbor{}}}); !strings.Contains(s, `"neighbors":[]`) {
		t.Fatalf("wire vacío honesto: %s", s)
	}
}
