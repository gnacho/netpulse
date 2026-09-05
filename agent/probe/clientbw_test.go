// clientbw_test.go — parser de `nlbw -c json -g mac` (issue #551) y
// comportamiento de la sonda probeClientBw con runner fake.
package probe

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Fixture del formato real de nlbwmon (handle_json en client.c): agrupación
// por MAC (-g mac) emite columnas mac, conns, rx_bytes, rx_pkts, tx_bytes,
// tx_pkts en ese orden, con los contadores acumulados del periodo.
const nlbwFixture = `{"columns":["mac","conns","rx_bytes","rx_pkts","tx_bytes","tx_pkts"],"data":[
["aa:bb:cc:dd:ee:01", 34, 123456789, 98765, 111222333, 121314],
["aa:bb:cc:dd:ee:02", 2, 1000, 5, 2000, 9]
]}`

func TestParseNlbwJSONFixture(t *testing.T) {
	hosts, err := ParseNlbwJSON([]byte(nlbwFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("hosts: %d", len(hosts))
	}
	h := hosts["AA:BB:CC:DD:EE:01"] // normalizada a mayúsculas
	if h.RxBytes != 123456789 || h.TxBytes != 111222333 {
		t.Fatalf("contadores aa:bb:...:01: %+v", h)
	}
	if h2 := hosts["AA:BB:CC:DD:EE:02"]; h2.RxBytes != 1000 || h2.TxBytes != 2000 {
		t.Fatalf("contadores aa:bb:...:02: %+v", h2)
	}
}

func TestParseNlbwJSONColumnasPorNombre(t *testing.T) {
	// El parser mapea por NOMBRE de columna: orden distinto o columnas
	// extra no deben romper (p.ej. versiones del CLI que añadan campos).
	reordered := `{"columns":["conns","tx_bytes","mac","rx_bytes","foo"],"data":[
[7, 999, "aa:bb:cc:dd:ee:03", 555, "irrelevante"]
]}`
	hosts, err := ParseNlbwJSON([]byte(reordered))
	if err != nil {
		t.Fatalf("parse reordenado: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("hosts: %d", len(hosts))
	}
	if h := hosts["AA:BB:CC:DD:EE:03"]; h.RxBytes != 555 || h.TxBytes != 999 {
		t.Fatalf("contadores reordenado: %+v", h)
	}
}

func TestParseNlbwJSONDefensivo(t *testing.T) {
	// JSON no válido → error.
	if _, err := ParseNlbwJSON([]byte("nlbw: connect failed")); err == nil {
		t.Fatal("no JSON debería dar error")
	}
	// Sin datos → vacío, sin error.
	hosts, err := ParseNlbwJSON([]byte(`{"columns":["mac","rx_bytes"],"data":[]}`))
	if err != nil || len(hosts) != 0 {
		t.Fatalf("sin datos: %v %v", hosts, err)
	}
	// Sin columna mac → sin hosts, sin error (formato inesperado, no pánico).
	if hosts, err := ParseNlbwJSON([]byte(`{"columns":["ip"],"data":[["1.2.3.4"]]}`)); err != nil || hosts != nil {
		t.Fatalf("sin columna mac: %v %v", hosts, err)
	}
	// Filas con tipos raros: no pánico, se saltan.
	weird := `{"columns":["mac","rx_bytes","tx_bytes"],"data":[[null, 1, 2],["aa:bb:cc:dd:ee:04","no-num",null],42]}`
	hosts, err = ParseNlbwJSON([]byte(weird))
	if err != nil {
		t.Fatalf("filas raras: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("hosts de filas raras: %d", len(hosts))
	}
	if h := hosts["AA:BB:CC:DD:EE:04"]; h.RxBytes != 0 || h.TxBytes != 0 {
		t.Fatalf("contadores no numéricos: %+v", h)
	}
}

// TestProberClientBw: contrato de disponibilidad de la sonda (#551).
func TestProberClientBw(t *testing.T) {
	ctx := context.Background()

	// nlbw NO instalado: command -v falla → Available=false (el server usa
	// entonces hostapd bytes por estación).
	p := NewProber(fakeRunner{outs: map[string]string{}}, Options{})
	cb := p.probeClientBw(ctx)
	if cb == nil || cb.Available || cb.Hosts != nil {
		t.Fatalf("sin nlbw: %+v", cb)
	}

	// Instalado con datos: fixture completo.
	p = NewProber(fakeRunner{outs: map[string]string{
		CmdNlbwmonCheck: "/usr/sbin/nlbw\n",
		CmdNlbwmonJSON:  nlbwFixture,
	}}, Options{})
	cb = p.probeClientBw(ctx)
	if cb == nil || !cb.Available || len(cb.Hosts) != 2 {
		t.Fatalf("con datos: %+v", cb)
	}

	// Instalado, daemon caído: check OK pero nlbw sin salida →
	// Available=true con Hosts nil (sonda fallida, no "no instalado").
	p = NewProber(fakeRunner{outs: map[string]string{
		CmdNlbwmonCheck: "/usr/sbin/nlbw\n",
	}}, Options{})
	cb = p.probeClientBw(ctx)
	if cb == nil || !cb.Available || cb.Hosts != nil {
		t.Fatalf("daemon caído: %+v", cb)
	}

	// Instalado, cero hosts: data [] → vacío honesto, no nil.
	p = NewProber(fakeRunner{outs: map[string]string{
		CmdNlbwmonCheck: "/usr/sbin/nlbw\n",
		CmdNlbwmonJSON:  `{"columns":["mac","rx_bytes"],"data":[]}`,
	}}, Options{})
	cb = p.probeClientBw(ctx)
	if cb == nil || !cb.Available || cb.Hosts == nil || len(cb.Hosts) != 0 {
		t.Fatalf("cero hosts: %+v", cb)
	}
}

// TestPayloadClientBwWire: nil vs vacío viaja distinto en el JSON (contrato
// del anti-parpadeo del server). Sin omitempty en Hosts a propósito.
func TestPayloadClientBwWire(t *testing.T) {
	mustJSON := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	// Sección presente con hosts
	if s := mustJSON(PayloadData{ClientBw: &ClientBwData{Available: true, Hosts: map[string]NlbwCounter{"AA:BB:CC:DD:EE:01": {RxBytes: 1}}}}); !strings.Contains(s, `"hosts":{`) {
		t.Fatalf("wire con hosts: %s", s)
	}
	// Sección presente, sonda fallida: hosts null (no ausente)
	if s := mustJSON(PayloadData{ClientBw: &ClientBwData{Available: true}}); !strings.Contains(s, `"hosts":null`) {
		t.Fatalf("wire sonda fallida: %s", s)
	}
	// No instalado: available false, hosts null
	if s := mustJSON(PayloadData{ClientBw: &ClientBwData{Available: false}}); !strings.Contains(s, `"available":false`) {
		t.Fatalf("wire no instalado: %s", s)
	}
	// Sección ausente (push event-driven): nada de clientBw en el JSON
	if s := mustJSON(PayloadData{}); strings.Contains(s, "clientBw") {
		t.Fatalf("wire sin sección: %s", s)
	}
	// Disponible sin hosts (vacío honesto): "hosts":{}
	if s := mustJSON(PayloadData{ClientBw: &ClientBwData{Available: true, Hosts: map[string]NlbwCounter{}}}); !strings.Contains(s, `"hosts":{}`) {
		t.Fatalf("wire vacío honesto: %s", s)
	}
}
