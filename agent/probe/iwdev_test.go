package probe

// iwdev_test.go - #1560: the neighbour scan ran once per interface, so a radio
// with several BSSIDs left the channel once per BSSID. Field report #1557: a
// GL-MT6000 with two 2.4 GHz SSIDs (LAN plus VLAN 10) dropped clients 44s
// apart, which is two scans of the same radio in one cycle.

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// iwDevFlint2 is a real `iw dev` from our Flint2 (GL-MT6000, GL.iNet stock
// firmware): one BSSID per radio, which is why our fleet never hit the double
// scan.
const iwDevFlint2 = `phy#1
	Interface wlan1
		ifindex 19
		wdev 0x100000001
		addr 94:83:c4:ba:bf:ab
		ssid temiscira
		type AP
		channel 36 (5180 MHz), width: 80 MHz, center1: 5210 MHz
		txpower 23.00 dBm
phy#0
	Interface wlan0
		ifindex 18
		wdev 0x1
		addr 32:4e:8e:ac:c6:16
		ssid temiscira
		type AP
		channel 1 (2412 MHz), width: 20 MHz, center1: 2412 MHz
		txpower 20.00 dBm
`

// iwDevMultiBSSID is the layout reported in #1557 for the same box: radio 0
// carries NETGEAR on the LAN and WiFi_10 on VLAN 10. Same `iw dev` shape as
// iwDevFlint2, with the second BSSID of that radio added.
const iwDevMultiBSSID = `phy#1
	Interface phy1-ap0
		ifindex 19
		addr 94:83:c4:ba:bf:c0
		ssid NETGEAR
		type AP
		channel 36 (5180 MHz), width: 80 MHz, center1: 5210 MHz
phy#0
	Interface phy0-ap0
		ifindex 18
		addr 94:83:c4:ba:bf:ab
		ssid NETGEAR
		type AP
		channel 1 (2412 MHz), width: 20 MHz, center1: 2412 MHz
	Interface phy0-ap1
		ifindex 20
		addr 96:83:c4:ba:bf:ab
		ssid WiFi_10
		type AP
		channel 1 (2412 MHz), width: 20 MHz, center1: 2412 MHz
`

// iwDevNoPhy is the output without phy headers (trimmed or old iw): the
// grouping cannot know which radio an interface belongs to.
const iwDevNoPhy = `	Interface wlan1
		ssid temiscira
		type AP
	Interface wlan0
		ssid temiscira
		type AP
`

