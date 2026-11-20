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
		 VALUES ('AA:BB:CC:DD:EE:FF', 'tv', 'TV Salón', 'tv', '', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("seed override: %v", err)
	}
	polled := map[string]*routerPolled{
		"patio": {
			cfg:      RouterConfig{ID: "patio", Name: "Patio", Host: "192.168.1.2"},
			wireless: map[string]WirelessClient{mac: {SignalDbm: -55, Band: "5 GHz"}},
		},
	}
	devs := l.buildDevices(polled, "")
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

// Los mismos overrides deben verse en el cliente retenido offline (fantasma):
// sin esto el icono cambiaba al caer offline y editar icono/tipo estando
// offline no se reflejaba nunca.
func TestGhostDevicesApplyManualOverrides(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	l.noteDevicesSeen([]Device{
		{MAC: "aa:bb:cc:dd:ee:ff", Online: true, Hostname: "viewsonic-proj", Type: "desconocido"},
	}, 1000)
	now := time.Now().UnixMilli()
	if _, err := d.Exec(
		`INSERT INTO device_overrides (mac, icon, name, device_type, banned_bands, created_at, updated_at)
		 VALUES ('AA:BB:CC:DD:EE:FF', 'projector', '投影', 'tv', '', ?, ?)`, now, now); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	ghosts := l.ghostDevices(nil)
	if len(ghosts) != 1 {
		t.Fatalf("esperado 1 fantasma, got %d", len(ghosts))
	}
	g := ghosts[0]
	if g.Online {
		t.Fatalf("el fantasma debe ser offline")
	}
	if g.IconOverride != "projector" {
		t.Errorf("icon override no aplicado en offline: %+v", g)
	}
	if g.Name != "投影" || g.NameOverride != "投影" {
		t.Errorf("name override no aplicado en offline: %+v", g)
	}
	if g.Type != "tv" || g.TypeOverride != "tv" {
		t.Errorf("type override no aplicado en offline: %+v", g)
	}
}
