// demo.go — Adapter demo COMPLETO (port de src/adapters/demo.js, SPEC §7.1):
// dataset canónico + random walk por tick:
//   - cpu/ram/temp ±2 máx (acotados 2-95 / 10-92 / 30-82)
//   - tráfico ±5 %, latencia WAN acotada 6–11 ms
//   - AdGuard siempre creciente, bytes WG crecientes solo en peers activos
//
// El PRIMER snapshot (antes del primer Tick) es exactamente el canon.
package adapters

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

const demoGatewayID = "flint2"

// wgByteCounter lleva los contadores numéricos internos de un peer WG
// (rx/tx se formatean ES al servir).
type wgByteCounter struct {
	rx, tx float64
	active bool
}

// Demo es el Snapshotter del modo demo.
type Demo struct {
	mu        sync.Mutex
	rng       *rand.Rand
	routers   []Router
	wan       WAN
	adguard   AdGuardStats
	wireguard WireGuardStats
	engine    *alerts.Engine
	devices   []Device
	extras    map[string]*demoExtras
	wgBytes   map[string]*wgByteCounter
	// Señuelos inmutables construidos una vez (series y extras del gateway).
	adguardSeries []AdGuardHour
	wanLatency    *WANLatency
	wgPeerExtras  map[string]wgPeerExtra
	wgTotals30d   *WGTotals
}

// NewDemo crea el adapter demo con una copia fresca del dataset canónico.
// Las 5 alertas canónicas entran en el motor vía Seed (SPEC-ALERTAS §5:
// deben sobrevivir con los defaults — nótese que con el filtrado estricto
// de creación, "vpn:none" y "clients:urgent" descartarían 2 de las 5; Seed
// omite SOLO el filtro de config y mantiene dedup/cap/read-state). Si se
// pasa un engine (main lo crea con kv) se usa; si no, memoria (tests).
func NewDemo(engine ...*alerts.Engine) *Demo {
	eng := alerts.New(nil, nil)
	if len(engine) > 0 && engine[0] != nil {
		eng = engine[0]
	}
	wg := canonWireguard()
	d := &Demo{
		rng:           rand.New(rand.NewSource(time.Now().UnixNano())),
		routers:       canonRouters(),
		wan:           canonWAN(),
		adguard:       canonAdguard(),
		wireguard:     wg,
		engine:        eng,
		devices:       canonAllDevices(),
		extras:        canonRouterExtras(),
		wgBytes:       map[string]*wgByteCounter{},
		adguardSeries: buildAdGuardSeries(),
		wanLatency: &WANLatency{
			Last24h: canonWANLatency24h(),
			Stats:   canonWANLatencyStats(),
		},
		wgPeerExtras: canonWGPeerExtras(),
		wgTotals30d:  canonWGTotals30d(),
	}
	for _, p := range wg.Peers {
		d.wgBytes[p.ID] = &wgByteCounter{rx: parseBytes(p.Rx), tx: parseBytes(p.Tx), active: p.Active}
	}
	// SPEC-CANON D5: totales y clientes por router DERIVADOS del dataset
	// (los literales de canonRouters/canonAdguard son solo el canon base).
	for i := range d.routers {
		d.routers[i].Clients = onlineClientsOf(d.devices, d.routers[i].ID)
	}
	d.adguard.ClientsTotal = len(d.devices)
	// Las 5 canon entran al motor en ORDEN INVERSO (Seed prepende) para que el
	// feed quede en el orden canónico; después se marca el read-state del
	// canon (3 leídas) en el read-set del servidor.
	canon := canonAlerts()
	for i := len(canon) - 1; i >= 0; i-- {
		eng.Seed(canon[i])
	}
	readIDs := []string{}
	for _, a := range canon {
		if a.Read {
			readIDs = append(readIDs, a.ID)
		}
	}
	eng.MarkRead(readIDs...)
	return d
}

// ---------------------------------------------------------------------------
// Random walk (demo.js:57-66, 89-122)
// ---------------------------------------------------------------------------

func (d *Demo) rnd(min, max float64) float64 { return min + d.rng.Float64()*(max-min) }

func clampF(v, lo, hi float64) float64 { return math.Min(hi, math.Max(lo, v)) }

// walkInt: entero en ±step del valor anterior, acotado.
func (d *Demo) walkInt(prev int, step, lo, hi int) int {
	v := int(math.Round(float64(prev) + d.rnd(-float64(step), float64(step))))
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v
}

// walkPct: ±pct % del valor anterior (1 decimal), acotado.
func (d *Demo) walkPct(prev, pct, lo, hi float64) float64 {
	v := math.Round(prev*(1+d.rnd(-pct, pct))*10) / 10 // toFixed(1)
	return clampF(v, lo, hi)
}

