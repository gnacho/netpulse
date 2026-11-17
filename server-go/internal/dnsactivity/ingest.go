// ingest.go — bucle de ingesta del query log (#2078).
package dnsactivity

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// Intervalo por defecto entre lecturas. El log es un buffer, no una métrica:
// un minuto da margen de sobra sin castigar a AdGuard.
const DefaultInterval = 60 * time.Second

// pageLimit es el máximo que acepta AdGuard por página.
const pageLimit = 1000

// Claves de estado en kv.
const (
	kvCursor = "adguard_querylog_cursor"
	kvState  = "adguard_querylog_state"
	kvStateT = "adguard_querylog_state_ts"
	kvGap    = "adguard_querylog_gap"
	// kvHost guarda la instancia cuyo log se está siguiendo: el cursor de una
	// instancia no vale para otra (hosts distintos, historiales distintos).
	kvHost = "adguard_querylog_host"
	// kvConfigHost es la clave de configuración de la integración.
	kvConfigHost = "adguard_host"
)

// Source es la lectura del log: la implementan el cliente estándar, el GL y
// el despachador AdGuardSource.
type Source interface {
	QueryLog(ctx context.Context, olderThan string, limit int) (*adapters.QueryLogPage, error)
	QueryLogConfig(ctx context.Context) (*adapters.QueryLogConfig, error)
}

// TickResult resume un ciclo (tests y logs).
type TickResult struct {
	State   State
	Pages   int
	Queries int
	Buckets int
	Gap     bool
}

// Ingestor lee el log y escribe el agregado.
type Ingestor struct {
	src        func() Source
	store      *Store
	db         *sql.DB
	interval   time.Duration
	maxPages   int
	now        func() time.Time
	mu         sync.Mutex
	lastResult TickResult
}

// NewIngestor construye el ingestor. src se consulta en cada tick: la
// integración se puede configurar o desactivar en caliente.
func NewIngestor(src func() Source, st *Store, d *sql.DB) *Ingestor {
	return &Ingestor{
		src: src, store: st, db: d,
		interval: DefaultInterval,
		maxPages: 20, // 20.000 entradas por tick como tope de trabajo
		now:      time.Now,
	}
}

// SetInterval ajusta la cadencia (tests).
func (i *Ingestor) SetInterval(d time.Duration) {
	if d > 0 {
		i.interval = d
	}
}

// SetMaxPages ajusta el tope de páginas por tick (tests).
func (i *Ingestor) SetMaxPages(n int) {
	if n > 0 {
		i.maxPages = n
	}
}

// SetNow inyecta el reloj (tests).
func (i *Ingestor) SetNow(fn func() time.Time) {
	if fn != nil {
		i.now = fn
	}
}

// LastResult devuelve el resumen del último tick.
func (i *Ingestor) LastResult() TickResult {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.lastResult
}

// Run ejecuta un tick inmediato y luego uno por intervalo, hasta cancelar.
func (i *Ingestor) Run(ctx context.Context) {
	i.tick(ctx)
	t := time.NewTicker(i.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			i.tick(ctx)
		}
	}
}

// tick evita solapar dos ingestas (una lenta no apila otra).
func (i *Ingestor) tick(ctx context.Context) {
	if !i.mu.TryLock() {
		return
	}
	defer i.mu.Unlock()
	res, err := i.ingest(ctx)
	if err != nil {
		log.Printf("[netpulse] querylog: %v", err)
	}
	i.lastResult = res
}

// Tick ejecuta un ciclo de forma síncrona (tests y sondeo manual).
func (i *Ingestor) Tick(ctx context.Context) (TickResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	res, err := i.ingest(ctx)
	i.lastResult = res
	return res, err
}

// ingest es el ciclo real: comprobar estado, paginar, agregar y persistir.
func (i *Ingestor) ingest(ctx context.Context) (TickResult, error) {
	src := i.src()
	if src == nil {
		i.setState(ctx, StateNoConf)
		return TickResult{State: StateNoConf}, nil
	}

	// Config: un endpoint ausente en un GL viejo no puede bloquear la ingesta.
	if cfg, err := src.QueryLogConfig(ctx); err == nil && cfg != nil {
		if !cfg.Enabled {
			i.setState(ctx, StateDisabled)
			return TickResult{State: StateDisabled}, nil
		}
		if cfg.AnonymizeClientIP {
			i.setState(ctx, StateAnonymize)
			return TickResult{State: StateAnonymize}, nil
		}
	}

	now := i.now()
	// Un cambio de instancia invalida el cursor: seguir con el de la anterior
	// mezclaría dos historiales y declararía huecos falsos.
	cfgHost := i.kvGet(ctx, kvConfigHost)
	if prev := i.kvGet(ctx, kvHost); prev != cfgHost {
		_ = i.kvSet(ctx, kvCursor, "")
		_ = i.kvSet(ctx, kvHost, cfgHost)
	}
	cursor, firstRun := i.cursor(ctx, now)

	macs, err := resolveMACs(ctx, i.db)
	if err != nil {
		return TickResult{}, fmt.Errorf("resolver MACs: %w", err)
	}

	collected, pages, gap, err := i.collect(ctx, src, cursor, firstRun)
	if err != nil {
		i.setState(ctx, StateError)
		return TickResult{State: StateError, Pages: pages}, err
	}

	buckets, firsts := aggregate(collected, macs)
	if err := i.store.Apply(ctx, buckets, firsts); err != nil {
		return TickResult{}, fmt.Errorf("escribir agregado: %w", err)
	}
	if gap {
		_ = i.kvSet(ctx, kvGap, fmt.Sprint(now.UnixMilli()))
	}

	// Cursor: el instante más reciente ingerido, o ahora si no hubo nada.
	// Arrancar en now y subir solo perdería entradas escritas por AdGuard con
	// retraso: el cursor debe ser el último dato, no la hora actual.
	var next time.Time
	for _, q := range collected {
		if q.Time.After(next) {
			next = q.Time
		}
	}
	if next.IsZero() {
		next = now
	}
	if len(collected) > 0 || firstRun {
		_ = i.kvSet(ctx, kvCursor, next.UTC().Format(time.RFC3339Nano))
	}
	i.setState(ctx, StateOK)

	return TickResult{State: StateOK, Pages: pages, Queries: len(collected), Buckets: len(buckets), Gap: gap}, nil
}

