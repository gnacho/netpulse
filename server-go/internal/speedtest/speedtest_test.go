package speedtest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func openStore(t *testing.T) (*Store, *Scheduler) {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	st, err := NewStore(d.DB)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return st, NewScheduler(st, d.DB, fakeRunner{})
}

func TestStoreInsertLatestHistoryPrune(t *testing.T) {
	st, _ := openStore(t)
	base := time.Now()
	for i := 0; i < 3; i++ {
		ping := float64(10 + i)
		if err := st.Insert(Result{
			TS: base.Add(time.Duration(i) * time.Hour), DownMbps: 100, UpMbps: 30,
			PingMs: &ping, ServerName: "srv", Origin: "scheduled",
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	last, err := st.Latest()
	if err != nil || last == nil {
		t.Fatalf("latest: %v %v", last, err)
	}
	if last.TS.Sub(base) < 2*time.Hour-time.Second {
		t.Fatalf("latest no es el más reciente: %v", last.TS)
	}
	hist, err := st.History(base.Add(-time.Minute), base.Add(3*time.Hour))
	if err != nil || len(hist) != 3 {
		t.Fatalf("history: %v %d", err, len(hist))
	}
	if hist[0].PingMs == nil || *hist[0].PingMs != 10 {
		t.Fatalf("ping no persistió como nullable: %v", hist[0].PingMs)
	}
	// Prune con cutoff posterior a todo → tabla vacía.
	if err := st.PruneBefore(base.Add(3 * time.Hour)); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if last, _ := st.Latest(); last != nil {
		t.Fatalf("prune no vació la tabla")
	}
}

func TestSettingsRoundtripAndValidation(t *testing.T) {
	_, sched := openStore(t)
	got := sched.LoadSettings()
	if got.Enabled || got.IntervalHours != DefaultIntervalHours {
		t.Fatalf("defaults inesperados: %+v", got)
	}
	if err := sched.SaveSettings(Settings{Enabled: true, IntervalHours: 24, ServerID: 1234, AlertPct: 60}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got = sched.LoadSettings()
	if !got.Enabled || got.IntervalHours != 24 || got.ServerID != 1234 || got.AlertPct != 60 {
		t.Fatalf("roundtrip: %+v", got)
	}
	if err := sched.SaveSettings(Settings{IntervalHours: 5}); err == nil {
		t.Fatalf("intervalo inválido aceptado")
	}
	if err := sched.SaveSettings(Settings{IntervalHours: 12, AlertPct: 95}); err == nil {
		t.Fatalf("alertPct fuera de rango aceptado")
	}
	// Claves corruptas → defaults, sin error.
	if _, err := sched.db.Exec("UPDATE kv SET value='garbage' WHERE key=?", kvInterval); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if got := sched.LoadSettings(); got.IntervalHours != DefaultIntervalHours {
		t.Fatalf("valor corrupto no cayó al default: %d", got.IntervalHours)
	}
}

func TestTickRunsWhenDueAndNotBefore(t *testing.T) {
	st, sched := openStore(t)
	runner := &countingRunner{}
	sched.runner = runner
	sched.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	if err := sched.SaveSettings(Settings{Enabled: true, IntervalHours: 6}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Sin resultados previos: el tick ejecuta (primera activación).
	sched.tick()
	if runner.calls() != 1 {
		t.Fatalf("primer tick no ejecutó: %d", runner.calls())
	}
	// Con el último reciente: no debe volver a ejecutar.
	sched.tick()
	if runner.calls() != 1 {
		t.Fatalf("ejecutó antes del intervalo: %d", runner.calls())
	}
	// Con now avanzado 7h: ejecuta de nuevo.
	sched.now = func() time.Time { return time.Unix(1_700_000_000, 0).Add(7 * time.Hour) }
	sched.tick()
	if runner.calls() != 2 {
		t.Fatalf("no ejecutó al vencer: %d", runner.calls())
	}
	if last, _ := st.Latest(); last == nil || last.Origin != "scheduled" {
		t.Fatalf("serie sin resultado programado: %+v", last)
	}
	// Desactivado: nunca ejecuta.
	if err := sched.SaveSettings(Settings{Enabled: false, IntervalHours: 6}); err != nil {
		t.Fatalf("save off: %v", err)
	}
	sched.tick()
	if runner.calls() != 2 {
		t.Fatalf("ejecutó estando desactivado: %d", runner.calls())
	}
}

func TestRunNowSingleFlight(t *testing.T) {
	_, sched := openStore(t)
	runner := &blockingRunner{release: make(chan struct{})}
	sched.runner = runner
	if err := sched.RunNow(); err != nil {
		t.Fatalf("runnow: %v", err)
	}
	if err := sched.RunNow(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("segundo RunNow no devolvió ErrAlreadyRunning: %v", err)
	}
	close(runner.release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sched.Status().Running == false && sched.Status().Last != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	st := sched.Status()
	if st.Running || st.Last == nil || st.Last.Origin != "manual" {
		t.Fatalf("estado tras runnow: %+v", st)
	}
}

func TestMaybeAlertDebounce(t *testing.T) {
	_, sched := openStore(t)
	em := &fakeEmitter{}
	sched.emit = em
	sched.contractDown = func() (float64, bool) { return 300, true }
	pct := 50
	// Sin umbral configurado → nunca.
	sched.maybeAlert(Settings{AlertPct: 0}, Result{DownMbps: 10})
	if em.count() != 0 {
		t.Fatalf("alertó con AlertPct=0")
	}
	low := Settings{AlertPct: pct}
	ok := Result{DownMbps: 200}
	bad := Result{DownMbps: 100}
	// Episodio: primera caída alerta, las siguientes no.
	sched.maybeAlert(low, bad)
	sched.maybeAlert(low, bad)
	if em.count() != 1 {
		t.Fatalf("debounce por episode falló: %d", em.count())
	}
	// Recuperación + nueva caída → segunda alerta.
	sched.maybeAlert(low, ok)
	sched.maybeAlert(low, bad)
	if em.count() != 2 {
		t.Fatalf("no re-alertó tras recuperación: %d", em.count())
	}
	// Sin plan declarado → no alerta (y resetea el estado).
	sched.contractDown = func() (float64, bool) { return 0, false }
	sched.maybeAlert(low, bad)
	if em.count() != 2 {
		t.Fatalf("alertó sin plan contratado")
	}
	// La alerta lleva hint y categoría internet.
	if em.last.Hint != alerts.HintFor(alerts.HintWanSlow) || em.last.Category != alerts.CatInternet {
		t.Fatalf("evento mal formado: %+v", em.last)
	}
}

// fakeRunner devuelve un resultado fijo sin red.
type fakeRunner struct{}

func (fakeRunner) Run(ctx context.Context, serverID int) (Result, error) {
	return Result{DownMbps: 100, UpMbps: 30, ServerName: "fake"}, nil
}

type countingRunner struct {
	mu    sync.Mutex
	n     int
	inner fakeRunner
}

func (c *countingRunner) Run(ctx context.Context, serverID int) (Result, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.Run(ctx, serverID)
}

func (c *countingRunner) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

type blockingRunner struct {
	release chan struct{}
}

func (b *blockingRunner) Run(ctx context.Context, serverID int) (Result, error) {
	<-b.release
	return Result{DownMbps: 50, UpMbps: 10, ServerName: "blocked"}, nil
}

type fakeEmitter struct {
	mu   sync.Mutex
	n    int
	last alerts.AlertEvent
}

func (f *fakeEmitter) Emit(ev alerts.AlertEvent) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.last = ev
	return true
}

func (f *fakeEmitter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}
