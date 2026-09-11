package probe

// scan_throttle_test.go — #591: el scan pasivo de vecinos (`iw dev scan`)
// saca la radio del canal y degrada a los clientes si corre en cada push
// (~30 s). No debe repetirse dentro de scanMinInterval salvo que el server
// pida un refresh explícito (ForceScan).

import (
	"context"
	"testing"
	"time"
)

// countingRunner cuenta las invocaciones de CmdScan sobre un fakeRunner.
type countingRunner struct {
	fakeRunner
	scanCalls int
}

func (r *countingRunner) Run(ctx context.Context, cmd string, timeout time.Duration) (string, error) {
	if cmd == CmdScan {
		r.scanCalls++
	}
	return r.fakeRunner.Run(ctx, cmd, timeout)
}

// newScanProber devuelve un Prober cuyo runner solo responde a CmdScan (el
// resto de sondas falla y Build sigue sin scans/radios: suficiente para
// probar el throttle).
func newScanProber() (*Prober, *countingRunner) {
	run := &countingRunner{fakeRunner: fakeRunner{outs: map[string]string{
		CmdScan: "==IFACE==wlan0\nBSS 00:11:22:33:44:55\n\tfreq: 2412\n\tsignal: -45.00 dBm\n\tSSID: test\n",
	}}}
	return NewProber(run, Options{}), run
}

func TestScanThrottleNoRepiteDentroDelIntervalo(t *testing.T) {
	p, run := newScanProber()
	ctx := context.Background()

	p.Build(ctx, "rt2", "test")
	if run.scanCalls != 1 {
		t.Fatalf("primera Build debería escanear (arranque), scanCalls=%d", run.scanCalls)
	}
	p.Build(ctx, "rt2", "test")
	if run.scanCalls != 1 {
		t.Fatalf("segunda Build dentro del intervalo NO debería escanear, scanCalls=%d", run.scanCalls)
	}
}

func TestScanThrottleForceScanDelServer(t *testing.T) {
	p, run := newScanProber()
	ctx := context.Background()

	p.Build(ctx, "rt2", "test") // arranque: 1 scan
	p.ForceScan()
	p.Build(ctx, "rt2", "test") // refresh del server: 2º scan sin esperar al intervalo
	if run.scanCalls != 2 {
		t.Fatalf("ForceScan debería escanear en el siguiente Build, scanCalls=%d", run.scanCalls)
	}
	p.Build(ctx, "rt2", "test") // y de nuevo dentro del intervalo: no escanea
	if run.scanCalls != 2 {
		t.Fatalf("Build tras ForceScan dentro del intervalo no debería escanear, scanCalls=%d", run.scanCalls)
	}
}

func TestScanThrottleExpiraIntervalo(t *testing.T) {
	p, run := newScanProber()
	ctx := context.Background()

	p.Build(ctx, "rt2", "test") // arranque: 1 scan
	// Envejece el último scan más allá del intervalo efectivo.
	p.scanMu.Lock()
	p.scanInterval = 10 * time.Minute
	p.lastScanAt = time.Now().Add(-(p.scanInterval + time.Minute))
	p.scanMu.Unlock()

	p.Build(ctx, "rt2", "test")
	if run.scanCalls != 2 {
		t.Fatalf("pasado scanInterval debería volver a escanear, scanCalls=%d", run.scanCalls)
	}
}

// #699: ScanDisabled no escanea NUNCA por intervalo (ni siquiera al
// arrancar); solo ForceScan dispara el scan.
func TestScanDisabledSoloOnDemand(t *testing.T) {
	p, run := newScanProber()
	p.scanMu.Lock()
	p.scanInterval = ScanDisabled
	p.scanMu.Unlock()
	ctx := context.Background()

	p.Build(ctx, "rt2", "test")
	if run.scanCalls != 0 {
		t.Fatalf("ScanDisabled no debería escanear al arrancar, scanCalls=%d", run.scanCalls)
	}
	p.ForceScan()
	p.Build(ctx, "rt2", "test")
	if run.scanCalls != 1 {
		t.Fatalf("ForceScan con ScanDisabled debería escanear, scanCalls=%d", run.scanCalls)
	}
	p.Build(ctx, "rt2", "test")
	if run.scanCalls != 1 {
		t.Fatalf("Build tras ForceScan con ScanDisabled no debería reescanear, scanCalls=%d", run.scanCalls)
	}
}

// #699: NewProber aplica DefaultScanInterval cuando Options.ScanInterval es
// cero (compatibilidad con quien no configura nada).
func TestScanIntervalDefaultCuandoCero(t *testing.T) {
	p := NewProber(&fakeRunner{}, Options{})
	p.scanMu.Lock()
	got := p.scanInterval
	p.scanMu.Unlock()
	if got != DefaultScanInterval {
		t.Fatalf("ScanInterval 0 debería resolver a DefaultScanInterval, got=%v", got)
	}
	p2 := NewProber(&fakeRunner{}, Options{ScanInterval: 15 * time.Minute})
	p2.scanMu.Lock()
	got2 := p2.scanInterval
	p2.scanMu.Unlock()
	if got2 != 15*time.Minute {
		t.Fatalf("ScanInterval configurado debería respetarse, got=%v", got2)
	}
}
