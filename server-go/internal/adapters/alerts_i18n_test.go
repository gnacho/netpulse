// alerts_i18n_test.go — tipado de alertas para traducción client-side (#671).
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// TestUnknownDeviceAlertCarriesType: la alerta de dispositivo desconocido
// lleva el slug del tipo y las vars (mac, router) para que el frontend
// traduzca título/descripción/hint en el idioma del usuario.
func TestUnknownDeviceAlertCarriesType(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Name: "Flint 2", IsGateway: true},
	}, nil)
	l.emitUnknownDevice(Device{
		ID: "aa-bb-cc-dd-ee-ff", MAC: "AA:BB:CC:DD:EE:FF", Name: "AA:BB:CC:DD:EE:FF",
		RouterID: "gateway", Online: true,
	}, nil, nil)
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("esperado 1 alerta, got %d", len(list))
	}
	ev := list[0]
	if ev.Type != alerts.HintUnknownDevice {
		t.Fatalf("Type=%q (want %q)", ev.Type, alerts.HintUnknownDevice)
	}
	if ev.Vars["mac"] != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("Vars mac=%q", ev.Vars["mac"])
	}
	if ev.Vars["router"] != "Flint 2" {
		t.Fatalf("Vars router=%q (want nombre visible, no el id)", ev.Vars["router"])
	}
	// Los literales siguen viajando como fallback.
	if ev.Title == "" || ev.Description == "" || ev.Hint == "" {
		t.Fatalf("los literales de fallback no deben vaciarse: %+v", ev)
	}
}

// TestUnknownDeviceAlertCarriesLocation: the alert has to say where the
// device is, not just its MAC — the IP, the box it hangs off and the port on
// that box, so it can be found without hunting through the client list.
func TestUnknownDeviceAlertCarriesLocation(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Name: "Gateway", IsGateway: true},
	}, nil)
	d := Device{
		ID: "02-00-00-00-00-31", MAC: "02:00:00:00:00:31", Name: "02:00:00:00:00:31",
		IP: "192.0.2.31", RouterID: "gateway", Online: true, Band: "cable",
		AttachTo: "sw-desk", Port: "7", PortLabel: "Desk 7",
	}
	// The switch hangs off router port 3; the client is learnt on the switch's
	// own port 7, so that one is the client's.
	dists := []DistributionNode{{ID: "sw-desk", Kind: "managed", Name: "Desk switch", RouterID: "gateway", Port: "3"}}
	l.emitUnknownDevice(d, []Device{d}, dists)
	ev := l.engine.List()[0]
	for k, want := range map[string]string{
		"ip":    "192.0.2.31",
		"where": "Desk switch",
		"port":  "Desk 7", // the friendly label wins over the raw port number
		"band":  "cable",
	} {
		if ev.Vars[k] != want {
			t.Fatalf("Vars[%q]=%q (want %q)", k, ev.Vars[k], want)
		}
	}
}

// TestUnknownDeviceLocationFallsBackToRouter: a wireless client with nothing
// between it and the router still gets a "where" — the router's visible name.
func TestUnknownDeviceLocationFallsBackToRouter(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Name: "Gateway", IsGateway: true},
	}, nil)
	d := Device{
		MAC: "02:00:00:00:00:32", Name: "02:00:00:00:00:32",
		RouterID: "gateway", Online: true, Band: "5 GHz",
	}
	l.emitUnknownDevice(d, []Device{d}, nil)
	ev := l.engine.List()[0]
	if ev.Vars["where"] != "Gateway" {
		t.Fatalf("Vars[where]=%q (want the router name)", ev.Vars["where"])
	}
	if _, ok := ev.Vars["ip"]; ok {
		t.Fatalf("an unknown IP must be absent, not empty: %+v", ev.Vars)
	}
}

// TestUnknownDeviceDropsUplinkPort: a client behind a switch is learnt on the
// router port the switch hangs off. That port is the uplink, not the socket
// the client is in, so the alert must not name it.
func TestUnknownDeviceDropsUplinkPort(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Name: "Gateway", IsGateway: true},
	}, nil)
	d := Device{
		MAC: "02:00:00:00:00:33", Name: "02:00:00:00:00:33", IP: "192.0.2.33",
		RouterID: "gateway", Online: true, Band: "cable",
		AttachTo: "sw-desk", Port: "3",
	}
	dists := []DistributionNode{{ID: "sw-desk", Kind: "managed", Name: "Desk switch", RouterID: "gateway", Port: "3"}}
	l.emitUnknownDevice(d, []Device{d}, dists)
	ev := l.engine.List()[0]
	if ev.Vars["where"] != "Desk switch" {
		t.Fatalf("Vars[where]=%q", ev.Vars["where"])
	}
	if p, ok := ev.Vars["port"]; ok {
		t.Fatalf("the uplink port must not be shown as the client's: %q", p)
	}
}
