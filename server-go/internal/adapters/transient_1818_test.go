package adapters

// transient_1818_test.go — #1818: una MAC que aparece y desaparece en menos de
// un minuto no debe listarse, ni alertar, ni entrar en device_seen.

import "testing"

func transientLive(t *testing.T) *Live {
	t.Helper()
	d := openLiveTestDB(t)
	return NewLive(nil, d, []RouterConfig{{ID: "gw", Name: "Gateway"}}, nil)
}

// El caso del issue: la MAC se ve, aguanta menos de un minuto y se va.
func TestTransientMACIgnoradaAntesDelMinuto(t *testing.T) {
	l := transientLive(t)
	// Registro pre-poblado: el primer arranque siembra en silencio.
	l.noteDevicesSeen([]Device{{MAC: "aa:aa:aa:00:00:01", Online: true, Hostname: "viejo"}}, 1000)

	mac := "5A:3B:41:63:08:FE"
	devs := []Device{{MAC: mac, Online: true, Hostname: "efimero", RouterID: "gw"}}

	if got := l.settleTransient(devs, 10_000); len(got) != 0 {
		t.Fatalf("el primer avistamiento no debe pasar: %+v", got)
	}
	if got := l.settleTransient(devs, 40_000); len(got) != 0 {
		t.Fatalf("a los 30 s sigue siendo provisional: %+v", got)
	}
	got := l.settleTransient(devs, 71_000)
	if len(got) != 1 {
		t.Fatalf("a los 61 s debe pasar: %+v", got)
	}
	if got[0].FirstSeenMs != 10_000 {
		t.Fatalf("first seen debe ser el primer avistamiento, got %d", got[0].FirstSeenMs)
	}
	// Ya promocionado, no vuelve a retenerse. En produccion el mismo ciclo que
	// lo deja pasar lo persiste, y a partir de ahi el registro es la fuente.
	l.noteDevicesSeen(got, 71_000)
	if got := l.settleTransient(devs, 72_000); len(got) != 1 {
		t.Fatalf("un cliente ya visto debe pasar siempre: %+v", got)
	}
}

// Una MAC de paso no debe quedar ni en el mapa ni en el registro.
func TestTransientMACQueNoPersisteNoSeGuarda(t *testing.T) {
	l := transientLive(t)
	l.noteDevicesSeen([]Device{{MAC: "aa:aa:aa:00:00:01", Online: true, Hostname: "viejo"}}, 1000)

	mac := "5A:3B:41:63:08:FE"
	l.settleTransient([]Device{{MAC: mac, Online: true, Hostname: "efimero", RouterID: "gw"}}, 10_000)

	// Pasada la ventana de expiracion, el mapa se olvida de ella.
	l.settleTransient(nil, 10_000+transientExpiryMs+1)
	if len(l.transient) != 0 {
		t.Fatalf("la MAC provisional debe purgarse: %+v", l.transient)
	}
	seen, err := l.seenByMac()
	if err != nil {
		t.Fatalf("seenByMac: %v", err)
	}
	if _, ok := seen[normSeenMAC(mac)]; ok {
		t.Fatal("una MAC de paso no debe entrar en device_seen")
	}
}

// Un cliente que ya esta en el registro no se retiene aunque el proceso acabe
// de arrancar (el mapa provisional esta vacio).
func TestTransientClienteConocidoPasaSiempre(t *testing.T) {
	l := transientLive(t)
	l.noteDevicesSeen([]Device{{MAC: "aa:aa:aa:00:00:01", Online: true, Hostname: "viejo"}}, 1000)

	devs := []Device{{MAC: "AA:AA:AA:00:00:01", Online: true, Hostname: "viejo", RouterID: "gw"}}
	if got := l.settleTransient(devs, 2000); len(got) != 1 {
		t.Fatalf("un cliente ya registrado pasa siempre: %+v", got)
	}
}

// El alta guarda el primer avistamiento real, no el ciclo en que se promociono.
func TestNoteDevicesSeenGuardaElPrimerAvistamiento(t *testing.T) {
	l := transientLive(t)
	l.noteDevicesSeen([]Device{{MAC: "aa:aa:aa:00:00:01", Online: true, Hostname: "viejo"}}, 1000)
	l.noteDevicesSeen([]Device{{MAC: "5A:3B:41:63:08:FE", Online: true, Hostname: "nuevo", FirstSeenMs: 10_000}}, 71_000)

	seen, err := l.seenByMac()
	if err != nil {
		t.Fatalf("seenByMac: %v", err)
	}
	row, ok := seen["5A:3B:41:63:08:FE"]
	if !ok {
		t.Fatal("el cliente promocionado debe estar en device_seen")
	}
	if row.first != 10_000 {
		t.Fatalf("first_seen debe ser el primer avistamiento, got %d", row.first)
	}
	if row.last != 71_000 {
		t.Fatalf("last_seen debe ser el ciclo actual, got %d", row.last)
	}
}
