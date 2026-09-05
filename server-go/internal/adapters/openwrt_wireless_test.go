package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// fakeWirelessRunner es un poolRunner que responde por comando exacto y
// registra las llamadas para poder afirmar qué se ejecutó (y qué NO).
type fakeWirelessRunner struct {
	calls     []string
	responses map[string]string
	errs      map[string]error
}

func (f *fakeWirelessRunner) Run(host, cmd string, timeout time.Duration) (string, error) {
	f.calls = append(f.calls, cmd)
	if err := f.errs[cmd]; err != nil {
		return "", err
	}
	return f.responses[cmd], nil
}

func (f *fakeWirelessRunner) RunCtx(ctx context.Context, host, cmd string, timeout time.Duration) (string, error) {
	return f.Run(host, cmd, timeout)
}

func (f *fakeWirelessRunner) ran(cmd string) bool {
	for _, c := range f.calls {
		if c == cmd {
			return true
		}
	}
	return false
}

const hostapdTwoAPOut = `==AP==hostapd.phy0-ap0
{
	"freq": 2437,
	"clients": {
		"AA:BB:CC:DD:EE:FF": { "auth": true, "assoc": true, "authorized": true, "signal": -55 }
	}
}
==AP==hostapd.phy1-ap0
{
	"freq": 5180,
	"clients": {
		"AA:BB:CC:DD:EE:01": { "auth": true, "assoc": true, "authorized": true, "signal": -42 }
	}
}`

// ubus-first (#373 en SSH): con objetos hostapd ubus, GetWirelessClients
// devuelve los clientes SIN ejecutar iwinfo (ni combined ni assoclist).
func TestGetWirelessClientsUbusFirstNoIwinfo(t *testing.T) {
	r := &fakeWirelessRunner{responses: map[string]string{probe.CmdHostapdClients: hostapdTwoAPOut}}
	c := &OpenWrtClient{Host: "192.168.1.9", pool: r}

	m := c.GetWirelessClients()
	if len(m) != 2 {
		t.Fatalf("clientes: %d (want 2)", len(m))
	}
	if r.ran(probe.CmdWirelessCombined) || r.ran(probe.CmdIwinfoAssoc) {
		t.Fatalf("ubus-first tocó iwinfo: %v", r.calls)
	}
}

// AP vacío con hostapd ubus: 0 clientes es un resultado REAL, no debe caer al
// fallback iwinfo en cada poll de 5 s (el bug de CPUs justas #368).
func TestGetWirelessClientsUbusEmptyAPNoIwinfo(t *testing.T) {
	empty := `==AP==hostapd.phy0-ap0
{
	"freq": 2437,
	"clients": {}
}`
	r := &fakeWirelessRunner{responses: map[string]string{probe.CmdHostapdClients: empty}}
	c := &OpenWrtClient{Host: "192.168.1.9", pool: r}

	m := c.GetWirelessClients()
	if len(m) != 0 {
		t.Fatalf("clientes: %+v (want 0)", m)
	}
	if r.ran(probe.CmdWirelessCombined) || r.ran(probe.CmdIwinfoAssoc) {
		t.Fatalf("AP vacío con ubus tocó iwinfo: %v", r.calls)
	}
}

// Sin objetos hostapd ubus (p.ej. wpad básico): cae al iwinfo combinado en UNA
// pasada, que además puebla la caché de radios para GetRadios.
func TestGetWirelessClientsFallbackCombined(t *testing.T) {
	combined := "R|2.437|6|HE20|22|2\nC|AA:BB:CC:DD:EE:FF|-55|2.437\n"
	r := &fakeWirelessRunner{responses: map[string]string{
		probe.CmdHostapdClients:   "",
		probe.CmdWirelessCombined: combined,
		probe.CmdIwinfoAssoc:      "AA:BB:CC:DD:EE:FF -55 2.4\n",
	}}
	c := &OpenWrtClient{Host: "192.168.1.9", pool: r}

	m := c.GetWirelessClients()
	if len(m) != 1 {
		t.Fatalf("clientes: %d (want 1)", len(m))
	}
	if r.ran(probe.CmdIwinfoAssoc) {
		t.Fatalf("el combinado bastó; no debe tocar el par legacy: %v", r.calls)
	}
	c.radiosMu.Lock()
	cached := c.radiosCache
	c.radiosMu.Unlock()
	if len(cached) != 1 {
		t.Fatalf("caché de radios no poblada por el combinado: %+v", cached)
	}
}

// Sin ubus ni combined: último recurso legacy (CmdIwinfoAssoc).
func TestGetWirelessClientsLegacyFallback(t *testing.T) {
	r := &fakeWirelessRunner{responses: map[string]string{
		probe.CmdHostapdClients:   "ubus: not found\n",
		probe.CmdWirelessCombined: "",
		probe.CmdIwinfoAssoc:      "AA:BB:CC:DD:EE:FF -55 5\n",
	}}
	c := &OpenWrtClient{Host: "192.168.1.9", pool: r}

	m := c.GetWirelessClients()
	if len(m) != 1 {
		t.Fatalf("clientes: %d (want 1)", len(m))
	}
	if !r.ran(probe.CmdIwinfoAssoc) {
		t.Fatalf("legacy no se usó: %v", r.calls)
	}
}

// GetRadios refresca al arranque (caché vacía) y sirve de la caché en las
// llamadas siguientes SIN volver a tocar el router (radiosTTL).
func TestGetRadiosCacheTTL(t *testing.T) {
	combined := "R|5.240|48|HE80|23|1\nC|AA:BB:CC:DD:EE:01|-40|5.240\n"
	r := &fakeWirelessRunner{responses: map[string]string{
		probe.CmdHostapdClients:   "",
		probe.CmdWirelessCombined: combined,
		probe.CmdRadios:           "5.24|48|HE80|23|1\n",
	}}
	c := &OpenWrtClient{Host: "192.168.1.9", pool: r}

	rs1 := c.GetRadios()
	if len(rs1) != 1 || rs1[0].Channel != 48 {
		t.Fatalf("primera llamada: %+v", rs1)
	}
	n1 := len(r.calls)
	rs2 := c.GetRadios()
	if len(rs2) != 1 {
		t.Fatalf("segunda llamada: %+v", rs2)
	}
	if len(r.calls) != n1 {
		t.Fatalf("la caché fresca no debe re-sondear: %v", r.calls)
	}

	// Caducada: al envejecer radiosAt, la siguiente llamada refresca.
	c.radiosMu.Lock()
	c.radiosAt = time.Now().Add(-radiosTTL - time.Second)
	c.radiosMu.Unlock()
	rs3 := c.GetRadios()
	if len(rs3) != 1 {
		t.Fatalf("refresco tras TTL: %+v", rs3)
	}
	if len(r.calls) <= n1 {
		t.Fatalf("TTL caducado debe refrescar: %v", r.calls)
	}
}
