package probe

// scan_periface_test.go — #1338: el scan de vecinos se lanzaba como UN solo
// comando (`iw dev` + bucle sobre TODAS las interfaces) con UN solo timeout
// de 10 s. Si una interfaz escanea lento (p. ej. una radio 5 GHz en canal
// DFS, scan pasivo canal a canal), el timeout mataba el bucle entero y la
// salida parcial —con las interfaces que SÍ habían escaneado— se descartaba:
// la unidad no reportaba NINGÚN vecino (UniFi 6 Lite, mt7621). Ahora el scan
// va por interfaz, cada una con su propio presupuesto de tiempo: una que
// falla o se cuelga deja su sección vacía y no arrastra a las demás.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// runnerScanParcial simula una unidad con dos radios donde el scan de la
// primera interfaz (5 GHz) muere por timeout y la segunda (2.4 GHz) devuelve
// un vecino real.
type runnerScanParcial struct {
	fakeRunner
}

func (r runnerScanParcial) Run(ctx context.Context, cmd string, timeout time.Duration) (string, error) {
	if cmd == CmdScanIface("phy1-ap0") {
		return "", &fakeErr{cmd} // timeout del runner real: err y sin salida
	}
	return r.fakeRunner.Run(ctx, cmd, timeout)
}

// TestScanPorInterfazSobreviveAFalloParcial: con el scan de la primera
// interfaz fallando, los vecinos de la segunda deben llegar igual.
func TestScanPorInterfazSobreviveAFalloParcial(t *testing.T) {
	iwDev := "phy#1\n" +
		"\tInterface phy1-ap0\n" +
		"\t\taddr 62:22:32:5b:5c:99\n" +
		"\t\tssid I1_LAN\n" +
		"\t\ttype AP\n" +
		"\t\tchannel 60 (5300 MHz), width: 80 MHz, center1: 5290 MHz\n" +
		"phy#0\n" +
		"\tInterface phy0-ap0\n" +
		"\t\taddr 60:22:32:5b:5c:98\n" +
		"\t\tssid I1_LAN\n" +
		"\t\ttype AP\n" +
		"\t\tchannel 5 (2432 MHz), width: 20 MHz, center1: 2432 MHz\n"
	// Salida real del scan en la U6 Lite del reportero (#1338), con las dos
	// líneas "last seen" (la de cabecera con [boottime] y la de señal).
	scan2g := "BSS c4:eb:39:0c:2b:c1(on phy0-ap0)\n" +
		"\tlast seen: 275247.799s [boottime]\n" +
		"\tTSF: 901954671988 usec (10d, 10:32:34)\n" +
		"\tfreq: 2437.0\n" +
		"\tbeacon interval: 100 TUs\n" +
		"\tcapability: ESS (0x1011)\n" +
		"\tsignal: -67.00 dBm\n" +
		"\tlast seen: 2140 ms ago\n" +
		"\tSSID: Vecino_24\n"
	run := runnerScanParcial{fakeRunner{outs: map[string]string{
		CmdIwDev:                 iwDev,
		CmdScanIface("phy0-ap0"): scan2g,
	}}}
	p := NewProber(run, Options{})

	pl := p.Build(context.Background(), "u6lite", "test")
	if pl.Data.Wireless == nil {
		t.Fatal("sección wireless ausente")
	}
	scans := pl.Data.Wireless.Scans
	if len(scans) != 1 {
		t.Fatalf("el fallo del scan 5 GHz no debe arrastrar al 2.4 GHz: %+v", scans)
	}
	sc := scans[0]
	if sc.Iface != "phy0-ap0" || sc.BSSID != "C4:EB:39:0C:2B:C1" || sc.SSID != "Vecino_24" || sc.Channel != 6 || sc.Signal != -67 {
		t.Fatalf("vecino mal parseado: %+v", sc)
	}
	// Los BSSIDs propios salen del MISMO `iw dev` (sin segundo spawn).
	if len(pl.Data.Wireless.OwnBssids) != 2 {
		t.Fatalf("ownBssids perdidos: %+v", pl.Data.Wireless.OwnBssids)
	}
}

// TestScanPorInterfazSinIw: sin `iw` en el equipo, ni scans ni BSSIDs propios
// (best-effort, como antes).
func TestScanPorInterfazSinIw(t *testing.T) {
	p := NewProber(fakeRunner{outs: map[string]string{}}, Options{})
	pl := p.Build(context.Background(), "sin-iw", "test")
	if pl.Data.Wireless != nil && len(pl.Data.Wireless.Scans) != 0 {
		t.Fatalf("sin iw no puede haber scans: %+v", pl.Data.Wireless.Scans)
	}
}

// TestCmdScanIface: el comando por interfaz cita el nombre (como hacía el
// bucle viejo con "$i").
func TestCmdScanIface(t *testing.T) {
	cmd := CmdScanIface("phy0-ap0")
	if !strings.Contains(cmd, `iw dev "phy0-ap0" scan`) {
		t.Fatalf("comando inesperado: %q", cmd)
	}
}

// TestScanPorInterfazThrottle: el throttle (scanDue) sigue gobernando el
// scan aunque ahora se lance por interfaz: 2 interfaces = 2 comandos en la
// primera Build y 0 en la segunda (dentro del intervalo).
func TestScanPorInterfazThrottle(t *testing.T) {
	iwDev := "phy#0\n" +
		"\tInterface phy0-ap0\n\t\taddr 60:22:32:5b:5c:98\n\t\ttype AP\n" +
		"\tInterface phy0-ap1\n\t\taddr 62:22:32:5b:5c:98\n\t\ttype AP\n"
	run := &countingRunner{fakeRunner: fakeRunner{outs: map[string]string{
		CmdIwDev: iwDev,
	}}}
	p := NewProber(run, Options{})
	ctx := context.Background()

	p.Build(ctx, "rt", "test")
	if run.scanCalls != 2 {
		t.Fatalf("primera Build debería escanear las 2 interfaces, scanCalls=%d", run.scanCalls)
	}
	p.Build(ctx, "rt", "test")
	if run.scanCalls != 2 {
		t.Fatalf("segunda Build dentro del intervalo NO debería escanear, scanCalls=%d", run.scanCalls)
	}
	p.ForceScan()
	p.Build(ctx, "rt", "test")
	if run.scanCalls != 4 {
		t.Fatalf("ForceScan debería reescanear las 2 interfaces, scanCalls=%d", run.scanCalls)
	}
}
