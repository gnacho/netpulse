package wgseries

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = d.Close() })
	s, err := NewStore(d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sample(key string, rx, tx int64) PeerSample {
	return PeerSample{Key: key, RxBytes: rx, TxBytes: tx}
}

func lastDelta(t *testing.T, s *Store, router, peer string) (int64, int64) {
	t.Helper()
	var drx, dtx int64
	err := s.db.QueryRow(`SELECT d_rx, d_tx FROM wg_peer_series_raw
		WHERE router_id = ? AND peer_key = ? ORDER BY ts DESC LIMIT 1`, router, peer).Scan(&drx, &dtx)
	if err != nil {
		t.Fatalf("sin muestra para %s: %v", peer, err)
	}
	return drx, dtx
}

// La primera muestra de un peer solo abre la ventana: el acumulado previo es
// anterior a nuestra observación y contarlo sería inventar tráfico.
func TestFirstSampleOpensTheWindow(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	if err := s.RecordSamples("rt1", now, []PeerSample{sample("k1", 5000, 7000)}); err != nil {
		t.Fatal(err)
	}
	if drx, dtx := lastDelta(t, s, "rt1", "k1"); drx != 0 || dtx != 0 {
		t.Fatalf("primera muestra delta = (%d,%d); want (0,0)", drx, dtx)
	}
	tot, err := s.Window("rt1", now, DefaultWindowDays)
	if err != nil {
		t.Fatal(err)
	}
	if tot.Rx != 0 || tot.Tx != 0 {
		t.Fatalf("ventana con una sola muestra = %+v; want ceros", tot)
	}
	if tot.First.IsZero() {
		t.Fatal("First debe marcar el inicio del histórico")
	}
}

func TestDeltasAccumulate(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	steps := []struct {
		at     time.Time
		rx, tx int64
	}{
		{now.Add(-3 * time.Minute), 1000, 2000},
		{now.Add(-2 * time.Minute), 1500, 2600},
		{now.Add(-1 * time.Minute), 1520, 2700},
	}
	for _, st := range steps {
		if err := s.RecordSamples("rt1", st.at, []PeerSample{sample("k1", st.rx, st.tx)}); err != nil {
			t.Fatal(err)
		}
	}
	if drx, dtx := lastDelta(t, s, "rt1", "k1"); drx != 20 || dtx != 100 {
		t.Fatalf("último delta = (%d,%d); want (20,100)", drx, dtx)
	}
	tot, err := s.Window("rt1", now, DefaultWindowDays)
	if err != nil {
		t.Fatal(err)
	}
	if tot.Rx != 520 || tot.Tx != 700 {
		t.Fatalf("ventana = (%d,%d); want (520,700)", tot.Rx, tot.Tx)
	}
}

// Un contador que baja significa que el peer o la interfaz se recrearon: se
// cuenta desde cero y no se pierde lo posterior al reinicio.
func TestResetCountsFromZero(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	_ = s.RecordSamples("rt1", now.Add(-2*time.Minute), []PeerSample{sample("k1", 10000, 10000)})
	_ = s.RecordSamples("rt1", now.Add(-1*time.Minute), []PeerSample{sample("k1", 400, 250)})
	if drx, dtx := lastDelta(t, s, "rt1", "k1"); drx != 400 || dtx != 250 {
		t.Fatalf("delta tras reset = (%d,%d); want (400,250)", drx, dtx)
	}
}

// Un peer que desaparece no cierra nada por su cuenta (el acumulado ya se
// contó). Si vuelve con el contador más alto tras un hueco, el delta cubre el
// hueco entero, que es lo que se transfirió mientras no mirábamos.
func TestPeerGapThenReturnCountsTheGap(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	_ = s.RecordSamples("rt1", now.Add(-30*time.Minute), []PeerSample{sample("k1", 1000, 1000)})
	// 20 minutos sin que el peer aparezca en el dump.
	_ = s.RecordSamples("rt1", now.Add(-10*time.Minute), []PeerSample{sample("k2", 5, 5)})
	_ = s.RecordSamples("rt1", now.Add(-1*time.Minute), []PeerSample{sample("k1", 9000, 3000)})
	if drx, dtx := lastDelta(t, s, "rt1", "k1"); drx != 8000 || dtx != 2000 {
		t.Fatalf("delta tras hueco = (%d,%d); want (8000,2000)", drx, dtx)
	}
}

// Y si vuelve recreado (contador a cero), no se resta nada: cuenta lo nuevo.
func TestPeerRecreatedCountsFromZero(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	_ = s.RecordSamples("rt1", now.Add(-2*time.Minute), []PeerSample{sample("k1", 9000, 9000)})
	_ = s.RecordSamples("rt1", now.Add(-1*time.Minute), []PeerSample{sample("k1", 120, 80)})
	if drx, dtx := lastDelta(t, s, "rt1", "k1"); drx != 120 || dtx != 80 {
		t.Fatalf("delta tras recrear = (%d,%d); want (120,80)", drx, dtx)
	}
}

// El rollup consolida solo días cerrados, así que la ventana (daily + raw de
// hoy) no cuenta dos veces el mismo tráfico.
func TestRollupAndWindowDoNotOverlap(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	// Dos muestras de ayer y dos de hoy.
	_ = s.RecordSamples("rt1", time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 10, 0, 0, 0, now.Location()), []PeerSample{sample("k1", 1000, 1000)})
	_ = s.RecordSamples("rt1", time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 11, 0, 0, 0, now.Location()), []PeerSample{sample("k1", 3000, 2000)})
	_ = s.RecordSamples("rt1", now.Add(-2*time.Minute), []PeerSample{sample("k1", 5000, 4000)})
	_ = s.RecordSamples("rt1", now.Add(-1*time.Minute), []PeerSample{sample("k1", 7000, 6000)})

	if err := s.RollupRawToDaily(7 * 24 * time.Hour); err != nil {
		t.Fatal(err)
	}
	var dailyRx int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(rx_bytes),0) FROM wg_peer_series_daily WHERE router_id = 'rt1'`).Scan(&dailyRx); err != nil {
		t.Fatal(err)
	}
	if dailyRx != 2000 {
		t.Fatalf("daily de ayer = %d; want 2000 (solo el delta de ayer)", dailyRx)
	}
	tot, err := s.Window("rt1", now, DefaultWindowDays)
	if err != nil {
		t.Fatal(err)
	}
	// Ayer 2000 + hoy 4000 (3000->5000 y 5000->7000), sin solape.
	if tot.Rx != 6000 {
		t.Fatalf("ventana rx = %d; want 6000", tot.Rx)
	}
	if got, want := tot.Peers, 1; got != want {
		t.Fatalf("peers = %d; want %d", got, want)
	}
}

// La ventana ignora lo que queda fuera de los días pedidos, pero el delta que
// cubre un hueco se atribuye a la muestra que lo cierra: si el peer pasa de
// 900 a 1000 tras 40 días sin mirar, esos 100 entran en la ventana porque no
// hay forma de repartirlos entre días que no se observaron.
func TestWindowRespectsDays(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	old := now.AddDate(0, 0, -40)
	_ = s.RecordSamples("rt1", old, []PeerSample{sample("k1", 100, 100)})
	_ = s.RecordSamples("rt1", old.Add(time.Minute), []PeerSample{sample("k1", 900, 900)})
	_ = s.RecordSamples("rt1", now.Add(-1*time.Minute), []PeerSample{sample("k1", 1000, 1000)})
	if err := s.RollupRawToDaily(400 * 24 * time.Hour); err != nil {
		t.Fatal(err)
	}
	tot, err := s.Window("rt1", now, DefaultWindowDays)
	if err != nil {
		t.Fatal(err)
	}
	if tot.Rx != 100 {
		t.Fatalf("ventana rx = %d; want 100 (el día de hace 40 no entra, el delta del hueco sí)", tot.Rx)
	}
}

func TestEmptyWindow(t *testing.T) {
	s := newStore(t)
	tot, err := s.Window("rt1", time.Now(), DefaultWindowDays)
	if err != nil {
		t.Fatal(err)
	}
	if tot.Rx != 0 || tot.Tx != 0 || !tot.First.IsZero() {
		t.Fatalf("ventana vacía = %+v; want ceros y First zero", tot)
	}
	n, err := s.RecentSamples("rt1", time.Now().Add(-time.Hour))
	if err != nil || n != 0 {
		t.Fatalf("RecentSamples = (%d,%v); want (0,nil)", n, err)
	}
}

func TestPurgeRaw(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	_ = s.RecordSamples("rt1", now.AddDate(0, 0, -10), []PeerSample{sample("k1", 10, 10)})
	_ = s.RecordSamples("rt1", now, []PeerSample{sample("k1", 20, 20)})
	n, err := s.PurgeRaw()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purgadas = %d; want 1", n)
	}
}

func TestRecordSamplesSkipsEmptyKey(t *testing.T) {
	s := newStore(t)
	if err := s.RecordSamples("rt1", time.Now(), []PeerSample{{Key: "", RxBytes: 10}}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM wg_peer_series_raw`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("filas = %d; want 0", n)
	}
}

func TestDelta(t *testing.T) {
	cases := []struct{ prev, cur, want int64 }{
		{0, 100, 100},
		{100, 250, 150},
		{100, 100, 0},
		{100, 40, 40}, // reset
		{100, 0, 0},   // contador a cero, nada que contar
		{-1, 30, 30},  // sin muestra previa
	}
	for _, c := range cases {
		if got := delta(c.prev, c.cur); got != c.want {
			t.Fatalf("delta(%d,%d) = %d; want %d", c.prev, c.cur, got, c.want)
		}
	}
}
