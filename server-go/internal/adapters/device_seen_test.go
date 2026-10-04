package adapters

import "testing"

// #954: first_seen se fija en el alta y no se toca después; last_seen
// avanza en cada ciclo online. Los clientes offline no entran en la tabla.
func TestDeviceSeenFirstStableLastAdvances(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	devs := []Device{
		{MAC: "aa:bb:cc:dd:ee:ff", Online: true},
		{MAC: "11:22:33:44:55:66", Online: false},
	}
	l.noteDevicesSeen(devs, 1000)
	l.noteDevicesSeen(devs, 2000)

	out := []Device{
		{MAC: "AA:BB:CC:DD:EE:FF"}, // normalización: case-insensitive
		{MAC: "11:22:33:44:55:66"},
	}
	l.applyDeviceSeen(out)
	if out[0].FirstSeenMs != 1000 {
		t.Errorf("first_seen = %d; want 1000 (el alta no se reescribe)", out[0].FirstSeenMs)
	}
	if out[0].LastSeenMs != 2000 {
		t.Errorf("last_seen = %d; want 2000 (avanza cada ciclo online)", out[0].LastSeenMs)
	}
	if out[1].FirstSeenMs != 0 || out[1].LastSeenMs != 0 {
		t.Errorf("offline no debe tener fila: %+v", out[1])
	}
}

// #954: sin BD (demo/tests) ambos lados son no-op y los campos quedan a 0.
func TestDeviceSeenNilDB(t *testing.T) {
	l := &Live{}
	devs := []Device{{MAC: "aa:bb:cc:dd:ee:ff", Online: true}}
	l.noteDevicesSeen(devs, 1000) // no panic
	l.applyDeviceSeen(devs)
	if devs[0].FirstSeenMs != 0 || devs[0].LastSeenMs != 0 {
		t.Errorf("con db nil no se rellena: %+v", devs[0])
	}
}
