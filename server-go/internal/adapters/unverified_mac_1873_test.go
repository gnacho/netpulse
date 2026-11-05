package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// issue #1873: una MAC que persiste solo en una tabla de reenvío, sin lease,
// ARP, ND ni mDNS, deja una nota informativa. No alerta y no toca el gate de
// #1818: el contador urgente tiene que quedarse a cero.
func TestUnverifiedMacLeavesOneNote(t *testing.T) {
	l := liveTestLive()
	l.unverifiedAfterMs = 1 // ms: el cruce se hace moviendo el sello, sin sleeps
	stray := Device{MAC: "02:11:22:33:44:55", Name: "02:11:22:33:44:55", RouterID: "living", Online: true}

	// Primera vista: solo siembra, nada en el feed.
	l.trackUnknownDevices([]Device{stray}, nil)
	if list := l.engine.List(); len(list) != 0 {
		t.Fatalf("primera vista no debía notar nada: %+v", list)
	}
	if _, ok := l.unverifiedSince[stray.MAC]; !ok {
		t.Fatal("la primera vista debía sembrar el sello de tiempo")
	}

	// Sigue ahí, con el sello retrasado más allá del umbral: una nota.
	l.unverifiedSince[stray.MAC] = l.unverifiedSince[stray.MAC] - 1000 - l.unverifiedThreshold()
	l.trackUnknownDevices([]Device{stray}, nil)
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("notas: %d, esperaba 1", len(list))
	}
	a := list[0]
	if a.Category != alerts.CatClients || a.Urgent || a.Severity != "info" || a.Type != alerts.TypeUnverifiedMac {
		t.Fatalf("taxonomía: (%s,%v,%s,%s), esperaba (clients,false,info,%s)",
			a.Category, a.Urgent, a.Severity, a.Type, alerts.TypeUnverifiedMac)
	}
	if a.Vars["mac"] != stray.MAC || a.Vars["where"] == "" {
		t.Fatalf("vars incompletas: %+v", a.Vars)
	}

	// La misma MAC no repite la nota aunque vuelva a aparecer sin evidencia.
	l.unverifiedNoted[stray.MAC] = true
	delete(l.unverifiedSince, stray.MAC)
	l.trackUnknownDevices([]Device{stray}, nil)
	if n := len(l.engine.List()); n != 1 {
		t.Fatalf("notas tras reaparecer: %d, esperaba 1 (una por MAC)", n)
	}

	// Tampoco dispara la alerta urgente de #248/#1818.
	if l.unknownAlerted[stray.MAC] {
		t.Fatal("la nota no debe marcar la MAC como desconocido alertado")
	}
}

// Una MAC con evidencia de host (aquí una dirección) nunca entra en el reloj
// de la nota: el mecanismo es solo para lo que no tiene nada.
func TestUnverifiedMacIgnoresKnownHosts(t *testing.T) {
	l := liveTestLive()
	l.unverifiedAfterMs = 1
	known := Device{MAC: "02:AA:BB:CC:DD:EE", Name: "02:AA:BB:CC:DD:EE", IP: "192.168.1.50", RouterID: "living", Online: true}

	l.trackUnknownDevices([]Device{known}, nil)
	if _, ok := l.unverifiedSince[known.MAC]; ok {
		t.Fatal("una MAC con IP no debía sembrar el sello de la nota")
	}
	l.trackUnknownDevices([]Device{known}, nil)
	for _, a := range l.engine.List() {
		if a.Type == alerts.TypeUnverifiedMac {
			t.Fatalf("MAC con IP no debía dejar nota: %+v", a)
		}
	}
}

// El reloj se reinicia cuando la MAC desaparece: el tiempo cuenta solo
// mientras está ahí, no de forma acumulada entre avistamientos sueltos.
func TestUnverifiedMacClockRestartsWhenItGoes(t *testing.T) {
	l := liveTestLive()
	l.unverifiedAfterMs = 1
	stray := Device{MAC: "02:99:88:77:66:55", Name: "02:99:88:77:66:55", RouterID: "living", Online: true}

	l.trackUnknownDevices([]Device{stray}, nil)
	if _, ok := l.unverifiedSince[stray.MAC]; !ok {
		t.Fatal("debía sembrar el sello")
	}
	l.trackUnknownDevices(nil, nil) // se va
	if _, ok := l.unverifiedSince[stray.MAC]; ok {
		t.Fatal("al irse debía borrarse el sello")
	}
}
