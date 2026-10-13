package adapters

// newclient_test.go — #1323: alerta "new client" cuando una MAC entra en
// device_seen por primera vez CON nombre (el hueco de unknown-device, que
// solo cubre sin-nombre con gracia).

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

func newClientLive(t *testing.T) *Live {
	t.Helper()
	d := openLiveTestDB(t)
	l := NewLive(nil, d, []RouterConfig{{ID: "gw", Name: "Gateway"}}, nil)
	return l
}

func newClientAlerts(l *Live) []AlertEvent {
	var out []AlertEvent
	for _, a := range l.engine.List() {
		if a.Type == alerts.HintNewClient {
			out = append(out, a)
		}
	}
	return out
}

// El caso del issue: cliente nuevo CON nombre (reserva DHCP) alerta una vez.
func TestNewClientNamedAlertsOnce(t *testing.T) {
	l := newClientLive(t)
	// Registro pre-poblado (no es el primer arranque): siembra manual.
	l.noteDevicesSeen([]Device{{MAC: "aa:aa:aa:00:00:01", Online: true, Hostname: "viejo"}}, 1000)

	devs := []Device{
		{MAC: "aa:aa:aa:00:00:01", Online: true, Hostname: "viejo"},
		{MAC: "5A:3B:41:63:08:FE", Online: true, Hostname: "portatil-nuevo", IP: "192.168.1.150", RouterID: "gw"},
	}
	first := l.noteDevicesSeen(devs, 2000)
	if len(first) != 1 || first[0].MAC != "5A:3B:41:63:08:FE" {
		t.Fatalf("firstTimers: %+v", first)
	}
	l.trackNewClients(first, devs, nil)
	got := newClientAlerts(l)
	if len(got) != 1 {
		t.Fatalf("una alerta new-client; got %d", len(got))
	}
	a := got[0]
	if !a.Urgent || a.Severity != "info" || a.Category != alerts.CatClients {
		t.Fatalf("urgent+info+clients: %+v", a)
	}
	if a.Vars["name"] != "portatil-nuevo" || a.Vars["ip"] != "192.168.1.150" || a.Vars["router"] != "Gateway" {
		t.Fatalf("vars: %+v", a.Vars)
	}
	// One-shot: el mismo MAC en el siguiente ciclo no vuelve a alertar.
	first2 := l.noteDevicesSeen(devs, 3000)
	if len(first2) != 0 {
		t.Fatalf("ya registrado no es first-timer: %+v", first2)
	}
}

// Sin nombre: lo cubre unknown-device (#234), no new-client (sin duplicar).
func TestNewClientNamelessDoesNotAlert(t *testing.T) {
	l := newClientLive(t)
	l.noteDevicesSeen([]Device{{MAC: "aa:aa:aa:00:00:01", Online: true, Hostname: "viejo"}}, 1000)
	devs := []Device{{MAC: "bb:bb:bb:00:00:02", Online: true, RouterID: "gw"}} // Name=MAC efectivo
	first := l.noteDevicesSeen(devs, 2000)
	if len(first) != 0 {
		t.Fatalf("sin nombre no es new-client (lo cubre unknown): %+v", first)
	}
	l.trackNewClients(first, devs, nil)
	if got := newClientAlerts(l); len(got) != 0 {
		t.Fatalf("sin nombre no alerta aquí: %+v", got)
	}
}

// Anti-avalancha: tabla vacía al entrar (primer arranque / BD nueva) =
// siembra silenciosa; los presentes no son "nuevos".
func TestNewClientStartupSeedingIsSilent(t *testing.T) {
	l := newClientLive(t)
	devs := []Device{
		{MAC: "aa:aa:aa:00:00:01", Online: true, Hostname: "uno", RouterID: "gw"},
		{MAC: "aa:aa:aa:00:00:02", Online: true, Hostname: "dos", RouterID: "gw"},
	}
	first := l.noteDevicesSeen(devs, 1000)
	if len(first) != 0 {
		t.Fatalf("primer arranque no alerta: %+v", first)
	}
	l.trackNewClients(first, devs, nil)
	if got := newClientAlerts(l); len(got) != 0 {
		t.Fatalf("siembra silenciosa: %+v", got)
	}
	// Pero un alta POSTERIOR a la siembra sí alerta.
	devs2 := append(devs, Device{MAC: "aa:aa:aa:00:00:03", Online: true, Hostname: "tres", RouterID: "gw"})
	first2 := l.noteDevicesSeen(devs2, 2000)
	if len(first2) != 1 {
		t.Fatalf("tras la siembra, el nuevo sí se detecta: %+v", first2)
	}
}

// Una MAC que ya alertó como desconocida no re-alert como new-client si
// luego resuelve nombre; y la tombstone (#1278/#1290) nunca re-alert.
func TestNewClientUnknownGuard(t *testing.T) {
	l := newClientLive(t)
	l.noteDevicesSeen([]Device{{MAC: "aa:aa:aa:00:00:01", Online: true, Hostname: "viejo"}}, 1000)
	_, _ = l.db.Exec("INSERT INTO kv (key, value) VALUES ('unknown_alerted:CC:CC:CC:00:00:04', '1')")
	devs := []Device{{MAC: "cc:cc:cc:00:00:04", Online: true, Hostname: "antes-desconocido", RouterID: "gw"}}
	first := l.noteDevicesSeen(devs, 2000)
	l.trackNewClients(first, devs, nil)
	if got := newClientAlerts(l); len(got) != 0 {
		t.Fatalf("ya alertó como desconocido: %+v", got)
	}
}
