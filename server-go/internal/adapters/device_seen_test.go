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

// #1145: los clientes del registro que ya no se ven en vivo salen como
// offline (ghosts) conservando su último nombre y seen; los que siguen
// vivos no se duplican.
func TestGhostDevicesOfflineRetention(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	l.noteDevicesSeen([]Device{
		{MAC: "aa:bb:cc:dd:ee:ff", Online: true, Hostname: "iphone-peter"},
		{MAC: "11:22:33:44:55:66", Online: true},
	}, 1000)

	// Solo uno sigue vivo: el otro sale como ghost con su nombre y seen.
	ghosts := l.ghostDevices([]Device{{MAC: "11:22:33:44:55:66", Online: true}})
	if len(ghosts) != 1 {
		t.Fatalf("ghosts = %d, want 1", len(ghosts))
	}
	g := ghosts[0]
	if g.MAC != "AA:BB:CC:DD:EE:FF" || g.Online {
		t.Fatalf("ghost = %+v, want offline AA:BB:CC:DD:EE:FF", g)
	}
	if g.Hostname != "iphone-peter" {
		t.Fatalf("hostname = %q, want iphone-peter", g.Hostname)
	}
	if g.FirstSeenMs != 1000 || g.LastSeenMs != 1000 {
		t.Fatalf("seen = %d/%d, want 1000/1000", g.FirstSeenMs, g.LastSeenMs)
	}

	// Los dos vivos: ningún ghost.
	if ghosts := l.ghostDevices([]Device{
		{MAC: "11:22:33:44:55:66", Online: true},
		{MAC: "AA:BB:CC:DD:EE:FF", Online: true},
	}); len(ghosts) != 0 {
		t.Fatalf("ghosts = %d, want 0", len(ghosts))
	}
}

// #1145: la caché gl-clients de GL.iNet emite MACs con guiones; el pase de
// normalización dedupe con la versión de dos puntos y enriquece la entrada.
func TestNormalizeDevicesDashedMAC(t *testing.T) {
	out := normalizeDevices([]Device{
		{MAC: "AA-BB-CC-DD-EE-FF", Online: false, Hostname: "watch"},
		{MAC: "aa:bb:cc:dd:ee:ff", Online: true, IP: "192.168.1.50"},
	})
	if len(out) != 1 {
		t.Fatalf("devices = %d, want 1 (dedup por MAC normalizada)", len(out))
	}
	if out[0].MAC != "AA:BB:CC:DD:EE:FF" || !out[0].Online {
		t.Fatalf("device = %+v, want online con MAC canonica", out[0])
	}
	if out[0].Hostname != "watch" || out[0].IP != "192.168.1.50" {
		t.Fatalf("merge = %+v, want hostname+IP combinados", out[0])
	}
}

// #1145: el nombre nuevo (lease) pisa al guardado solo si no es vacío.
func TestNoteDevicesSeenKeepsLastName(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	l.noteDevicesSeen([]Device{{MAC: "aa:bb:cc:dd:ee:ff", Online: true, Hostname: "iphone"}}, 1000)
	l.noteDevicesSeen([]Device{{MAC: "AA:BB:CC:DD:EE:FF", Online: true}}, 2000)

	ghosts := l.ghostDevices(nil)
	if len(ghosts) != 1 || ghosts[0].Hostname != "iphone" {
		t.Fatalf("ghosts = %+v, want 1 con hostname iphone", ghosts)
	}
}

// #1278: un cliente borrado a mano (lápida) no vuelve a alta aunque una
// fuente lo siga reportando online, y no sale en la lista de fantasmas.
// La re-alta manual (allowlist) quita la lápida.
func TestDeletedClientTombstone(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	mac := "12:77:BC:5A:5A:FA"
	if err := d.AddDeletedClient(mac); err != nil {
		t.Fatal(err)
	}
	devs := []Device{{MAC: mac, Online: true, Name: "iphone-viejo"}}
	l.noteDevicesSeen(devs, 1000)

	count := func() int {
		t.Helper()
		rows, err := d.Query("SELECT COUNT(*) FROM device_seen WHERE mac = ?", mac)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatal("sin fila de conteo")
		}
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if n := count(); n != 0 {
		t.Errorf("la MAC enterrada se dio de alta igual (%d filas)", n)
	}

	// Fantasmas: la MAC no sale aunque tenga registro previo.
	if _, err := d.Exec("INSERT INTO device_seen (mac, first_seen, last_seen) VALUES (?, 1, 2)", mac); err != nil {
		t.Fatal(err)
	}
	for _, g := range l.ghostDevices(nil) {
		if g.MAC == mac {
			t.Errorf("la MAC enterrada sale como fantasma: %+v", g)
		}
	}

	// Re-alta manual: la lápida se quita y vuelve a alta normal.
	if err := d.DeleteDeletedClient(mac); err != nil {
		t.Fatal(err)
	}
	l.noteDevicesSeen(devs, 3000)
	if n := count(); n != 1 {
		t.Errorf("tras quitar la lápida la MAC se da de alta (%d filas)", n)
	}
}

// #1292: la lápida filtra la LISTA FINAL - una estación stale del AP puede
// re-emitir la MAC sin que pase por device_seen.
func TestFilterTombstonedFinalList(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	if err := d.AddDeletedClient("12:77:BC:5A:5A:FA"); err != nil {
		t.Fatal(err)
	}
	devs := l.filterTombstoned([]Device{
		{MAC: "12:77:BC:5A:5A:FA", Online: true},
		{MAC: "AA:BB:CC:DD:EE:FF", Online: true},
	})
	if len(devs) != 1 || devs[0].MAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("filterTombstoned = %+v", devs)
	}
}
