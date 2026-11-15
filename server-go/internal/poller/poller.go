// Package poller — sondeo periódico de routers (SPEC §6):
// tick inmediato + intervalo configurable; adapter.tick → getOverview → persiste
// (solo live) → broadcast SSE snapshot → alertas nuevas (cebado en el 1er tick).
// Robustez: un fallo del adapter nunca mata el poller.
//
// NOTA (núcleo Go): con el adapter STUB demo esto ya es funcional; el agente
// B enchufa aquí el adapter live sin tocar este bucle.
package poller

import (
	"context"
	"log"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"github.com/gnacho/netpulse/server-go/internal/wgseries"
)

// TickMS es el intervalo por defecto del poller (5 s). El valor efectivo lo
// fija la config (NETPULSE_POLL_INTERVAL, default 30 s) vía New; TickMS solo
// se usa como respaldo cuando New recibe un intervalo no positivo.
const TickMS = 5 * time.Second

// wgSampleInterval (#2019): cada cuánto se persiste una muestra por peer de
// WireGuard. Una ventana de 30 días no necesita la resolución del tick: con 5
// minutos el error de un reset queda acotado a esos 5 minutos y el raw se
// queda en ~24k filas por semana con una docena de peers.
const wgSampleInterval = 5 * time.Minute

// Poller ejecuta el bucle de sondeo.
type Poller struct {
	adapter adapters.Snapshotter
	db      *db.DB
	hub     *sse.Hub

	// interval es la cadencia del ticker (config). <= 0 → TickMS.
	interval time.Duration

	// enrich (opcional) inyecta en cada overview los campos que viven en el
	// kv del server (orchestration, plan contratado #151, speedtest #511)
	// ANTES de cacheálo y emitirlo por SSE. Sin esto, el snapshot del SSE
	// pisaría la copia enriquecida que sirvió /api/overview y esos campos
	// desaparecerían de la UI al primer evento.
	enrich func(*adapters.Overview)

	mu           sync.RWMutex
	lastOverview *adapters.Overview
	lastAdguard  *adapters.AdGuardStats
	knownAlerts  map[string]struct{}
	primed       bool

	stopCh chan struct{}
	doneCh chan struct{}
	once   sync.Once

	// tickMu serializa los ticks (ticker + sondeos manuales PollNow).
	tickMu sync.Mutex

	// wgMu protege wgLastWrite: último instante en que se persistió una
	// muestra de peers por router (#2019).
	wgMu        sync.Mutex
	wgLastWrite map[string]int64
}

// New crea el poller. interval fija la cadencia del ticker (segundos de la
// config); un valor <= 0 cae a TickMS (respaldo de paquete).
func New(adapter adapters.Snapshotter, d *db.DB, hub *sse.Hub, interval time.Duration) *Poller {
	return &Poller{
		adapter:     adapter,
		db:          d,
		hub:         hub,
		interval:    interval,
		knownAlerts: map[string]struct{}{},
		wgLastWrite: map[string]int64{},
		stopCh:      make(chan struct{}), doneCh: make(chan struct{}),
	}
}

// LastOverview devuelve el último overview (para SSE y /api/overview).
func (p *Poller) LastOverview() *adapters.Overview {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastOverview
}

// Start lanza el bucle: primer tick inmediato + ticker del intervalo fijado.
func (p *Poller) Start() {
	go func() {
		defer close(p.doneCh)
		p.tickOnce()
		interval := p.interval
		if interval <= 0 {
			interval = TickMS
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-p.stopCh:
				return
			case <-t.C:
				p.tickOnce()
			}
		}
	}()
}

// PollNow solicita un ciclo de sondeo inmediato (POST /api/refresh). No
// bloquea al caller HTTP: el tick corre en background y el snapshot fresco
// llega por el broadcast SSE normal. Si ya hay un tick en curso (manual o
// del ticker) no apila otro: ese snapshot también estará fresco.
func (p *Poller) PollNow() {
	go p.tickOnce()
}

// tickOnce ejecuta un tick solo si no hay otro en curso (evita solapar
// sondeos SSH de todos los routers cuando coinciden ticker y refresh manual).
func (p *Poller) tickOnce() {
	if !p.tickMu.TryLock() {
		return
	}
	defer p.tickMu.Unlock()
	p.tick()
}

// SetEnrich fija el hook de enriquecimiento del overview (main lo cablea con
// las inyecciones kv del httpapi).
func (p *Poller) SetEnrich(fn func(*adapters.Overview)) { p.enrich = fn }

// stopWait acota cuánto espera Stop al tick en curso. Issue #481: un sondeo
// SNMP/SSH lento (o una primitiva de red sin deadline) no debe colgar el
// apagado hasta que systemd mate el proceso por TimeoutStopSec; cuando main
// retorna, el proceso muere igualmente y se lleva la goroutine colgada.
var stopWait = 15 * time.Second

