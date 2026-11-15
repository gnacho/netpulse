package httpapi

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/wgseries"
	_ "modernc.org/sqlite"
)

func wgStore(t *testing.T) *wgseries.Store {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = d.Close() })
	s, err := wgseries.NewStore(d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// #2019: la tarjeta enseña la ventana real y los días que cubre el histórico,
// en el mismo formato que el total del túnel.
func TestEnrichWireGuardWindow(t *testing.T) {
	store := wgStore(t)
	now := time.Now()
	samples := []struct {
		at     time.Time
		rx, tx int64
	}{
		{now.AddDate(0, 0, -3), 1_000_000_000, 500_000_000},
		{now.AddDate(0, 0, -2), 2_500_000_000, 900_000_000},
		{now.Add(-time.Minute), 3_000_000_000, 1_200_000_000},
	}
	for _, s := range samples {
		if err := store.RecordSamples("gw1", s.at, []wgseries.PeerSample{
			{Key: "peer-a", RxBytes: s.rx, TxBytes: s.tx},
		}); err != nil {
			t.Fatal(err)
		}
	}
	ov := &adapters.Overview{Wireguard: adapters.WireGuardStats{RouterID: "gw1"}}
	EnrichWireGuardWindow(ov, store)
	got := ov.Wireguard.Transfer30d
	if got == nil {
		t.Fatal("transfer30d ausente con histórico")
	}
	// Deltas: 1.5 GB y 0.4 GB del segundo salto, más 0.5 GB y 0.3 GB del
	// tercero. Formateado como el resto del contrato.
	if got.Rx != "2 GB" || got.Tx != "700 MB" {
		t.Fatalf("ventana = (%s,%s); want (2 GB,700 MB)", got.Rx, got.Tx)
	}
	if got.Peers != 1 {
		t.Fatalf("peers = %d; want 1", got.Peers)
	}
	if got.Days != 4 {
		t.Fatalf("days = %d; want 4 (desde hace 3 días)", got.Days)
	}
	if got.Since == "" {
		t.Fatal("since vacío con histórico")
	}
}

// Una instalación sin muestras no publica un cero: deja el campo ausente para
// que la UI pueda decir "sin histórico todavía".
func TestEnrichWireGuardWindowEmpty(t *testing.T) {
	store := wgStore(t)
	ov := &adapters.Overview{Wireguard: adapters.WireGuardStats{RouterID: "gw1"}}
	EnrichWireGuardWindow(ov, store)
	if ov.Wireguard.Transfer30d != nil {
		t.Fatalf("transfer30d = %+v; want nil sin muestras", ov.Wireguard.Transfer30d)
	}
	// Sin store (demo, tests sin BD) tampoco se toca.
	ov2 := &adapters.Overview{Wireguard: adapters.WireGuardStats{RouterID: "gw1"}}
	EnrichWireGuardWindow(ov2, nil)
	if ov2.Wireguard.Transfer30d != nil {
		t.Fatal("transfer30d no debe salir sin store")
	}
}

// El detalle del gateway es un sondeo distinto del overview: la ventana se
// estampa en ambos.
func TestEnrichWireGuardStatsForDetail(t *testing.T) {
	store := wgStore(t)
	now := time.Now()
	_ = store.RecordSamples("gw1", now.Add(-2*time.Hour), []wgseries.PeerSample{{Key: "k", RxBytes: 100, TxBytes: 100}})
	_ = store.RecordSamples("gw1", now.Add(-time.Minute), []wgseries.PeerSample{{Key: "k", RxBytes: 1100, TxBytes: 600}})
	wg := &adapters.WireGuardStats{RouterID: "gw1"}
	EnrichWireGuardStats(wg, store)
	if wg.Transfer30d == nil {
		t.Fatal("transfer30d ausente en el detalle")
	}
	if wg.Transfer30d.Rx != "1 KB" || wg.Transfer30d.Tx != "500 B" {
		t.Fatalf("ventana = (%s,%s); want (1 KB,500 B)", wg.Transfer30d.Rx, wg.Transfer30d.Tx)
	}
	if wg.Transfer30d.Days != 1 {
		t.Fatalf("days = %d; want 1", wg.Transfer30d.Days)
	}
	// Idempotente: no se recalcula si ya viene puesto.
	wg.Transfer30d.Rx = "sin tocar"
	EnrichWireGuardStats(wg, store)
	if wg.Transfer30d.Rx != "sin tocar" {
		t.Fatal("EnrichWireGuardStats recalculó un campo ya puesto")
	}
}
