// wifi_noip_2111_test.go — #2111: un cliente asociado por WiFi que no tiene
// dirección deja UNA nota informativa. No es un desconocido (la asociación
// prueba que existe) y no toca el contador urgente.
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

func TestWiFiNoIPLeavesOneNote(t *testing.T) {
	l := liveTestLive()
	l.wifiNoIPAfterMs = 1 // el cruce se hace moviendo el sello, sin sleeps
	tab := Device{
		MAC: "AA:BB:CC:00:11:22", Name: "Tablet", RouterID: "living",
		Online: true, Band: "5 GHz", IP: "",
	}

	// Primera vista: solo siembra.
	l.trackWiFiNoIP([]Device{tab}, nil)
	if list := l.engine.List(); len(list) != 0 {
		t.Fatalf("primera vista no debía notar nada: %+v", list)
	}
	if _, ok := l.wifiNoIPSince[tab.MAC]; !ok {
		t.Fatal("la primera vista debía sembrar el sello de tiempo")
	}

	// Sigue asociado sin IP, con el sello retrasado: una nota.
	l.wifiNoIPSince[tab.MAC] -= 1000 + l.wifiNoIPThreshold()
	l.trackWiFiNoIP([]Device{tab}, nil)
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("notas: %d, esperaba 1", len(list))
	}
	a := list[0]
	if a.Category != alerts.CatClients || a.Urgent || a.Severity != "info" || a.Type != alerts.TypeWiFiNoIP {
		t.Fatalf("taxonomía: (%s,%v,%s,%s), esperaba (clients,false,info,%s)",
			a.Category, a.Urgent, a.Severity, a.Type, alerts.TypeWiFiNoIP)
	}
	if a.Vars["mac"] != tab.MAC || a.Vars["where"] == "" || a.Vars["minutes"] == "" {
		t.Fatalf("vars incompletas: %+v", a.Vars)
	}

	// La misma MAC no repite la nota.
	delete(l.wifiNoIPSince, tab.MAC)
	l.trackWiFiNoIP([]Device{tab}, nil)
	l.trackWiFiNoIP([]Device{tab}, nil)
	if n := len(l.engine.List()); n != 1 {
		t.Fatalf("notas tras repetir: %d, esperaba 1 (una por MAC)", n)
	}
}

// En cuanto tiene dirección, el reloj se resetea y no hay nota.
func TestWiFiNoIPIgnoresClientsWithIP(t *testing.T) {
	l := liveTestLive()
	l.wifiNoIPAfterMs = 1
	tab := Device{
		MAC: "AA:BB:CC:00:11:33", Name: "Tablet", RouterID: "living",
		Online: true, Band: "5 GHz", IP: "",
	}
	l.trackWiFiNoIP([]Device{tab}, nil)
	if _, ok := l.wifiNoIPSince[tab.MAC]; !ok {
		t.Fatal("debía sembrar el sello")
	}
	tab.IP = "192.168.1.60"
	l.trackWiFiNoIP([]Device{tab}, nil)
	if _, ok := l.wifiNoIPSince[tab.MAC]; ok {
		t.Fatal("con IP el reloj debía borrarse")
	}
	if n := len(l.engine.List()); n != 0 {
		t.Fatalf("notas con IP: %d, esperaba 0", n)
	}
}

// Un equipo cableado sin IP no entra aquí: ese es el camino de #1873/#1818,
// con su propia nota y sus propias reglas.
func TestWiFiNoIPCableWithoutIPIsNotThisSignal(t *testing.T) {
	l := liveTestLive()
	l.wifiNoIPAfterMs = 1
	cable := Device{
		MAC: "AA:BB:CC:00:11:44", Name: "AA:BB:CC:00:11:44", RouterID: "living",
		Online: true, Band: "cable", IP: "",
	}
	l.trackWiFiNoIP([]Device{cable}, nil)
	l.trackWiFiNoIP([]Device{cable}, nil)
	if n := len(l.engine.List()); n != 0 {
		t.Fatalf("un cableado sin IP no debe generar la nota de WiFi: %d", n)
	}
}