// Stop detiene el bucle y espera al tick en curso como mucho stopWait.
func (p *Poller) Stop() {
	p.once.Do(func() {
		close(p.stopCh)
		select {
		case <-p.doneCh:
		case <-time.After(stopWait):
			log.Printf("[netpulse] poller: current tick did not finish within %v, shutdown continues anyway", stopWait)
		}
	})
}

// tick ejecuta un ciclo completo (nunca propaga errores).
func (p *Poller) tick() {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[netpulse] error en tick del poller: %v\n%s", rec, debug.Stack())
		}
	}()
	ctx := context.Background()

	_ = p.adapter.Tick(ctx) // demo: random walk; live: no-op

	overview, err := p.adapter.GetOverview(ctx)
	if err != nil || overview == nil {
		if err != nil {
			log.Printf("[netpulse] error en tick del poller: %v", err)
		}
		return
	}
	if p.enrich != nil {
		p.enrich(overview)
	}

	p.mu.Lock()
	p.lastOverview = overview
	ag := overview.Adguard
	p.lastAdguard = &ag
	p.mu.Unlock()

	// Persistencia SOLO en live (en demo la BD se mantiene vacía de métricas).
	if p.adapter.Mode() != "demo" {
		p.persist(overview)
	}

	p.hub.Broadcast("snapshot", overview)

	// Alertas: el 1er tick solo ceba knownAlertIds (no re-notifica).
	alerts := p.adapter.GetAlerts(ctx)
	if !p.primed {
		for _, a := range alerts {
			p.knownAlerts[a.ID] = struct{}{}
		}
		p.primed = true
		return
	}
	for _, a := range alerts {
		if _, known := p.knownAlerts[a.ID]; known {
			continue
		}
		p.knownAlerts[a.ID] = struct{}{}
		p.hub.Broadcast("alert", a)
	}
}

// persist guarda métricas del tick (ts común, epoch ms). Quirk preservado:
// en live se inserta una fila adguard_stats (0,0) cada tick aunque AdGuard
// no esté configurado, porque overview.adguard siempre va presente (SPEC §6).
func (p *Poller) persist(overview *adapters.Overview) {
	ts := db.NowMS()
	for _, row := range p.adapter.GetMetricsRows(context.Background()) {
		if _, err := p.db.Exec(
			"INSERT INTO metrics (router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			row.RouterID, ts, row.CPU, row.RAM, row.Temp, row.LatencyMs, row.RxBps, row.TxBps,
		); err != nil {
			log.Printf("[netpulse] error en tick del poller (metrics): %v", err)
		}
	}
	p.mu.RLock()
	ag := p.lastAdguard
	p.mu.RUnlock()
	if ag != nil {
		if _, err := p.db.Exec(
			"INSERT INTO adguard_stats (ts, queries, blocked) VALUES (?, ?, ?)",
			ts, ag.Queries24h, ag.Blocked24h,
		); err != nil {
			log.Printf("[netpulse] error en tick del poller (adguard_stats): %v", err)
		}
	}
	p.persistWireGuard(overview, ts)
}

// persistWireGuard guarda el acumulado de cada peer de WireGuard para poder
// sumar la ventana de 30 días (#2019). WireGuard no guarda historia y sus
// contadores se reinician con el peer, así que el delta lo calcula el store
// contra la muestra anterior. Se muestrea cada wgSampleInterval, no en cada
// tick: el histórico es para una ventana de días, no para una gráfica.
func (p *Poller) persistWireGuard(overview *adapters.Overview, ts int64) {
	if p.db == nil || p.db.WGSeries == nil {
		return
	}
	wg := overview.Wireguard
	if wg.RouterID == "" || len(wg.Peers) == 0 {
		return
	}
	p.wgMu.Lock()
	last := p.wgLastWrite[wg.RouterID]
	p.wgMu.Unlock()
	if last != 0 && ts-last < wgSampleInterval.Milliseconds() {
		return
	}
	samples := make([]wgseries.PeerSample, 0, len(wg.Peers))
	for _, peer := range wg.Peers {
		if peer.Key == "" {
			continue
		}
		samples = append(samples, wgseries.PeerSample{
			Key: peer.Key, RxBytes: peer.RxBytes, TxBytes: peer.TxBytes,
		})
	}
	if len(samples) == 0 {
		return
	}
	if err := p.db.WGSeries.RecordSamples(wg.RouterID, time.UnixMilli(ts), samples); err != nil {
		log.Printf("[netpulse] error en tick del poller (wg peers): %v", err)
		return
	}
	p.wgMu.Lock()
	p.wgLastWrite[wg.RouterID] = ts
	p.wgMu.Unlock()
}
