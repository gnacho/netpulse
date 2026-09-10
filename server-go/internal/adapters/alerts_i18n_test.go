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
	})
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
