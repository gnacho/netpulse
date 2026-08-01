// netpulse-collector — piloto Go: latencia TCP a los routers de NetPulse.
//
// Sondea cada router (tabla `routers` de la netpulse.db, leída en SOLO LECTURA)
// con un dial TCP al puerto 22 cada 5s y persiste latencia/disponibilidad en su
// propia SQLite (frontera: 1 fichero = 1 escritor; nunca escribe en netpulse.db).
//
// Lecciones aplicadas:
//   - go-collector-stack: apagado con WaitGroup solo para lo que drena/persiste.
//   - sqlite-timeseries-daemon: schema metrics/samples/buckets/daily + NightlyJob.
//   - Beszel/OMV: heartbeat + fichero de estado (state.json) desde el día 1.
//   - LXC sin cap_net_raw: no hay ping ICMP; el dial TCP es la sonda.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gnacho/netpulse/collector/timeseries"
)

const (
	probeInterval = 5 * time.Second
	probeTimeout  = 2 * time.Second
	reloadEvery   = 5 * time.Minute
	version       = "0.1.0"
)

type Target struct {
	Name string
	Addr string // host:port
}

type probeResult struct {
	OK        bool    `json:"ok"`
	LatencyMs float64 `json:"latency_ms,omitempty"`
	Error     string  `json:"error,omitempty"`
}

func main() {
	slog.Info("netpulse-collector arrancando", "version", version)

	dataDir := envOr("DATA_DIR", "/opt/netpulse-collector/data")
	// Ruta real donde install.sh del servidor deja netpulse.db
	// (WorkingDirectory=/var/lib/netpulse + DATA_DIR=./data).
	netpulseDB := envOr("NETPULSE_DB", "/var/lib/netpulse/data/netpulse.db")
	listen := envOr("LISTEN", "127.0.0.1:9100")

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		slog.Error("data dir", "err", err)
		os.Exit(1)
	}

	ts, err := timeseries.NewTimeSeries(filepath.Join(dataDir, "metrics.db"))
	if err != nil {
		slog.Error("timeseries", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := time.Now()
	if err := ts.RegisterMetric(timeseries.Metric{Key: "collector_uptime_s", Unit: "s", Kind: "gauge"}); err != nil {
		slog.Error("register uptime", "err", err)
	}

	// Resultados recientes por target (para state.json y /healthz).
	// RWMutex: /healthz lee un snapshot bajo RLock corto y nunca queda
	// bloqueado por las sondas que publican resultados.
	var mu sync.RWMutex
	last := map[string]probeResult{}
	snapshotLast := func() map[string]probeResult {
		mu.RLock()
		defer mu.RUnlock()
		snap := make(map[string]probeResult, len(last))
		for k, v := range last {
			snap[k] = v
		}
		return snap
	}

	// Recarga de targets: netpulse.db es la fuente de verdad (CRUD en caliente
	// desde la UI de NetPulse; el collector la relee, nunca la escribe).
	targetCh := make(chan []Target, 1)
	go func() {
		for {
			targets, err := loadTargets(netpulseDB)
			if err != nil {
				slog.Error("load targets", "err", err)
			} else {
				select {
				case targetCh <- targets:
				default:
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(reloadEvery):
			}
		}
	}()

	// Supervisor: mantiene una goroutine sonda por target; las que desaparecen
	// de la config mueren por ctx; las nuevas arrancan en caliente; las que
	// cambian de host se reinician con el host nuevo.
	// wg: sondas (drenan en el apagado). supWg: supervisor + state.json —
	// ninguna goroutine que toque `ts` queda viva tras supWg.Wait().
	var wg, supWg sync.WaitGroup
	children := map[string]childProc{}
	onStart := func(t Target) {
		if err := registerTargetMetrics(ts, t); err != nil {
			slog.Error("register metric", "target", t.Name, "err", err)
			return
		}
		cctx, ccancel := context.WithCancel(ctx)
		children[t.Name] = childProc{cancel: ccancel, addr: t.Addr}
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			runProbe(cctx, ts, t, &mu, last)
		}(t)
		slog.Info("target añadido", "name", t.Name, "addr", t.Addr)
	}
	onStop := func(name string) {
		// Purgar `last`: sin esto, state.json y /healthz muestran targets
		// eliminados para siempre (stale).
		mu.Lock()
		delete(last, name)
		mu.Unlock()
		slog.Info("target eliminado", "name", name)
	}
	supWg.Add(1)
	go func() {
		defer supWg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case targets := <-targetCh:
				reconcileTargets(children, targets, onStart, onStop)
			}
		}
	}()

	// Fichero de estado atómico (lección OMV/Beszel): la salud del collector
	// es un dato más, sondeable sin hablar con el proceso. Bajo supWg para que
	// pare (y deje de escribir en ts) antes de ts.Close().
	supWg.Add(1)
	go func() {
		defer supWg.Done()
		tick := time.NewTicker(probeInterval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				ts.Write("collector_uptime_s", time.Since(started).Seconds())
				writeStateFile(dataDir, started, snapshotLast())
			}
		}
	}()

	// Job nocturno: rollup → purga → checkpoint → optimize → VACUUM condicional
	go ts.RunNightlyJob(ctx, 3, 30)

	// /healthz — snapshot de `last` bajo RLock corto: las consultas a la DB del
	// store (lentas con DB ocupada) NO se hacen con el mutex de sondas cogido.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		targets := snapshotLast()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"version":  version,
			"uptime_s": int(time.Since(started).Seconds()),
			"store":    ts.Health(filepath.Join(dataDir, "metrics.db")),
			"targets":  targets,
		})
	})
	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("healthz escuchando", "addr", listen)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("http", "err", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	slog.Info("apagando: parando sondas, drenando writer…")
	cancel()
	supWg.Wait() // supervisor + state.json: ya nadie registra métricas
	wg.Wait()    // sondas: acotadas por probeTimeout
	ts.Close()   // flush final + checkpoint (drena de verdad)
	srv.Close()
	slog.Info("apagado limpio")
}

