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

// #1307: el purge de MACs de flota (#1278) también debe cubrir los BSSID de
// las radios (ap1-phy1-mac-addr...): los AP se ven a sí mismos como
// estaciones y esas MACs reaparecen como clientes fantasma igual que la MAC
// base y la de bridge.
func TestPurgeFleetSeenIncludesRadioBSSIDs(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d, lastPolled: map[string]*routerPolled{
		"ap1": {brMac: "aa:aa:aa:aa:aa:01", radios: []Radio{
			{Name: "2.4 GHz", BSSID: "aa:aa:aa:aa:aa:02"},
			{Name: "5 GHz", BSSID: "AA-AA-AA-AA-AA-03"}, // guiones: normaliza
			{Name: "5 GHz", BSSID: ""},                  // sin BSSID: no-op
		}},
	}}
	// El registro guarda MACs normalizadas (normSeenMAC en el alta): mayúsculas
	// y dos puntos.
	purged := []string{"AA:AA:AA:AA:AA:01", "AA:AA:AA:AA:AA:02", "AA:AA:AA:AA:AA:03"}
	kept := "BB:BB:BB:BB:BB:BB"
	for _, m := range append(append([]string{}, purged...), kept) {
		if _, err := d.Exec("INSERT INTO device_seen (mac, first_seen, last_seen) VALUES (?, 1, 2)", m); err != nil {
			t.Fatalf("seed %s: %v", m, err)
		}
	}
	l.purgeFleetSeen()
	for _, m := range purged {
		var n int
		if err := d.QueryRow("SELECT COUNT(*) FROM device_seen WHERE mac = ?", m).Scan(&n); err != nil || n != 0 {
			t.Errorf("mac %s debería estar purgada (n=%d, err=%v)", m, n, err)
		}
	}
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM device_seen WHERE mac = ?", kept).Scan(&n); err != nil || n != 1 {
		t.Errorf("mac ajena %s no debe tocarse (n=%d, err=%v)", kept, n, err)
	}
}

// #1307: la fuente viva re-emite las MACs de flota cada ciclo; el purge del
// registro no basta, la LISTA final debe filtrarlas igual que las lápidas.
func TestFilterTombstonedAlsoFiltersFleetMACs(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d, lastPolled: map[string]*routerPolled{
		"ap1": {brMac: "aa:aa:aa:aa:aa:01", radios: []Radio{{Name: "2.4 GHz", BSSID: "aa:aa:aa:aa:aa:02"}}},
	}}
	devs := []Device{
		{MAC: "AA:AA:AA:AA:AA:01"}, // bridge
		{MAC: "aa:aa:aa:aa:aa:02"}, // BSSID radio (case-insensitive)
		{MAC: "cc:cc:cc:cc:cc:cc"}, // cliente real
	}
	out := l.filterTombstoned(devs)
	if len(out) != 1 || out[0].MAC != "cc:cc:cc:cc:cc:cc" {
		t.Fatalf("filterTombstoned = %+v, want solo el cliente real", out)
	}
}

// #1315: cuando el lease expira la fuente viva deja de dar nombre/IP (Name
// vuelve a ser la MAC, IP vacía) pero el cliente sigue listado vía
// estación/ARP. applyDeviceSeen debe restaurar el último conocido del
// registro (#1145), como ya hacían los ghosts.
func TestApplyDeviceSeenRestoresNameAndIP(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	l.noteDevicesSeen([]Device{
		{MAC: "aa:bb:cc:dd:ee:ff", Online: true, Hostname: "iphone-peter", IP: "192.168.1.50"},
	}, 1000)

	// Mismo cliente en el ciclo vivo SIN nombre ni IP (lease expirado).
	out := []Device{{MAC: "AA:BB:CC:DD:EE:FF", Name: "AA:BB:CC:DD:EE:FF", Online: false}}
	l.applyDeviceSeen(out)
	if out[0].Name != "iphone-peter" {
		t.Errorf("Name = %q, want iphone-peter (retenido del registro)", out[0].Name)
	}
	if out[0].Hostname != "iphone-peter" {
		t.Errorf("Hostname = %q, want iphone-peter", out[0].Hostname)
	}
	if out[0].IP != "192.168.1.50" {
		t.Errorf("IP = %q, want 192.168.1.50", out[0].IP)
	}

	// Un nombre real (alias/override) NO se pisa con el del registro.
	aliased := []Device{{MAC: "AA:BB:CC:DD:EE:FF", Name: "telefono-ana", Online: false}}
	l.applyDeviceSeen(aliased)
	if aliased[0].Name != "telefono-ana" {
		t.Errorf("Name = %q, want telefono-ana (el alias manda)", aliased[0].Name)
	}
}

