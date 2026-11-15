package poller

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// #2019: el poller persiste el acumulado de los peers del túnel cada
// wgSampleInterval, no en cada tick, y guarda el delta contra la muestra
// anterior. Sin esto la ventana de 30 días no existe.
func TestPersistWireGuardSamplesOnInterval(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	p := New(adapters.NewDemo(), d, nil, 0)
	ov := &adapters.Overview{
		Wireguard: adapters.WireGuardStats{
			RouterID: "gw1",
			Peers: []adapters.WGPeer{
				{Key: "peer-a", RxBytes: 1000, TxBytes: 2000},
			},
		},
	}

	base := time.Now().UnixMilli()
	p.persistWireGuard(ov, base)
	if n := countWGRows(t, d, "gw1"); n != 1 {
		t.Fatalf("primera muestra: filas = %d; want 1", n)
	}

	// Un tick dentro del intervalo no escribe nada.
	ov.Wireguard.Peers[0] = adapters.WGPeer{Key: "peer-a", RxBytes: 1500, TxBytes: 2000}
	p.persistWireGuard(ov, base+60_000)
	if n := countWGRows(t, d, "gw1"); n != 1 {
		t.Fatalf("tick dentro del intervalo escribió: filas = %d; want 1", n)
	}

	// Pasado el intervalo sí, y con el delta acumulado del hueco.
	ov.Wireguard.Peers[0] = adapters.WGPeer{Key: "peer-a", RxBytes: 4000, TxBytes: 2500}
	p.persistWireGuard(ov, base+(wgSampleInterval.Milliseconds()))
	if n := countWGRows(t, d, "gw1"); n != 2 {
		t.Fatalf("muestra tras el intervalo: filas = %d; want 2", n)
	}
	var dRx, dTx int64
	if err := d.QueryRow(`SELECT d_rx, d_tx FROM wg_peer_series_raw
		WHERE router_id = 'gw1' ORDER BY ts DESC LIMIT 1`).Scan(&dRx, &dTx); err != nil {
		t.Fatal(err)
	}
	if dRx != 3000 || dTx != 500 {
		t.Fatalf("delta = (%d,%d); want (3000,500)", dRx, dTx)
	}
}

// Sin RouterID (peers que no vienen de un sondeo identificado) o sin peers no
// se escribe nada: no se inventan filas huérfanas.
func TestPersistWireGuardSkipsUnidentified(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	p := New(adapters.NewDemo(), d, nil, 0)
	p.persistWireGuard(&adapters.Overview{Wireguard: adapters.WireGuardStats{
		Peers: []adapters.WGPeer{{Key: "peer-a", RxBytes: 10}},
	}}, time.Now().UnixMilli())
	if n := countWGRows(t, d, ""); n != 0 {
		t.Fatalf("filas sin router = %d; want 0", n)
	}

	p.persistWireGuard(&adapters.Overview{Wireguard: adapters.WireGuardStats{
		RouterID: "gw1",
	}}, time.Now().UnixMilli())
	if n := countWGRows(t, d, "gw1"); n != 0 {
		t.Fatalf("filas sin peers = %d; want 0", n)
	}
}

func countWGRows(t *testing.T, d *db.DB, routerID string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM wg_peer_series_raw WHERE router_id = ?`, routerID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
