// override_builddevices_test.go — issue #797: los overrides manuales de
// dispositivo (icono, nombre visible y tipo) se aplican en buildDevices con
// lookup de MAC insensible al caso (los sondeos pueden traerla en mayúsculas
// y la tabla la guarda en minúsculas: por eso los iconos "desaparecían").
package adapters

import (
	"testing"
	"time"
)

func TestBuildDevicesNameTypeIconOverride(t *testing.T) {
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)
	mac := "AA:BB:CC:DD:EE:FF"
	now := time.Now().UnixMilli()
	_, err := d.Exec(
		`INSERT INTO device_overrides (mac, icon, name, device_type, banned_bands, created_at, updated_at)
		 VALUES ('aa:bb:cc:dd:ee:ff', 'tv', 'TV Salón', 'tv', '', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("seed override: %v", err)
	}
	polled := map[string]*routerPolled{
		"patio": {
			cfg:      RouterConfig{ID: "patio", Name: "Patio", Host: "192.168.1.2"},
			wireless: map[string]WirelessClient{mac: {SignalDbm: -55, Band: "5 GHz"}},
		},
	}
	devs := l.buildDevices(polled)
	if len(devs) != 1 {
		t.Fatalf("esperado 1 dispositivo, got %d", len(devs))
	}
	got := devs[0]
	if got.Name != "TV Salón" || got.NameOverride != "TV Salón" {
		t.Errorf("name override no aplicado: %+v", got)
	}
	if got.Type != "tv" || got.TypeOverride != "tv" {
		t.Errorf("type override no aplicado: %+v", got)
	}
	if got.IconOverride != "tv" {
		t.Errorf("icon override no aplicado: %+v", got)
	}
}
