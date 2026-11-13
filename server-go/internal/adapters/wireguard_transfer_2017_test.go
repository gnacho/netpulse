// #2017: la tarjeta WireGuard pintaba un "Transfer 30 d" fijo (17,9 GB /
// 5,0 GB) que venía de una constante del frontend, no de ninguna medida. El
// dato real que existe es la suma de los contadores por peer de `wg show
// dump` (todas las interfaces), que es acumulada, no una ventana de 30 días.
// Estos tests fijan que el total se calcule de ahí y que no se invente nada
// cuando no hay peers.
package adapters

import (
	"fmt"
	"strings"
	"testing"
)

// wgPeerLineBytes compone la línea TSV de un peer con sus contadores de
// transferencia (campos 6 y 7 del dump).
func wgPeerLineBytes(pubkey, allowedIPs string, rx, tx int64) string {
	return fmt.Sprintf("%s\t\t(none)\t%s\t1700000060\t%d\t%d\t0", pubkey, allowedIPs, rx, tx)
}

func TestWGTransferSumsEveryInterface(t *testing.T) {
	wg0 := wgPeerDump("wg0",
		wgPeerLineBytes("AAA=", "10.0.0.2/32", 1_200_000_000, 214_000_000),
		wgPeerLineBytes("BBB=", "10.0.0.4/32", 3_100_000_000, 402_000_000),
	)
	wg1 := wgPeerDump("wg1",
		wgPeerLineBytes("CCC=", "10.0.0.6/32", 12_000_000_000, 4_200_000_000),
	)
	f := &fakeWGRunner{run: func(cmd string) (string, error) {
		switch {
		case cmd == wgListCommand:
			return "wg0 wg1\n", nil
		case strings.Contains(cmd, "wg show wg0 dump"):
			return wgStatsOutput(wg0, ""), nil
		case strings.Contains(cmd, "wg show wg1 dump"):
			return wgStatsOutput(wg1, ""), nil
		}
		return "", fmt.Errorf("comando inesperado: %s", cmd)
	}}
	stats, err := GetWireGuardStats(f, "host", "auto", "", nil, nil)
	if err != nil {
		t.Fatalf("GetWireGuardStats: %v", err)
	}
	if len(stats.Peers) != 3 {
		t.Fatalf("peers: %d, want 3", len(stats.Peers))
	}
	if stats.Transfer == nil {
		t.Fatal("sin transfer con peers sondeados")
	}
	// 1,2 + 3,1 + 12 GB y 214 + 402 MB + 4,2 GB
	if stats.Transfer.Rx != "16,3 GB" || stats.Transfer.Tx != "4,8 GB" {
		t.Fatalf("transfer = %+v, want 16,3 GB / 4,8 GB", *stats.Transfer)
	}
}

func TestWGTransferSkippedInterfaceDoesNotCount(t *testing.T) {
	// #714 + #2017: una interfaz caída se salta y no aporta al total, pero el
	// total sigue saliendo de las que responden.
	wg0 := wgPeerDump("wg0", wgPeerLineBytes("AAA=", "10.0.0.2/32", 2_000_000_000, 500_000_000))
	f := &fakeWGRunner{run: func(cmd string) (string, error) {
		switch {
		case cmd == wgListCommand:
			return "wg0 wg1\n", nil
		case strings.Contains(cmd, "wg show wg0 dump"):
			return wgStatsOutput(wg0, ""), nil
		}
		return "", fmt.Errorf("interfaz caída")
	}}
	stats, err := GetWireGuardStats(f, "host", "auto", "", nil, nil)
	if err != nil {
		t.Fatalf("GetWireGuardStats: %v", err)
	}
	if stats.Transfer == nil || stats.Transfer.Rx != "2 GB" || stats.Transfer.Tx != "500 MB" {
		t.Fatalf("transfer = %+v, want 2 GB / 500 MB", stats.Transfer)
	}
}

func TestWGTransferAbsentWithoutPeers(t *testing.T) {
	// Sin peers no hay total: el cliente pinta "—" en vez de un número.
	f := &fakeWGRunner{run: func(cmd string) (string, error) {
		if cmd == wgListCommand {
			return "wgserver\n", nil
		}
		return wgStatsOutput(wgPeerDump("wgserver"), ""), nil
	}}
	stats, err := GetWireGuardStats(f, "host", "auto", "", nil, nil)
	if err != nil {
		t.Fatalf("GetWireGuardStats: %v", err)
	}
	if stats.Transfer != nil {
		t.Fatalf("transfer sin peers = %+v, want nil", *stats.Transfer)
	}
}

func TestDemoWGTransferMatchesPeers(t *testing.T) {
	// En demo el total se deriva de los peers que pinta la tarjeta: si alguien
	// toca el dataset, el número de arriba sigue cuadrando con la lista.
	d := NewDemo()
	ov, err := d.GetOverview(t.Context())
	if err != nil {
		t.Fatalf("GetOverview: %v", err)
	}
	if ov.Wireguard.Transfer == nil {
		t.Fatal("demo sin transfer")
	}
	var rx, tx float64
	for _, p := range ov.Wireguard.Peers {
		rx += parseBytes(p.Rx)
		tx += parseBytes(p.Tx)
	}
	if want := fmtBytes(rx); ov.Wireguard.Transfer.Rx != want {
		t.Fatalf("rx = %q, want %q (suma de los peers)", ov.Wireguard.Transfer.Rx, want)
	}
	if want := fmtBytes(tx); ov.Wireguard.Transfer.Tx != want {
		t.Fatalf("tx = %q, want %q (suma de los peers)", ov.Wireguard.Transfer.Tx, want)
	}
}