func TestParseIwDevDetailedKeepsRadio(t *testing.T) {
	got := ParseIwDevDetailed(iwDevMultiBSSID)
	if len(got) != 3 {
		t.Fatalf("interfaces: %+v", got)
	}
	want := []IwDevIface{
		{Iface: "phy1-ap0", Phy: 1, Type: "AP"},
		{Iface: "phy0-ap0", Phy: 0, Type: "AP"},
		{Iface: "phy0-ap1", Phy: 0, Type: "AP"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("iface %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

// TestScanTargetsOnePerRadio: two BSSIDs on phy#0 must collapse into one
// target, and the AP interface of that radio is the one picked.
func TestScanTargetsOnePerRadio(t *testing.T) {
	targets := IwDevScanTargets(ParseIwDevDetailed(iwDevMultiBSSID))
	if len(targets) != 2 {
		t.Fatalf("dos radios deben dar dos targets: %+v", targets)
	}
	if targets[0].Iface != "phy1-ap0" || targets[0].Phy != 1 {
		t.Fatalf("orden de aparición perdido: %+v", targets)
	}
	if targets[1].Iface != "phy0-ap0" || targets[1].Phy != 0 {
		t.Fatalf("radio 0 debe escanearse una vez: %+v", targets)
	}
}

func TestScanTargetsOnePerRadioOnOurFleet(t *testing.T) {
	targets := IwDevScanTargets(ParseIwDevDetailed(iwDevFlint2))
	if len(targets) != 2 || targets[0].Iface != "wlan1" || targets[1].Iface != "wlan0" {
		t.Fatalf("una BSS por radio debe seguir dando un target por radio: %+v", targets)
	}
}

// TestScanTargetsPrefersAP: a monitor or P2P interface sharing the radio with
// the AP must not be the one scanned.
func TestScanTargetsPrefersAP(t *testing.T) {
	iwDev := "phy#0\n" +
		"\tInterface mon0\n\t\ttype monitor\n" +
		"\tInterface p2p-dev-wlan0\n\t\ttype P2P-device\n" +
		"\tInterface phy0-ap0\n\t\ttype AP\n"
	targets := IwDevScanTargets(ParseIwDevDetailed(iwDev))
	if len(targets) != 1 || targets[0].Iface != "phy0-ap0" {
		t.Fatalf("se debe escanear el AP: %+v", targets)
	}
}

// TestScanTargetsFallsBackWithoutPhy: sin secciones phy# se mantiene el
// comportamiento anterior (una interfaz por target).
func TestScanTargetsFallsBackWithoutPhy(t *testing.T) {
	targets := IwDevScanTargets(ParseIwDevDetailed(iwDevNoPhy))
	if len(targets) != 2 || targets[0].Iface != "wlan1" || targets[1].Iface != "wlan0" {
		t.Fatalf("fallback por interfaz: %+v", targets)
	}
	for _, tgt := range targets {
		if tgt.Phy != -1 {
			t.Fatalf("sin phy# el radio debe ser -1: %+v", tgt)
		}
	}
}

// recordingRunner keeps every command the prober runs, in order.
type recordingRunner struct {
	mu   sync.Mutex
	cmds []string
	outs map[string]string
}

func (r *recordingRunner) Run(_ context.Context, cmd string, _ time.Duration) (string, error) {
	r.mu.Lock()
	r.cmds = append(r.cmds, cmd)
	r.mu.Unlock()
	if out, ok := r.outs[cmd]; ok {
		return out, nil
	}
	return "", &fakeErr{cmd}
}

func (r *recordingRunner) scanCmds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var res []string
	for _, c := range r.cmds {
		if strings.HasPrefix(c, "iw dev \"") && strings.Contains(c, " scan") {
			res = append(res, c)
		}
	}
	return res
}

// TestBuildScansOncePerRadio is the regression test for #1560: the same
// multi-BSSID unit must run ONE scan, not one per BSSID.
func TestBuildScansOncePerRadio(t *testing.T) {
	run := &recordingRunner{outs: map[string]string{
		CmdIwDev:                 iwDevMultiBSSID,
		CmdScanIface("phy1-ap0"): scanVecino24,
		CmdScanIface("phy0-ap0"): scanVecino24,
		CmdScanIface("phy0-ap1"): scanVecino24,
	}}
	p := NewProber(run, Options{})

	p.Build(context.Background(), "mt6000", "test")

	got := run.scanCmds()
	if len(got) != 2 {
		t.Fatalf("dos radios deben dar dos scans, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "phy1-ap0") || !strings.Contains(got[1], "phy0-ap0") {
		t.Fatalf("scans inesperados: %v", got)
	}
	for _, c := range got {
		if strings.Contains(c, "phy0-ap1") {
			t.Fatalf("la segunda BSS de la misma radio no se escanea: %v", got)
		}
	}
}

// scanVecino24 is a real single-neighbour scan output (rt2, 2.4 GHz).
const scanVecino24 = `BSS c4:eb:39:0c:2b:c1(on phy0-ap0)
	freq: 2437.0
	signal: -67.00 dBm
	SSID: Vecino_24
`

// TestScanLogsCycle covers #1559: a scan must leave a trace (start, per-radio
// duration and result, total) so a client drop can be lined up with it.
func TestScanLogsCycle(t *testing.T) {
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	run := &recordingRunner{outs: map[string]string{
		CmdIwDev:                 iwDevMultiBSSID,
		CmdScanIface("phy1-ap0"): scanVecino24,
		CmdScanIface("phy0-ap0"): scanVecino24,
	}}
	p := NewProber(run, Options{})
	p.Build(context.Background(), "mt6000", "test")

	log := buf.String()
	for _, want := range []string{
		"neighbor scan start",
		"radios=2",
		"neighbor scan radio",
		`iface=phy0-ap0`,
		"bss=1",
		"duration_ms=",
		"neighbor scan done",
		"bss=2",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log sin %q:\n%s", want, log)
		}
	}
}
