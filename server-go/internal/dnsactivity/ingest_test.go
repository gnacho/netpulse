// ingest_test.go — ciclo de ingesta del query log (#2078).
package dnsactivity

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// testDB levanta el esquema mínimo que toca la ingesta.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	d.SetMaxOpenConns(1)
	schema := `
CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE IF NOT EXISTS device_seen (
  mac TEXT PRIMARY KEY, first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL,
  name TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '',
  device_type TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS dns_activity (
  mac TEXT NOT NULL, domain TEXT NOT NULL, hour_ts INTEGER NOT NULL,
  queries INTEGER NOT NULL DEFAULT 0, blocked INTEGER NOT NULL DEFAULT 0,
  first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL,
  PRIMARY KEY (mac, domain, hour_ts)
);
CREATE TABLE IF NOT EXISTS dns_domain_first (
  domain TEXT PRIMARY KEY, first_seen INTEGER NOT NULL, mac TEXT NOT NULL DEFAULT ''
);`
	if _, err := d.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO device_seen (mac, first_seen, last_seen, ip) VALUES
		('aa:bb:cc:dd:ee:01', 0, 0, '192.168.1.42'),
		('aa:bb:cc:dd:ee:02', 0, 0, '192.168.1.43')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return d
}

// fakeSource sirve páginas en orden y permite fijar config/error.
type fakeSource struct {
	seq  []*adapters.QueryLogPage
	i    int
	cfg  *adapters.QueryLogConfig
	err  error
	cfgE error
}

func (f *fakeSource) QueryLog(_ context.Context, _ string, _ int) (*adapters.QueryLogPage, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.i >= len(f.seq) {
		return &adapters.QueryLogPage{Queries: []adapters.DNSQuery{}}, nil
	}
	p := f.seq[f.i]
	f.i++
	return p, nil
}

func (f *fakeSource) QueryLogConfig(context.Context) (*adapters.QueryLogConfig, error) {
	if f.cfgE != nil {
		return nil, f.cfgE
	}
	if f.cfg == nil {
		return &adapters.QueryLogConfig{Enabled: true}, nil
	}
	return f.cfg, nil
}

func q(ts string, ip, domain string, blocked bool) adapters.DNSQuery {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	reason := "NotFilteredNotFound"
	if blocked {
		reason = "FilteredBlackList"
	}
	return adapters.DNSQuery{Time: t, ClientIP: ip, Domain: domain, Reason: reason}
}

func newTestIngestor(t *testing.T, src *fakeSource, now time.Time) (*Ingestor, *sql.DB) {
	t.Helper()
	d := testDB(t)
	st := NewStore(d)
	ing := NewIngestor(func() Source {
		if src == nil {
			return nil
		}
		return src
	}, st, d)
	ing.SetNow(func() time.Time { return now })
	return ing, d
}

func TestIngestFirstRunAggregatesAndAttributes(t *testing.T) {
	now := time.Date(2026, 11, 16, 11, 0, 30, 0, time.UTC)
	src := &fakeSource{seq: []*adapters.QueryLogPage{{
		Oldest: time.Date(2026, 11, 16, 10, 30, 0, 0, time.UTC),
		Queries: []adapters.DNSQuery{
			q("2026-11-16T10:59:00Z", "192.168.1.42", "a.example", false),
			q("2026-11-16T10:59:10Z", "192.168.1.42", "a.example", true),
			q("2026-11-16T10:59:20Z", "192.168.1.43", "b.example", false),
			q("2026-11-16T10:59:30Z", "192.168.1.99", "c.example", false), // sin atribuir
		},
	}}}
	ing, d := newTestIngestor(t, src, now)

	res, err := ing.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if res.State != StateOK || res.Queries != 4 {
		t.Fatalf("res = %+v", res)
	}

	var rows, queries, blocked int
	if err := d.QueryRow(`SELECT COUNT(*), SUM(queries), SUM(blocked) FROM dns_activity`).Scan(&rows, &queries, &blocked); err != nil {
		t.Fatal(err)
	}
	if rows != 3 {
		t.Errorf("filas = %d, want 3 (MAC+dominio+hora)", rows)
	}
	if queries != 4 || blocked != 1 {
		t.Errorf("queries/blocked = %d/%d", queries, blocked)
	}

	var unattributed int
	_ = d.QueryRow(`SELECT queries FROM dns_activity WHERE mac = '' AND domain = 'c.example'`).Scan(&unattributed)
	if unattributed != 1 {
		t.Errorf("sin atribuir = %d, want 1", unattributed)
	}

	var cursor string
	_ = d.QueryRow(`SELECT value FROM kv WHERE key = ?`, kvCursor).Scan(&cursor)
	if cursor == "" {
		t.Error("cursor vacío tras la ingesta")
	}
}