// #1315 (sub-bug a, MACs enlazadas): con alias -> canónico, al expirar el
// lease el merge deja como nombre del canónico la MAC de una alias (la
// clasificación/restore antigua solo reconocía la propia MAC). El nombre
// vivo que es una MAC de cualquier interfaz (isMACLike) se restaura desde
// el registro y el server nunca emite esa MAC como hostname.
func TestApplyDeviceSeenRestoresLinkedAliasMACName(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	l.noteDevicesSeen([]Device{
		{MAC: "cc:cc:cc:00:00:01", Online: true, Hostname: "galaxy-s9", Type: "movil"},
	}, 1000)

	// El canónico llega vivo con la MAC de una alias como nombre (merge de
	// #1151 cuando el primario no tiene hostname).
	out := []Device{{MAC: "CC:CC:CC:00:00:01", Name: "AA:AA:AA:00:00:02", Online: true, Type: "desconocido"}}
	l.applyDeviceSeen(out)
	if out[0].Name != "galaxy-s9" {
		t.Errorf("Name = %q, want galaxy-s9 (la MAC de la alias no es un nombre)", out[0].Name)
	}
	if out[0].Hostname != "galaxy-s9" {
		t.Errorf("Hostname = %q, want galaxy-s9", out[0].Hostname)
	}
}

// #1315 (sub-bug a): noteDevicesSeen nunca persiste una MAC como nombre. El
// caso linked es el que contaminaba el registro: el nombre fusionado era la
// MAC de una alias (distinta de la MAC del dispositivo) y el guard antiguo
// (!= MAC propia, case-insensitive) la dejaba pasar.
func TestNoteDevicesSeenSkipsMACLikeNames(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	l.noteDevicesSeen([]Device{
		{MAC: "cc:cc:cc:00:00:01", Online: true, Name: "cc:cc:cc:00:00:01"},           // MAC propia
		{MAC: "dd:dd:dd:00:00:01", Online: true, Name: "AA:AA:AA:00:00:02"},           // MAC de otra interfaz (linked)
		{MAC: "ee:ee:ee:00:00:01", Online: true, Name: "EE-EE-EE-00-00-01"},           // MAC con guiones
		{MAC: "ff:ff:ff:00:00:01", Online: true, Name: "iphone-peter", Type: "movil"}, // nombre real
	}, 1000)

	ghosts := l.ghostDevices(nil)
	byMAC := map[string]Device{}
	for _, g := range ghosts {
		byMAC[g.MAC] = g
	}
	for _, mac := range []string{"CC:CC:CC:00:00:01", "DD:DD:DD:00:00:01", "EE:EE:EE:00:00:01"} {
		if g, ok := byMAC[mac]; !ok || g.Hostname != "" || g.Name != "" {
			t.Errorf("registro de %s = %+v, want sin nombre (una MAC no es nombre)", mac, g)
		}
	}
	if g, ok := byMAC["FF:FF:FF:00:00:01"]; !ok || g.Hostname != "iphone-peter" {
		t.Errorf("registro de FF:FF:FF:00:00:01 = %+v, want iphone-peter", g)
	}
}

// #1315 (sub-bug a): sanidad de BD vieja. Los ciclos anteriores al fix
// pudieron persistir una MAC como nombre; al leer, seenByMac lo ignora y el
// server deja de emitirla como hostname en vez de restaurarla.
func TestApplyDeviceSeenIgnoresStoredMACName(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	if _, err := d.Exec("INSERT INTO device_seen (mac, first_seen, last_seen, name) VALUES ('CC:CC:CC:00:00:01', 1, 2, 'AA-BB-CC-DD-EE-FF')"); err != nil {
		t.Fatal(err)
	}
	out := []Device{{MAC: "CC:CC:CC:00:00:01", Name: "CC:CC:CC:00:00:01", Online: false}}
	l.applyDeviceSeen(out)
	if out[0].Name != "CC:CC:CC:00:00:01" {
		t.Errorf("Name = %q, want la MAC viva conservada (el nombre MAC del registro no se restaura)", out[0].Name)
	}
	if out[0].Hostname != "" {
		t.Errorf("Hostname = %q, want vacío (nunca se emite una MAC como hostname)", out[0].Hostname)
	}
}

