package probe

// scan_fallback_1839_test.go — #1839: en Asuswrt (dhd/Broadcom) cada radio
// expone una VAP AP y una managed. La AP contesta al scan con "Operation not
// supported (-95)" y la managed si devuelve BSSes. El agente elegia la AP
// (para que el nombre de interfaz en wifi_scans fuera el de siempre) y se
// quedaba sin vecinos sin dejar rastro. Ahora, si la elegida falla, se
// reintenta en el mismo ciclo con otra interfaz del mismo phy, y el resultado
// por radio viaja en scanOutcomes.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// iwDevAsuswrt reproduce una radio con VAP AP y managed en el mismo phy, más
// una segunda radio distinta que no debe entrar en los reintentos.
const iwDevAsuswrt = "phy#1\n" +
	"\tInterface wl1.1\n" +
	"\t\taddr 62:22:32:5b:5c:99\n" +
	"\t\tssid I1_LAN\n" +
	"\t\ttype AP\n" +
	"\t\tchannel 48 (5240 MHz), width: 80 MHz, center1: 5210 MHz\n" +
	"\tInterface eth6\n" +
	"\t\taddr 62:22:32:5b:5c:9a\n" +
	"\t\ttype managed\n" +
	"\t\tchannel 48 (5240 MHz), width: 80 MHz, center1: 5210 MHz\n" +
	"phy#0\n" +
	"\tInterface wl0.1\n" +
	"\t\taddr 62:22:32:5b:5c:98\n" +
	"\t\tssid I1_LAN\n" +
	"\t\ttype AP\n" +
	"\t\tchannel 11 (2462 MHz), width: 20 MHz, center1: 2462 MHz\n"

// scan5g es la salida real que devuelve la VAP managed en el equipo del
// reportero: freqs 5200/5240, o sea el mismo bloque de 80 MHz.
const scan5g = "BSS a4:2b:b0:11:22:33(on eth6)\n" +
	"\tlast seen: 275247.799s [boottime]\n" +
	"\tfreq: 5240.0\n" +
	"\tcapability: ESS (0x1011)\n" +
	"\tsignal: -58.00 dBm\n" +
	"\tlast seen: 120 ms ago\n" +
	"\tSSID: Vecino_5G\n"

// runnerVapApRechaza simula la AP que rechaza el scan y la managed que
// responde.
type runnerVapApRechaza struct {
	fakeRunner
	fail map[string]bool
}

func (r runnerVapApRechaza) Run(ctx context.Context, cmd string, timeout time.Duration) (string, error) {
	if r.fail[cmd] {
		return "", &fakeErr{cmd}
	}
	return r.fakeRunner.Run(ctx, cmd, timeout)
}

func TestIwDevScanAlternates(t *testing.T) {
	ifaces := ParseIwDevDetailed(iwDevAsuswrt)
	chosen := IwDevScanTargets(ifaces)[0]
	if chosen.Iface != "wl1.1" {
		t.Fatalf("se esperaba la AP como objetivo: %+v", chosen)
	}
	alts := IwDevScanAlternates(ifaces, chosen)
	if len(alts) != 1 || alts[0].Iface != "eth6" {
		t.Fatalf("solo la managed del mismo phy vale como reintento: %+v", alts)
	}
	// Monitor va despues de managed.
	withMonitor := append(append([]IwDevIface{}, ifaces...), IwDevIface{Iface: "mon0", Phy: 1, Type: "monitor"})
	alts = IwDevScanAlternates(withMonitor, chosen)
	if len(alts) != 2 || alts[0].Iface != "eth6" || alts[1].Iface != "mon0" {
		t.Fatalf("orden de reintentos: %+v", alts)
	}
	if got := IwDevScanAlternates(ifaces, IwDevIface{Iface: "wlan0", Phy: -1}); got != nil {
		t.Fatalf("sin phy no hay reintento posible: %+v", got)
	}
}

func TestScanRechazadoReintentaEnLaManaged(t *testing.T) {
	run := runnerVapApRechaza{
		fakeRunner: fakeRunner{outs: map[string]string{
			CmdIwDev:                 iwDevAsuswrt,
			CmdScanIface("eth6"):     scan5g,
			CmdScanIface("wl0.1"):    "",
			CmdScanIface("wl1.1"):    "",
			CmdScanIface("phy1-ap0"): "",
		}},
		fail: map[string]bool{CmdScanIface("wl1.1"): true},
	}
	p := NewProber(run, Options{})
	pl := p.Build(context.Background(), "rt-ax92u", "test")
	if pl.Data.Wireless == nil {
		t.Fatal("sección wireless ausente")
	}
	w := pl.Data.Wireless
	if len(w.Scans) != 1 || w.Scans[0].Iface != "eth6" || w.Scans[0].SSID != "Vecino_5G" {
		t.Fatalf("el vecino debe venir de la managed: %+v", w.Scans)
	}
	if len(w.ScanOutcomes) != 2 {
		t.Fatalf("un outcome por radio: %+v", w.ScanOutcomes)
	}
	if o := w.ScanOutcomes[0]; o.Iface != "eth6" || o.Phy != 1 || o.BSS != 1 || o.Error != "" {
		t.Fatalf("outcome de la radio 5 GHz: %+v", o)
	}
}

func TestScanRechazadoRegistraElMotivo(t *testing.T) {
	run := runnerVapApRechaza{
		fakeRunner: fakeRunner{outs: map[string]string{CmdIwDev: iwDevAsuswrt}},
		fail: map[string]bool{
			CmdScanIface("wl1.1"): true,
			CmdScanIface("eth6"):  true,
			CmdScanIface("wl0.1"): true,
		},
	}
	p := NewProber(run, Options{})
	pl := p.Build(context.Background(), "rt-ax92u", "test")
	if pl.Data.Wireless == nil {
		t.Fatal("sección wireless ausente")
	}
	w := pl.Data.Wireless
	if len(w.Scans) != 0 {
		t.Fatalf("sin respuesta no hay vecinos: %+v", w.Scans)
	}
	if len(w.ScanOutcomes) != 2 {
		t.Fatalf("un outcome por radio: %+v", w.ScanOutcomes)
	}
	for _, o := range w.ScanOutcomes {
		if o.Error == "" {
			t.Fatalf("el motivo debe quedar registrado: %+v", o)
		}
	}
}

// TestShellRunnerExponeElStderr cubre la otra mitad del issue: el motivo lo
// escribe iw en stderr y Output() lo descartaba.
func TestShellRunnerExponeElStderr(t *testing.T) {
	_, err := ShellRunner{}.Run(context.Background(),
		"printf 'command failed: Operation not supported (-95)\\n' >&2; exit 1", 5*time.Second)
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if !strings.Contains(err.Error(), "Operation not supported (-95)") {
		t.Fatalf("el error debe llevar el motivo del stderr: %v", err)
	}
}