// childProc: sonda viva supervisada — cancel para matarla y addr para detectar
// que el router cambió de host (hay que reiniciar la sonda, no basta el nombre).
type childProc struct {
	cancel context.CancelFunc
	addr   string
}

// reconcileTargets arranca sondas nuevas, reinicia las que cambiaron de host y
// para las que ya no están en la config. onStart/onStop son callbacks para que
// el supervisor enchufe el registro de métricas y la purga de `last`
// (factorizado así para ser testeable sin DB ni red).
func reconcileTargets(children map[string]childProc, targets []Target, onStart func(Target), onStop func(string)) {
	seen := map[string]bool{}
	for _, t := range targets {
		seen[t.Name] = true
		if c, ok := children[t.Name]; ok {
			if c.addr == t.Addr {
				continue // sin cambios
			}
			c.cancel()
			delete(children, t.Name)
			slog.Warn("target cambió de host: reiniciando sonda",
				"name", t.Name, "antes", c.addr, "ahora", t.Addr)
		}
		onStart(t)
	}
	for name, c := range children {
		if !seen[name] {
			c.cancel()
			delete(children, name)
			onStop(name)
		}
	}
}

// loadTargets lee los routers de NetPulse en SOLO LECTURA + targets extra por env
// (EXTRA_TARGETS="internet=1.1.1.1:443,nas=192.168.1.50:22").
func loadTargets(netpulseDB string) ([]Target, error) {
	var targets []Target
	db, err := sql.Open("sqlite", "file:"+netpulseDB+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT name, host FROM routers ORDER BY is_gateway DESC, created_at ASC")
	if err != nil {
		return nil, fmt.Errorf("routers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, host string
		if err := rows.Scan(&name, &host); err != nil {
			return nil, err
		}
		targets = append(targets, Target{Name: slug(name), Addr: net.JoinHostPort(host, "22")})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, extra := range strings.Split(os.Getenv("EXTRA_TARGETS"), ",") {
		if extra == "" {
			continue
		}
		name, addr, ok := strings.Cut(extra, "=")
		if !ok {
			slog.Error("EXTRA_TARGETS malformado", "entrada", extra)
			continue
		}
		targets = append(targets, Target{Name: slug(name), Addr: addr})
	}
	return dedupeSlugs(targets), nil
}

// dedupeSlugs garantiza nombres únicos (las métricas cuelgan del slug: una
// colisión silenciosa mezclaría series de dos routers). Colisión → sufijo
// -2, -3… + aviso.
func dedupeSlugs(targets []Target) []Target {
	used := map[string]bool{}
	for i, t := range targets {
		if !used[t.Name] {
			used[t.Name] = true
			continue
		}
		base := t.Name
		name := ""
		for n := 2; ; n++ {
			name = fmt.Sprintf("%s-%d", base, n)
			if !used[name] {
				break
			}
		}
		slog.Warn("colisión de slug: target renombrado",
			"slug", base, "nuevo", name, "addr", t.Addr)
		used[name] = true
		targets[i].Name = name
	}
	return targets
}

func registerTargetMetrics(ts *timeseries.TimeSeries, t Target) error {
	if err := ts.RegisterMetric(timeseries.Metric{Key: "tcp_latency_ms." + t.Name, Unit: "ms", Kind: "gauge", MaxValue: ptr(30000.0)}); err != nil {
		return err
	}
	return ts.RegisterMetric(timeseries.Metric{Key: "tcp_ok." + t.Name, Unit: "bool", Kind: "gauge", MaxValue: ptr(1.0)})
}

func runProbe(ctx context.Context, ts *timeseries.TimeSeries, t Target, mu *sync.RWMutex, last map[string]probeResult) {
	// Arranque escalonado (jitter) para no sondear todos los targets a la vez
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(len(t.Name)*137%4000) * time.Millisecond):
	}
	tick := time.NewTicker(probeInterval)
	defer tick.Stop()
	for {
		res := probe(t.Addr)
		if res.OK {
			ts.Write("tcp_latency_ms."+t.Name, res.LatencyMs)
		}
		if res.OK {
			ts.Write("tcp_ok."+t.Name, 1)
		} else {
			ts.Write("tcp_ok."+t.Name, 0)
		}
		mu.Lock()
		last[t.Name] = res
		mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func probe(addr string) probeResult {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err != nil {
		return probeResult{OK: false, Error: err.Error()}
	}
	conn.Close()
	return probeResult{OK: true, LatencyMs: float64(time.Since(start).Microseconds()) / 1000}
}

// writeStateFile escribe state.json de forma atómica (tmp + rename): un lector
// nunca ve el fichero a medias.
func writeStateFile(dataDir string, started time.Time, last map[string]probeResult) {
	state := map[string]any{
		"ts":       time.Now().Unix(),
		"version":  version,
		"uptime_s": int(time.Since(started).Seconds()),
		"targets":  last,
	}
	buf, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	tmp := filepath.Join(dataDir, ".state.json.tmp")
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		slog.Error("state tmp", "err", err)
		return
	}
	if err := os.Rename(tmp, filepath.Join(dataDir, "state.json")); err != nil {
		slog.Error("state rename", "err", err)
	}
}

func slug(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		case r == ' ', r == '_':
			return '-'
		default:
			return -1
		}
	}, s)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func ptr(f float64) *float64 { return &f }