// #1315 (sub-bug b): stickiness del tipo. Se persiste el último tipo
// INFERIDO (no override, no genérico) y, al expirar el lease, cuando la
// clasificación viva cae en "desconocido" se conserva el retenido. Los
// overrides manuales nunca se persisten ni se pisan.
func TestApplyDeviceSeenStickyInferredType(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	l.noteDevicesSeen([]Device{
		{MAC: "aa:bb:cc:dd:ee:ff", Online: true, Hostname: "galaxy-s9", Type: "movil"},
		{MAC: "11:22:33:44:55:66", Online: true, Hostname: "tv-salon", Type: "tv", TypeOverride: "tv"},
		{MAC: "77:77:77:77:77:77", Online: true, Hostname: "sin-tipo", Type: "desconocido"},
	}, 1000)

	// Lease expirado: nombre degradado, clasificación viva "desconocido".
	out := []Device{
		{MAC: "AA:BB:CC:DD:EE:FF", Name: "AA:BB:CC:DD:EE:FF", Online: true, Type: "desconocido"},
		{MAC: "11:22:33:44:55:66", Name: "11:22:33:44:55:66", Online: true, Type: "tv", TypeOverride: "tv"},
	}
	l.applyDeviceSeen(out)
	if out[0].Type != "movil" {
		t.Errorf("Type = %q, want movil (sticky del último inferido)", out[0].Type)
	}
	if out[1].Type != "tv" || out[1].TypeOverride != "tv" {
		t.Errorf("Type = %q/%q, want tv override intacto (nunca se pisa)", out[1].Type, out[1].TypeOverride)
	}
	// El override manual NO quedó persistido como inferido.
	var stored string
	if err := d.QueryRow("SELECT device_type FROM device_seen WHERE mac = '11:22:33:44:55:66'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Errorf("device_type persistido del override = %q, want vacío (solo se guarda lo inferido)", stored)
	}
	// El genérico tampoco se persiste.
	if err := d.QueryRow("SELECT device_type FROM device_seen WHERE mac = '77:77:77:77:77:77'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Errorf("device_type persistido del desconocido = %q, want vacío", stored)
	}

	// Inferencia viva no genérica: manda sobre el retenido.
	l.noteDevicesSeen([]Device{
		{MAC: "aa:bb:cc:dd:ee:ff", Online: true, Hostname: "nuevo-nombre-tv", Type: "tv"},
	}, 2000)
	out2 := []Device{{MAC: "AA:BB:CC:DD:EE:FF", Name: "AA:BB:CC:DD:EE:FF", Online: true, Type: "desconocido"}}
	l.applyDeviceSeen(out2)
	if out2[0].Type != "tv" {
		t.Errorf("Type = %q, want tv (la inferencia nueva pisa al retenido)", out2[0].Type)
	}
}

// #1315 (sub-bug b): los fantasmas también conservan el último tipo
// inferido; sin esto el icono se perdía igual que en la lista viva.
func TestGhostDevicesStickyType(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	l.noteDevicesSeen([]Device{
		{MAC: "aa:bb:cc:dd:ee:ff", Online: true, Hostname: "roborock-s8", Type: "aspirador"},
		{MAC: "11:22:33:44:55:66", Online: true, Hostname: "sin-tipo"},
	}, 1000)
	ghosts := l.ghostDevices(nil)
	byMAC := map[string]Device{}
	for _, g := range ghosts {
		byMAC[g.MAC] = g
	}
	if g := byMAC["AA:BB:CC:DD:EE:FF"]; g.Type != "aspirador" {
		t.Errorf("ghost Type = %q, want aspirador (sticky)", g.Type)
	}
	if g := byMAC["11:22:33:44:55:66"]; g.Type != "desconocido" {
		t.Errorf("ghost Type = %q, want desconocido (nunca hubo inferencia)", g.Type)
	}
}