func TestIngestSecondTickSumsSameHour(t *testing.T) {
	now := time.Date(2026, 11, 16, 11, 5, 0, 0, time.UTC)
	src := &fakeSource{seq: []*adapters.QueryLogPage{
		{Oldest: time.Date(2026, 11, 16, 10, 40, 0, 0, time.UTC), Queries: []adapters.DNSQuery{
			q("2026-11-16T10:59:00Z", "192.168.1.42", "a.example", false),
		}},
		{Oldest: time.Date(2026, 11, 16, 10, 59, 0, 0, time.UTC), Queries: []adapters.DNSQuery{
			q("2026-11-16T11:04:00Z", "192.168.1.42", "a.example", false),
		}},
	}}
	ing, d := newTestIngestor(t, src, now)

	if _, err := ing.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := d.QueryRow(`SELECT SUM(queries) FROM dns_activity WHERE mac = 'aa:bb:cc:dd:ee:01'`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("queries = %d, want 2 (upsert aditivo)", total)
	}
}

func TestIngestHonoursDisabledAndAnonymized(t *testing.T) {
	now := time.Date(2026, 11, 16, 11, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		cfg  *adapters.QueryLogConfig
		want State
	}{
		{"disabled", &adapters.QueryLogConfig{Enabled: false}, StateDisabled},
		{"anonymized", &adapters.QueryLogConfig{Enabled: true, AnonymizeClientIP: true}, StateAnonymize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeSource{cfg: tc.cfg}
			ing, d := newTestIngestor(t, src, now)
			res, err := ing.Tick(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if res.State != tc.want {
				t.Errorf("state = %q, want %q", res.State, tc.want)
			}
			var rows int
			_ = d.QueryRow(`SELECT COUNT(*) FROM dns_activity`).Scan(&rows)
			if rows != 0 {
				t.Errorf("filas = %d, no debería ingerir con %s", rows, tc.name)
			}
		})
	}
}

func TestIngestDeclaresGapWhenLogRotated(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{
		cfg: &adapters.QueryLogConfig{Enabled: true},
		seq: []*adapters.QueryLogPage{{
			// Lo más antiguo disponible es 10:00 pero el cursor estará en 09:00:
			// AdGuard rotó el log y falta una hora.
			Oldest: time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC),
			Queries: []adapters.DNSQuery{
				q("2026-11-16T11:30:00Z", "192.168.1.42", "a.example", false),
			},
		}},
	}
	ing, d := newTestIngestor(t, src, now)
	if _, err := d.Exec(`INSERT INTO kv (key, value) VALUES (?, ?)`,
		kvCursor, "2026-11-16T09:00:00Z"); err != nil {
		t.Fatal(err)
	}

	res, err := ing.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Gap {
		t.Error("Gap = false, want true (log rotado)")
	}
	if GapSince(context.Background(), d) == 0 {
		t.Error("gap no persistido")
	}
}

func TestIngestNoSourceMarksUnavailable(t *testing.T) {
	ing, d := newTestIngestor(t, nil, time.Now())
	res, err := ing.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateNoConf {
		t.Errorf("state = %q", res.State)
	}
	if st, _ := StateOf(context.Background(), d); st != StateNoConf {
		t.Errorf("estado persistido = %q", st)
	}
}

func TestIngestErrorKeepsState(t *testing.T) {
	src := &fakeSource{err: context.DeadlineExceeded}
	ing, _ := newTestIngestor(t, src, time.Now())
	res, err := ing.Tick(context.Background())
	if err == nil {
		t.Fatal("esperaba error")
	}
	if res.State != StateError {
		t.Errorf("state = %q", res.State)
	}
}

func TestIngestResetsCursorWhenInstanceChanges(t *testing.T) {
	now := time.Date(2026, 11, 16, 11, 0, 30, 0, time.UTC)
	src := &fakeSource{seq: []*adapters.QueryLogPage{
		{Oldest: time.Date(2026, 11, 16, 10, 30, 0, 0, time.UTC), Queries: []adapters.DNSQuery{
			q("2026-11-16T10:59:00Z", "192.168.1.42", "a.example", false),
		}},
		{Oldest: time.Date(2026, 11, 16, 10, 45, 0, 0, time.UTC), Queries: []adapters.DNSQuery{
			q("2026-11-16T10:58:00Z", "192.168.1.42", "a.example", false),
		}},
	}}
	ing, d := newTestIngestor(t, src, now)
	if _, err := d.Exec(`INSERT INTO kv (key, value) VALUES (?, ?)`, kvConfigHost, "adguard.lan"); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Otra instancia: cursor a cero, primera lectura de una hora hacia atrás.
	// La consulta de 10:58 es más antigua que el cursor de la instancia previa
	// (10:59), así que solo entra si el cursor se reseteó.
	if _, err := d.Exec(`UPDATE kv SET value = 'otra.lan' WHERE key = ?`, kvConfigHost); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	var host string
	_ = d.QueryRow(`SELECT value FROM kv WHERE key = ?`, kvHost).Scan(&host)
	if host != "otra.lan" {
		t.Errorf("host = %q, want otra.lan", host)
	}
	// Gate del reset: sin él, la entrada de 10:58 quedaría por debajo del
	// cursor anterior y no se ingeriría.
	var total int
	_ = d.QueryRow(`SELECT COALESCE(SUM(queries),0) FROM dns_activity WHERE domain = 'a.example'`).Scan(&total)
	if total != 2 {
		t.Errorf("queries = %d, want 2 (la segunda instancia reingiere su hora)", total)
	}
}