// Tick avanza el random walk (5 s). Paridad demo.js:89-122.
func (d *Demo) Tick(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.routers {
		r := &d.routers[i]
		*r.CPU = d.walkInt(*r.CPU, 2, 2, 95)
		*r.RAM = d.walkInt(*r.RAM, 2, 10, 92)
		*r.Temp = d.walkInt(*r.Temp, 2, 30, 82)
		if ex := d.extras[r.ID]; ex != nil {
			tn := ex.TrafficNow
			if tn == 0 {
				tn = 0.1
			}
			ex.TrafficNow = d.walkPct(tn, 0.05, 0.05, 950)
		}
	}
	d.wan.DownMbps = d.walkPct(d.wan.DownMbps, 0.05, 5, 580)
	d.wan.UpMbps = d.walkPct(d.wan.UpMbps, 0.05, 2, 120)
	d.wan.LatencyMs = clampF(float64(d.walkInt(int(d.wan.LatencyMs), 1, 6, 11)), 6, 11)
	for i := range d.devices {
		dev := &d.devices[i]
		if dev.Online && dev.TrafficMbps > 0 {
			dev.TrafficMbps = math.Round(d.walkPct(dev.TrafficMbps, 0.05, 0.001, 950)*100) / 100
		}
	}
	// AdGuard: contadores siempre crecientes
	d.adguard.Queries24h += int64(math.Round(d.rnd(2, 10)))
	newBlocked := int64(math.Round(d.rnd(0, 2)))
	d.adguard.Blocked24h += newBlocked
	d.adguard.TrackersBlocked += int64(math.Round(d.rnd(0, float64(newBlocked))))
	d.adguard.BlockedPct = math.Round(float64(d.adguard.Blocked24h)/float64(d.adguard.Queries24h)*1000) / 10
	// WireGuard: bytes crecientes solo en peers activos
	for id, b := range d.wgBytes {
		if !b.active {
			continue
		}
		b.rx += math.Round(d.rnd(0.2, 1.7) * 1e6)
		b.tx += math.Round(d.rnd(0.04, 0.4) * 1e6)
		for i := range d.wireguard.Peers {
			if d.wireguard.Peers[i].ID == id {
				d.wireguard.Peers[i].Rx = fmtBytes(b.rx)
				d.wireguard.Peers[i].Tx = fmtBytes(b.tx)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Snapshotter
// ---------------------------------------------------------------------------

func (d *Demo) Mode() string              { return "demo" }
func (d *Demo) SetRouters([]RouterConfig) {} // la demo ignora la configuración
func (d *Demo) Close() error              { return nil }

func (d *Demo) GetUsteer(context.Context) (*Usteer, error) { return nil, nil }

// KickUsteerClient no-op en demo.
func (d *Demo) KickUsteerClient(context.Context, string) error { return errors.New("not available in demo") }

// GetReanchorRecommendations no-op en demo.
func (d *Demo) GetReanchorRecommendations(context.Context, ReanchorConfig) ([]ReanchorRecommendation, RoamingDaemon, error) {
	return []ReanchorRecommendation{}, RoamingDaemonNone, nil
}

func (d *Demo) GetDot11r(context.Context) (*Dot11rOverview, error) { return nil, nil }

func (d *Demo) GetSurvey(context.Context) (*SurveyOverview, error) { return nil, nil }

func (d *Demo) GetAdguardClients(context.Context) ([]AdguardClient, error) { return nil, nil }

// GetOverview: overview completo (ts en SEGUNDOS). Paridad demo.js:132-146.
func (d *Demo) GetOverview(context.Context) (*Overview, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.buildOverview(), nil
}

func (d *Demo) buildOverview() *Overview {
	wg := d.wireguardSnapshot()
	dists := canonDistributionNodes()
	return &Overview{
		Health:            canonHealthScore(),
		WAN:               d.wan,
		Traffic:           canonTraffic(),
		Adguard:           d.adguard,
		Wireguard:         wg,
		Routers:           d.routersCopy(),
		DeviceTotals:      deviceTotalsOf(d.devices), // D5: derivado del dataset
		TopDevices:        d.topDevices(5),
		Alerts:            d.alertsCopy(),
		UnreadAlerts:      d.engine.UnreadCount(),
		DistributionNodes: dists,
		Topology:          BuildTopoSemantics(d.routers, d.devices, wg, dists), // SPEC-65 D65-3
		VM:                ViewModelVersion,                                    // SPEC-65 D65-4
		Ts:                time.Now().Unix(),
	}
}

func (d *Demo) wireguardSnapshot() WireGuardStats {
	peers := make([]WGPeer, len(d.wireguard.Peers))
	copy(peers, d.wireguard.Peers)
	return WireGuardStats{
		Interface: d.wireguard.Interface, Subnet: d.wireguard.Subnet,
		Status: d.wireguard.Status, Peers: peers,
	}
}

func (d *Demo) routersCopy() []Router {
	out := make([]Router, len(d.routers))
	copy(out, d.routers)
	return out
}

func (d *Demo) alertsCopy() []AlertEvent {
	return d.engine.List()
}

func (d *Demo) devicesCopy() []Device {
	out := make([]Device, len(d.devices))
	copy(out, d.devices)
	return out
}

// topDevices: 5 por trafficMbps desc (orden estable, como el sort de V8).
func (d *Demo) topDevices(n int) []Device {
	devs := d.devicesCopy()
	sort.SliceStable(devs, func(i, j int) bool { return devs[i].TrafficMbps > devs[j].TrafficMbps })
	if len(devs) > n {
		devs = devs[:n]
	}
	return devs
}

// GetRouters devuelve copia de las 4 tarjetas.
func (d *Demo) GetRouters(context.Context) []Router {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.routersCopy()
}

// GetRouterDetail (demo.js:152-178): extras canónicos, series deterministas,
// y solo para flint2 adguard/wireguard/adguardSeries24h/wanLatency/
// wgPeerExtras/wgTotals30d. Id desconocido → (nil, nil).
func (d *Demo) GetRouterDetail(_ context.Context, id string) (*RouterDetail, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var router *Router
	for i := range d.routers {
		if d.routers[i].ID == id {
			router = &d.routers[i]
			break
		}
	}
	if router == nil {
		return nil, nil
	}
	extras := d.extras[id]
	if extras == nil {
		extras = d.extras[demoGatewayID]
	}
	var radios []Radio // nil → null si no hay (quirk del contrato)
	if len(extras.Radios) > 0 {
		radios = extras.Radios
	}
	var backhaul any
	if extras.Backhaul != nil {
		backhaul = extras.Backhaul
	}
	var clients []Device
	for _, dev := range d.devices {
		if dev.RouterID == id {
			clients = append(clients, dev)
		}
	}
	cpu, ram, temp := *router.CPU, *router.RAM, *router.Temp
	detail := &RouterDetail{
		Router:   *router,
		Ports:    extras.EthPorts,
		Radios:   radios,
		Backhaul: backhaul,
		Series: PerfSeries{
			H1:  perfSeries(id, cpu, ram, temp, "1h"),
			H24: perfSeries(id, cpu, ram, temp, "24h"),
			D7:  perfSeries(id, cpu, ram, temp, "7d"),
		},
		Clients: clients,
		Extras:  extras,
	}
	if id == demoGatewayID {
		ag := d.adguard
		wg := d.wireguardSnapshot()
		detail.Adguard = &ag
		detail.Wireguard = &wg
		detail.AdguardSeries24h = d.adguardSeries
		detail.WANLatency = d.wanLatency
		detail.WGPeerExtras = d.wgPeerExtras
		detail.WGTotals30d = d.wgTotals30d
	}
	return detail, nil
}

// GetDevices devuelve copia de los 66 dispositivos.
func (d *Demo) GetDevices(context.Context) []Device {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.devicesCopy()
}

// GetAlerts devuelve las 5 alertas canónicas (via motor; read-set aplicado).
func (d *Demo) GetAlerts(context.Context) []AlertEvent {
	return d.alertsCopy()
}

// AlertsEngine: motor de alertas del adapter (SPEC-ALERTAS §3).
func (d *Demo) AlertsEngine() *alerts.Engine { return d.engine }

// GetMetricsRows (demo.js:189-203). El poller no persiste en demo, pero la
// interfaz lo exige.
func (d *Demo) GetMetricsRows(context.Context) []MetricsRow {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows := make([]MetricsRow, 0, len(d.routers))
	for _, r := range d.routers {
		ex := d.extras[r.ID]
		var trafficNow, gwLatency float64
		if ex != nil {
			trafficNow = ex.TrafficNow
			if ex.GatewayLatencyMs != nil {
				gwLatency = *ex.GatewayLatencyMs
			}
		}
		rx := trafficNow * 1e6
		row := MetricsRow{
			RouterID: r.ID,
			CPU:      fptr(float64(*r.CPU)),
			RAM:      fptr(float64(*r.RAM)),
			Temp:     fptr(float64(*r.Temp)),
			RxBps:    fptr(math.Round(rx)),
			TxBps:    fptr(math.Round(rx * 0.15)),
		}
		if r.ID == demoGatewayID {
			row.LatencyMs = fptr(d.wan.LatencyMs)
		} else if ex != nil && ex.GatewayLatencyMs != nil {
			row.LatencyMs = fptr(gwLatency)
		}
		rows = append(rows, row)
	}
	return rows
}
