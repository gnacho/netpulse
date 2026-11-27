package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// #2183: un vecino del segmento WAN no dispara "dispositivo desconocido".
// La gracia de unknown-device (#234) es de 3 ticks; el vecino se queda
// online y sin nombre ese tiempo y aun así no debe alertar.
func TestTrackUnknownDevicesIgnoresWanSide(t *testing.T) {
	l := liveTestLive()
	neighbour := Device{MAC: "0C:EF:15:00:00:02", IP: "203.0.113.248", Name: "0C:EF:15:00:00:02", RouterID: "gw", Online: true, Band: "cable", WanSide: true}
	named := Device{MAC: "77:88:99:AA:BB:CC", Name: "Galaxy Tab", RouterID: "gw", Online: true}

	// Primer ciclo: siembra (no alerta) y siguientes, más allá de la gracia.
	for i := 0; i < 6; i++ {
		l.trackUnknownDevices([]Device{neighbour, named}, nil)
	}
	if n := len(l.engine.List()); n != 0 {
		t.Fatalf("un vecino del WAN generó %d alertas, want 0", n)
	}
	// Gate negativo: el mismo dispositivo sin la marca WanSide sí alerta.
	neighbour.WanSide = false
	for i := 0; i < 6; i++ {
		l.trackUnknownDevices([]Device{neighbour, named}, nil)
	}
	if n := len(l.engine.List()); n != 1 {
		t.Fatalf("sin WanSide el desconocido debe alertar una vez, got %d", n)
	}
}

// #2183: el registro (device_seen) no guarda vecinos del WAN, y los que
// quedaron de versiones anteriores se purgan para que no reaparezcan como
// fantasmas offline en el inventario.
func TestDeviceSeenSkipsAndPurgesWanSide(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	l := NewLive(nil, d, nil, nil)

	// Una fila previa (de una versión anterior) para el vecino WAN.
	if _, err := d.Exec(`INSERT INTO device_seen (mac, first_seen, last_seen, name) VALUES (?, 1, 1, '')`, "0C:EF:15:00:00:02"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO device_seen (mac, first_seen, last_seen, name) VALUES (?, 1, 1, 'NAS')`, "AA:BB:CC:00:00:01"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	wan := Device{MAC: "0C:EF:15:00:00:02", Name: "0C:EF:15:00:00:02", Online: true, WanSide: true}
	lan := Device{MAC: "AA:BB:CC:00:00:01", Name: "NAS", Online: true}
	if firsts := l.noteDevicesSeen([]Device{wan, lan}, 2000); len(firsts) != 0 {
		t.Fatalf("clientes nuevos inesperados: %+v", firsts)
	}
	if n := countRows(t, d, "SELECT COUNT(*) FROM device_seen WHERE mac = ?", "0C:EF:15:00:00:02"); n != 1 {
		t.Fatalf("noteDevicesSeen no debe insertar vecinos WAN, filas=%d", n)
	}

	l.purgeWanSideSeen([]Device{wan, lan})
	if n := countRows(t, d, "SELECT COUNT(*) FROM device_seen WHERE mac = ?", "0C:EF:15:00:00:02"); n != 0 {
		t.Fatalf("el vecino WAN debe purgarse del registro, filas=%d", n)
	}
	if n := countRows(t, d, "SELECT COUNT(*) FROM device_seen WHERE mac = ?", "AA:BB:CC:00:00:01"); n != 1 {
		t.Fatalf("el cliente LAN no se toca, filas=%d", n)
	}
}

func countRows(t *testing.T, d *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	return n
}
