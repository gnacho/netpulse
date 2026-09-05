package probe

import "testing"

const hostapdOut = `==AP==hostapd.phy0-ap0
{
	"freq": 2462,
	"clients": {
		"AA:BB:CC:DD:EE:FF": { "auth": true, "assoc": true, "authorized": true, "signal": -55 },
		"11:22:33:44:55:66": { "auth": true, "assoc": false, "authorized": false, "signal": -80 }
	}
}
==AP==hostapd.phy1-ap0
{
	"freq": 5180,
	"clients": {
		"AA:BB:CC:DD:EE:01": { "auth": true, "assoc": true, "authorized": true, "signal": -42 }
	}
}`

func TestParseHostapdClients(t *testing.T) {
	m := ParseHostapdClients(hostapdOut)
	// La estación sin assoc/authorized se filtra (paridad con assoclist).
	if len(m) != 2 {
		t.Fatalf("clientes: %+v (want 2, la pre-auth se filtra)", m)
	}
	c24 := m["AA:BB:CC:DD:EE:FF"]
	if c24.SignalDbm != -55 || c24.Band != "2.4 GHz" {
		t.Fatalf("2.4 GHz: %+v", c24)
	}
	c5 := m["AA:BB:CC:DD:EE:01"]
	if c5.SignalDbm != -42 || c5.Band != "5 GHz" {
		t.Fatalf("5 GHz: %+v", c5)
	}
}

func TestParseHostapdClientsBasura(t *testing.T) {
	if m := ParseHostapdClients("ubus: not found\n"); len(m) != 0 {
		t.Fatalf("output sin JSON debe dar vacío: %+v", m)
	}
	if m := ParseHostapdClients(""); len(m) != 0 {
		t.Fatalf("vacío: %+v", m)
	}
}

// #551: el parser de ubus hostapd get_clients debe conservar los contadores
// rx/tx por estación (bytes.rx/tx) que el driver expone, para que el server
// calcule el tráfico por cliente. Los drivers que NO los reportan dejan el
// campo a cero (compatibilidad).
func TestParseHostapdClientsBytes(t *testing.T) {
	withBytes := `==AP==hostapd.phy0-ap0
{
	"freq": 2437,
	"clients": {
		"AA:BB:CC:DD:EE:FF": {
			"auth": true, "assoc": true, "authorized": true, "signal": -55,
			"bytes": { "rx": 123456, "tx": 654321 }
		},
		"11:22:33:44:55:66": {
			"auth": true, "assoc": true, "authorized": true, "signal": -70
		}
	}
}`
	m := ParseHostapdClients(withBytes)
	if len(m) != 2 {
		t.Fatalf("clientes: %d (want 2)", len(m))
	}
	c := m["AA:BB:CC:DD:EE:FF"]
	if c.RxBytes != 123456 || c.TxBytes != 654321 {
		t.Fatalf("contadores no parseados: rx=%d tx=%d (want 123456/654321)", c.RxBytes, c.TxBytes)
	}
	if c2 := m["11:22:33:44:55:66"]; c2.RxBytes != 0 || c2.TxBytes != 0 {
		t.Fatalf("estación sin bytes debe quedar a 0: %+v", c2)
	}
}

func TestParseWirelessCombined(t *testing.T) {
	out := `R|2.437|6|HE20|22|2
C|AA:BB:CC:DD:EE:FF|-55|2.437
C|aa:bb:cc:dd:ee:02|-67|2.437
R|5.240|48|HE80|23|1
C|AA:BB:CC:DD:EE:01|-40|5.240
`
	clients, radios := ParseWirelessCombined(out)
	if len(clients) != 3 {
		t.Fatalf("clientes: %+v", clients)
	}
	if clients["AA:BB:CC:DD:EE:02"].SignalDbm != -67 || clients["AA:BB:CC:DD:EE:02"].Band != "2.4 GHz" {
		t.Fatalf("cliente lower: %+v", clients["AA:BB:CC:DD:EE:02"])
	}
	if len(radios) != 2 {
		t.Fatalf("radios: %+v", radios)
	}
	var r24, r5 *Radio
	for i := range radios {
		switch radios[i].Name {
		case "2.4 GHz":
			r24 = &radios[i]
		case "5 GHz":
			r5 = &radios[i]
		}
	}
	if r24 == nil || r24.Clients != 2 || r24.Channel != 6 || r24.WidthMhz != 20 {
		t.Fatalf("radio 2.4: %+v", r24)
	}
	if r5 == nil || r5.Clients != 1 || r5.WidthMhz != 80 {
		t.Fatalf("radio 5: %+v", r5)
	}
}

func TestParseWirelessCombinedVacio(t *testing.T) {
	clients, radios := ParseWirelessCombined("")
	if len(clients) != 0 || len(radios) != 0 {
		t.Fatalf("vacío: %+v %+v", clients, radios)
	}
}

// TestProberWirelessUbusPath: con ubus hostapd disponible, el sondeo usa SU
// output (el runner falso solo responde a comandos ubus; si el flujo llamara
// a iwinfo recibiría error y el test fallaría).
func TestProberWirelessUbusPath(t *testing.T) {
	run := fakeRunner{outs: map[string]string{
		CmdHostapdClients: `==AP==hostapd.phy0-ap0
{
	"freq": 2437,
	"clients": {
		"EC:71:DB:44:12:8A": { "auth": true, "assoc": true, "authorized": true, "signal": -58 }
	}
}`,
	}}
	p := NewProber(run, Options{})
	wd := p.probeWireless(nil, true)
	if wd == nil {
		t.Fatal("wireless nil en la vía ubus")
	}
	c := wd.Clients["EC:71:DB:44:12:8A"]
	if c.SignalDbm != -58 || c.Band != "2.4 GHz" {
		t.Fatalf("cliente ubus: %+v", c)
	}
	// Path de eventos: mismos clientes sin tocar iwinfo.
	wd2 := p.probeWireless(nil, false)
	if wd2 == nil || wd2.Clients["EC:71:DB:44:12:8A"].SignalDbm != -58 {
		t.Fatalf("path eventos: %+v", wd2)
	}
}

// TestProberWirelessRadiosCache: el path de eventos reutiliza las radios del
// último sondeo completo dentro del TTL.
func TestProberWirelessRadiosCache(t *testing.T) {
	run := fakeRunner{outs: map[string]string{
		CmdHostapdClients: `==AP==hostapd.phy1-ap0
{
	"freq": 5240,
	"clients": {
		"EC:71:DB:44:12:8B": { "auth": true, "assoc": true, "authorized": true, "signal": -40 }
	}
}`,
		CmdRadios: "5.24|48|HE80|23|1\n",
	}}
	p := NewProber(run, Options{})
	wd := p.probeWireless(nil, true)
	if len(wd.Radios) != 1 || wd.Radios[0].Channel != 48 {
		t.Fatalf("radios full: %+v", wd.Radios)
	}
	wd2 := p.probeWireless(nil, false)
	if len(wd2.Radios) != 1 || wd2.Radios[0].Channel != 48 {
		t.Fatalf("radios cacheadas en eventos: %+v", wd2.Radios)
	}
}
