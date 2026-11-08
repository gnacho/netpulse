// deleted_client_revive_1984_test.go - el borrado de un cliente es un olvido,
// no un baneo: si vuelve con evidencia real (IP de lease o ARP) se da de alta
// otra vez. Una entrada cacheada sin IP sigue enterrada (#1278, #1984).
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func newSeenLive(t *testing.T) (*Live, *db.DB) {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return NewLive(nil, d, nil, nil), d
}

func tombstoned(t *testing.T, d *db.DB, mac string) bool {
	t.Helper()
	tomb, err := d.DeletedClients()
	if err != nil {
		t.Fatalf("DeletedClients: %v", err)
	}
	return tomb[mac]
}

func TestDeletedClientComesBackWithIP(t *testing.T) {
	l, d := newSeenLive(t)
	mac := "EC:71:DB:44:12:8A"
	if err := d.AddDeletedClient(mac); err != nil {
		t.Fatalf("AddDeletedClient: %v", err)
	}

	dev := Device{MAC: mac, IP: "192.168.8.71", Online: true, Hostname: "movil"}
	l.noteDevicesSeen([]Device{dev}, 1000)

	if tombstoned(t, d, mac) {
		t.Fatal("la lapida deberia levantarse al volver el cliente con IP")
	}
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM device_seen WHERE mac = ?", mac).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("device_seen filas = %d, esperaba 1", n)
	}
}

func TestDeletedClientStaysBuriedWithoutIP(t *testing.T) {
	l, d := newSeenLive(t)
	mac := "EC:71:DB:44:12:8B"
	if err := d.AddDeletedClient(mac); err != nil {
		t.Fatalf("AddDeletedClient: %v", err)
	}

	// Estacion cacheada en el AP: online y con banda, pero sin IP.
	dev := Device{MAC: mac, Online: true, Band: "5 GHz"}
	l.noteDevicesSeen([]Device{dev}, 1000)

	if !tombstoned(t, d, mac) {
		t.Fatal("sin IP la lapida debe seguir")
	}
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM device_seen WHERE mac = ?", mac).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Fatalf("device_seen filas = %d, esperaba 0", n)
	}
}

func TestFilterTombstonedLetsTheRevivedClientThrough(t *testing.T) {
	l, d := newSeenLive(t)
	mac := "EC:71:DB:44:12:8C"
	buried := "EC:71:DB:44:12:8D"
	for _, m := range []string{mac, buried} {
		if err := d.AddDeletedClient(m); err != nil {
			t.Fatalf("AddDeletedClient: %v", err)
		}
	}

	out := l.filterTombstoned([]Device{
		{MAC: mac, IP: "192.168.8.72", Online: true}, // vuelve con IP
		{MAC: buried, Band: "5 GHz", Online: true},   // cacheada, sin IP
	})
	if len(out) != 1 || out[0].MAC != mac {
		t.Fatalf("lista filtrada = %+v", out)
	}
	if tombstoned(t, d, mac) {
		t.Fatal("la lapida deberia haberse levantado")
	}
	if !tombstoned(t, d, buried) {
		t.Fatal("la lapida sin evidencia debe seguir")
	}
}