// cursor lee el último instante ingerido. En el primer arranque arranca en la
// última hora: no se reingieren los 90 días de historial de AdGuard.
func (i *Ingestor) cursor(ctx context.Context, now time.Time) (time.Time, bool) {
	v := i.kvGet(ctx, kvCursor)
	if v == "" {
		return now.Add(-time.Hour), true
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return now.Add(-time.Hour), true
	}
	return t, false
}

// collect pagina hacia atrás hasta el cursor o hasta el tope de trabajo.
//
// Declara hueco cuando AdGuard ya no tiene entradas tan antiguas como el
// cursor (rotación del log en memoria): sin ese aviso, la UI mostraría una
// continuidad que no existe.
func (i *Ingestor) collect(ctx context.Context, src Source, cursor time.Time, firstRun bool) ([]adapters.DNSQuery, int, bool, error) {
	var collected []adapters.DNSQuery
	olderThan := ""
	pages := 0
	reached := false
	var oldestAvailable time.Time

	for pages < i.maxPages {
		page, err := src.QueryLog(ctx, olderThan, pageLimit)
		if err != nil {
			return nil, pages, false, err
		}
		pages++
		if len(page.Queries) == 0 {
			break
		}
		if !page.Oldest.IsZero() {
			oldestAvailable = page.Oldest
		}
		for _, q := range page.Queries {
			if q.Time.After(cursor) {
				collected = append(collected, q)
			} else {
				reached = true
			}
		}
		if reached || page.Oldest.IsZero() || !page.Oldest.After(cursor) {
			reached = true
			break
		}
		if len(page.Queries) < pageLimit {
			break
		}
		olderThan = page.Oldest.UTC().Format(time.RFC3339)
	}

	gap := false
	if !firstRun && !reached && !oldestAvailable.IsZero() && oldestAvailable.After(cursor.Add(5*time.Minute)) {
		gap = true
	}
	if pages >= i.maxPages && !reached {
		gap = true
	}
	return collected, pages, gap, nil
}

// aggregate convierte entradas en buckets horarios y primeras apariciones.
func aggregate(queries []adapters.DNSQuery, macs map[string]string) ([]Bucket, []FirstSeen) {
	type key struct {
		mac    string
		domain string
		hour   int64
	}
	agg := map[key]*Bucket{}
	var orderKeys []key
	seenDomain := map[string]struct{}{}
	var firsts []FirstSeen

	for _, q := range queries {
		mac := macs[canonicalIP(q.ClientIP)]
		hour := q.Time.Truncate(time.Hour)
		k := key{mac: mac, domain: q.Domain, hour: hour.UnixMilli()}
		b := agg[k]
		if b == nil {
			b = &Bucket{MAC: mac, Domain: q.Domain, Hour: hour, FirstSeen: q.Time, LastSeen: q.Time}
			agg[k] = b
			orderKeys = append(orderKeys, k)
		}
		b.Queries++
		if q.BlockedByFilter() {
			b.Blocked++
		}
		if q.Time.Before(b.FirstSeen) {
			b.FirstSeen = q.Time
		}
		if q.Time.After(b.LastSeen) {
			b.LastSeen = q.Time
		}
		if _, ok := seenDomain[q.Domain]; !ok {
			seenDomain[q.Domain] = struct{}{}
			firsts = append(firsts, FirstSeen{Domain: q.Domain, MAC: mac, FirstSeenMS: q.Time.UnixMilli()})
		}
	}

	out := make([]Bucket, 0, len(orderKeys))
	for _, k := range orderKeys {
		out = append(out, *agg[k])
	}
	sortFirsts(firsts)
	return out, firsts
}

// kvGet/kvSet: helpers locales para no duplicar el acceso kv del httpapi.
func (i *Ingestor) kvGet(ctx context.Context, key string) string {
	var v string
	if err := i.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}

func (i *Ingestor) kvSet(ctx context.Context, key, value string) error {
	_, err := i.db.ExecContext(ctx,
		`INSERT INTO kv (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// setState persiste el estado para que los endpoints lo expongan.
func (i *Ingestor) setState(ctx context.Context, st State) {
	_ = i.kvSet(ctx, kvState, string(st))
	_ = i.kvSet(ctx, kvStateT, fmt.Sprint(i.now().UnixMilli()))
}

// StateOf lee el estado persistido y su marca de tiempo (0 si nunca corrió).
func StateOf(ctx context.Context, d *sql.DB) (State, int64) {
	var st, ts string
	if err := d.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, kvState).Scan(&st); err != nil {
		return StateNoConf, 0
	}
	_ = d.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, kvStateT).Scan(&ts)
	var n int64
	_, _ = fmt.Sscan(strings.TrimSpace(ts), &n)
	return State(st), n
}

// GapSince devuelve la marca del último hueco detectado (0 si no hay).
func GapSince(ctx context.Context, d *sql.DB) int64 {
	var v string
	if err := d.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, kvGap).Scan(&v); err != nil {
		return 0
	}
	var n int64
	_, _ = fmt.Sscan(strings.TrimSpace(v), &n)
	return n
}
