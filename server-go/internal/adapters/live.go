// live.go — Adapter live (port de src/adapters/index.js, SPEC §7.2-§7.8):
// compone el snapshot por router con degradación elegante (router caído →
// offline + alerta tras 2 fallos seguidos; el resto sigue), single-flight en
// GetOverview (los SSH son lo caro), persistencia de atribución en
// device_attrib (solo wireless) y alertas en memoria (máx 100).
package adapters

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/deviceevents"
)

// fmtUptime: "<d>d <h>h" (index.js:32-36).
func fmtUptime(sec float64) string {
	d := int(sec) / 86400
	h := (int(sec) % 86400) / 3600
	return fmt.Sprintf("%dd %dh", d, h)
}

// bptr: puntero a bool (el view-model usa *bool para distinguir "ausente" de
// "false" explícito, p.ej. Router.LldpAvailable).
func bptr(v bool) *bool { return &v }

// mbps: bps → Mbps 1 decimal (null → 0).
func mbps(bps *float64) float64 {
	if bps == nil {
		return 0
	}
	return math.Round(*bps/1e6*10) / 10
}

// pickGateway: is_gateway → glinet → primero.
func pickGateway(routers []RouterConfig) *RouterConfig {
	for i := range routers {
		if routers[i].IsGateway {
			return &routers[i]
		}
	}
	for i := range routers {
		if routers[i].Type == "glinet" {
			return &routers[i]
		}
	}
	if len(routers) > 0 {
		return &routers[0]
	}
	return nil
}

// pickGatewayCfg: same logic as pickGateway but from polled router configs.
func pickGatewayCfg(polled map[string]*routerPolled) *RouterConfig {
	ids := make([]string, 0, len(polled))
	for id := range polled {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if polled[id].cfg.IsGateway {
			return &polled[id].cfg
		}
	}
	for _, id := range ids {
		if polled[id].cfg.Type == "glinet" {
			return &polled[id].cfg
		}
	}
	if len(ids) > 0 {
		return &polled[ids[0]].cfg
	}
	return nil
}

// routerPolled es el sondeo de un tick de un router.
type routerPolled struct {
	cfg       RouterConfig
	client    *OpenWrtClient
	sysInfo   *SysInfo
	board     *BoardInfo
	cpu       int
	ram       int
	temp      int
	uptimeSec float64
	net       *NetDevBps
	leases    []DhcpLease
	// glClients (GL.iNet): base de clientes del firmware, superset de las
	// leases. Se usa SOLO para resolver IPs de dispositivos ya conocidos que
	// salen sin IP (dnsmasq sin lease), nunca para crear dispositivos nuevos.
	glClients []DhcpLease
	wireless  map[string]WirelessClient
	ports     []EthPort
	radios    []Radio
	fdb       map[string]string
	brMac     string
	latencyMs *float64
	lossPct   *float64
	// backhaul: "cable"|"wifi"|"" (desconocido → se omite en el contrato).
	backhaul string
	// lldp: vecinos LLDP del tier lento (nil = sin lldpd o sonda fallida).
	lldp []LldpNeighbor
	// lldpUnavailable: lldpd no está instalado en este router (issue #247) →
	// el view-model expone `lldpAvailable:false` para el hint de instalación.
	lldpUnavailable bool
	// luci: etiquetas de puertos/VLANs de LuCI (issue #258), si el router las
	// define en /etc/config/luci. Fuente de nombres para la topología.
	luci *probe.LuCILabels
	// wanInfo: estado de la conexión WAN (solo gateway, issue #276). Los APs
	// dejan el struct vacío (sin datos WAN).
	wanInfo probe.WanInfo
}

// extrasSnapshot es la caché anti-parpadeo por router.
type extrasSnapshot struct {
	ports    []EthPort
	radios   []Radio
	wireless map[string]WirelessClient
	fdb      map[string]string
	luci     *probe.LuCILabels
}

// backhaulCacheTTL: el medio del uplink cambia muy raro; no se sondea cada 5 s.
const backhaulCacheTTL = 5 * time.Minute

// backhaulCacheEntry: resultado cacheado de la detección de uplink wifi
// (value "" = desconocido/sonda fallida → el campo se omite).
type backhaulCacheEntry struct {
	value string
	at    time.Time
}

// lldpCacheTTL: tier lento LLDP (45 s, como los extras cacheados).
const lldpCacheTTL = 45 * time.Second

// lldpCacheEntry: vecinos LLDP cacheados por router (nil = sin datos).
// unavailable=true cuando lldpd NO está instalado en el router (ErrLldpUnavailable);
// la UI lo usa para el hint "instala lldpd" (issue #247).
type lldpCacheEntry struct {
	neighbors   []LldpNeighbor
	unavailable bool
	at          time.Time
}

// wanInfoCacheTTL: tier lento del estado WAN del gateway. La conexión WAN no
// cambia cada tick; un sondeo cada 60 s sobra y evita martillear ubus.
const wanInfoCacheTTL = 60 * time.Second

// wanInfoCacheEntry: WanInfo cacheado del gateway (issue #276).
type wanInfoCacheEntry struct {
	info probe.WanInfo
	at   time.Time
}

// Live es el Snapshotter del modo live.
type Live struct {
	cfg  *config.Config
	db   *db.DB
	pool *SSHPool

	mu         sync.Mutex
	routers    []RouterConfig
	gatewayCfg *RouterConfig
	clients    map[string]*OpenWrtClient

	lastGood    map[string]*Router
	lastStatus  map[string]string
	boardCache  map[string]*BoardInfo
	layoutCache map[string][]PortLayout
	extrasCache map[string]*extrasSnapshot
	lastPolled  map[string]*routerPolled
	failCount   map[string]int
	lastErr     map[string]error // último error del sondeo (issue #257: distinguir sin-acceso de caído)
	engine      *alerts.Engine
	wgActive    map[string]bool
	weakAlerted map[string]int64
	onlineMacs  map[string]bool
	// unknownGrace: ticks consecutivos online+sin nombre (Name == MAC) por
	// MAC. unknownAlerted: MACs que ya dispararon la alerta de desconocido
	// (issue #248, persistido en kv: cada MAC alerta UNA sola vez).
	unknownGrace    map[string]int
	unknownAlerted  map[string]bool
	unknownGraceNum int // ticks antes de declarar desconocido (default 3, #234)
	// Presencia wireless (issue #184): devicePresence = MAC → online del
	// último ciclo evaluado; presenceMisses = ticks seguidos sin verse.
	// presenceSeen evita la avalancha del primer ciclo (anti-arranque).
	presenceSeen         bool
	devicePresence       map[string]bool
	presenceMisses       map[string]int
	presenceOfflineAfter int // ticks sin verse antes de declarar offline (default 3)
	// presencePruneAfter: ticks sin verse que eliminan la MAC de los mapas
	// (dispositivo que se fue para siempre; ~2h a 5s/tick, issue #206).
	presencePruneAfter int
	// dawnAvailable: cache de "¿hay DAWN en algún router?" para el flag del
	// overview (entrada /roaming). Refrescado asíncronamente (TTL 30s, 1 SSH
	// al gateway) por dawnAvailableCached para no bloquear buildOverview.
	dawnAvailable  bool
	dawnCheckedAt  time.Time
	dawnChecking   bool
	seenOnlineMacs bool
	wanDown        map[string]int
	backhaulCache  map[string]backhaulCacheEntry
	lldpCache      map[string]lldpCacheEntry
	wanInfoCache   map[string]wanInfoCacheEntry

	// Agentes nativos (Tier 2): último payload por slug + flag de caída
	// (degradado a SSH tras emitir la alerta, SPEC-AGENTE-PILOTO §1).
	agents           *AgentRegistry
	agentDown        map[string]bool
	agentDownConfirm time.Duration // Dead Man's Switch: confirmar caída tras este periodo sin alertar

	// now: reloj inyectable para tests deterministas (nil → time.Now).
	// Mismo patrón que AgentRegistry.SetClock.
	now func() time.Time

	agStd *AdGuardClient
	agGL  *AdGuardGlinetClient
	agKey string

	sfMu   sync.Mutex
	sfCall *sfCall
}

type sfCall struct {
	done chan struct{}
	ov   *Overview
	err  error
}

// NewLive crea el adapter live (db puede ser nil en tests).
func NewLive(cfg *config.Config, d *db.DB, initial []RouterConfig, pool *SSHPool) *Live {
	l := &Live{
		cfg:                  cfg,
		db:                   d,
		pool:                 pool,
		lastGood:             map[string]*Router{},
		lastStatus:           map[string]string{},
		boardCache:           map[string]*BoardInfo{},
		layoutCache:          map[string][]PortLayout{},
		extrasCache:          map[string]*extrasSnapshot{},
		lastPolled:           map[string]*routerPolled{},
		failCount:            map[string]int{},
		lastErr:              map[string]error{},
		engine:               alerts.New(d, nil),
		wgActive:             map[string]bool{},
		weakAlerted:          map[string]int64{},
		onlineMacs:           map[string]bool{},
		unknownGrace:         map[string]int{},
		unknownAlerted:       map[string]bool{},
		unknownGraceNum:      3, // default de #234; el engine/demo puede afinarlo
		devicePresence:       map[string]bool{},
		presenceMisses:       map[string]int{},
		presenceOfflineAfter: 3,
		presencePruneAfter:   2000,
		wanDown:              map[string]int{},
		backhaulCache:        map[string]backhaulCacheEntry{},
		lldpCache:            map[string]lldpCacheEntry{},
		wanInfoCache:         map[string]wanInfoCacheEntry{},
		agentDown:            map[string]bool{},
		agentDownConfirm:     3 * time.Minute, // Dead Man's Switch (P6): 3 min por defecto
	}
	// Migración una vez (attrib_v2): tabla limpia (index.js:385-394)
	if d != nil {
		var flag string
		if err := d.QueryRow("SELECT value FROM kv WHERE key = 'attrib_v2'").Scan(&flag); err == sql.ErrNoRows {
			_, _ = d.Exec("DELETE FROM device_attrib")
			_, _ = d.Exec("INSERT INTO kv (key, value) VALUES ('attrib_v2', '1')")
			log.Printf("[netpulse] device_attrib limpiada (attrib_v2: solo wireless persiste)")
		}
		// issue #248: memoria per-MAC de "desconocido ya alertado", persistida
		// en kv para que un reinicio del servidor no vuelva a alertar las
		// MACs ya conocidas (clave `unknown_alerted:<mac>`).
		if rows, err := d.Query("SELECT key FROM kv WHERE key LIKE 'unknown_alerted:%'"); err == nil {
			for rows.Next() {
				var key string
				if rows.Scan(&key) == nil {
					l.unknownAlerted[strings.TrimPrefix(key, "unknown_alerted:")] = true
				}
			}
			rows.Close()
		}
	}
	l.SetRouters(initial)
	return l
}

// Mode: "live".
func (l *Live) Mode() string { return "live" }

// Tick: no-op (el sondeo real ocurre en GetOverview, como el JS).
func (l *Live) Tick(context.Context) error { return nil }

// Close cierra el pool SSH.
func (l *Live) Close() error {
	l.pool.Close()
	return nil
}

// SetRouters actualiza la lista en caliente y limpia cachés de bajas.
func (l *Live) SetRouters(list []RouterConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.routers = append([]RouterConfig(nil), list...)
	l.gatewayCfg = pickGateway(l.routers)
	l.clients = map[string]*OpenWrtClient{}
	for _, c := range l.routers {
		if !c.AgentOnly {
			l.clients[c.ID] = NewOpenWrtClient(c, l.pool, "root", "")
		}
	}
	ids := map[string]bool{}
	for _, r := range l.routers {
		ids[r.ID] = true
	}
	for id := range l.lastGood {
		if !ids[id] {
			delete(l.lastGood, id)
		}
	}
	for id := range l.lastStatus {
		if !ids[id] {
			delete(l.lastStatus, id)
		}
	}
	for id := range l.boardCache {
		if !ids[id] {
			delete(l.boardCache, id)
		}
	}
	for id := range l.layoutCache {
		if !ids[id] {
			delete(l.layoutCache, id)
		}
	}
	for id := range l.extrasCache {
		if !ids[id] {
			delete(l.extrasCache, id)
		}
	}
	for id := range l.lastPolled {
		if !ids[id] {
			delete(l.lastPolled, id)
		}
	}
	for id := range l.failCount {
		if !ids[id] {
			delete(l.failCount, id)
		}
	}
	for id := range l.lastErr {
		if !ids[id] {
			delete(l.lastErr, id)
		}
	}
	// unknownGrace es per-MAC (no por router): se poda por ausencia en
	// trackUnknownDevices; aquí solo se limpia la lista completa si no queda
	// ningún router (config vacía tras un reset).
	if len(l.routers) == 0 {
		l.unknownGrace = map[string]int{}
	}
	for id := range l.backhaulCache {
		if !ids[id] {
			delete(l.backhaulCache, id)
		}
	}
	for id := range l.lldpCache {
		if !ids[id] {
			delete(l.lldpCache, id)
		}
	}
	for id := range l.wanInfoCache {
		if !ids[id] {
			delete(l.wanInfoCache, id)
		}
	}
}

// probeBackhaul detecta el medio del uplink (interfaz STA asociada → "wifi";
// si no → "cable") con caché de 5 min: no se sondea cada tick. Error (router
// sin wifi, ubus caído) → "" cacheado igualmente (el campo se omite y el
// sondeo no se rompe).
func (l *Live) probeBackhaul(routerID string, client *OpenWrtClient) string {
	l.mu.Lock()
	e, ok := l.backhaulCache[routerID]
	l.mu.Unlock()
	if ok && time.Since(e.at) < backhaulCacheTTL {
		return e.value
	}
	value := ""
	if wifi, err := client.GetWirelessUplink(); err == nil {
		value = "cable"
		if wifi {
			value = "wifi"
		}
	}
	l.mu.Lock()
	l.backhaulCache[routerID] = backhaulCacheEntry{value: value, at: time.Now()}
	l.mu.Unlock()
	return value
}

// probeWanInfo: estado de la conexión WAN del gateway (issue #276) con caché
// de 60 s. En routers sin interfaz wan (APs) devuelve WanInfo vacío. Fallo de
// ubus → vacío cacheado (el detalle sigue mostrando lo demás, sin romper).
func (l *Live) probeWanInfo(routerID string, client *OpenWrtClient) probe.WanInfo {
	l.mu.Lock()
	e, ok := l.wanInfoCache[routerID]
	l.mu.Unlock()
	if ok && time.Since(e.at) < wanInfoCacheTTL {
		return e.info
	}
	info := client.GetWanInfo()
	l.mu.Lock()
	l.wanInfoCache[routerID] = wanInfoCacheEntry{info: info, at: time.Now()}
	l.mu.Unlock()
	return info
}

// probeLldp: vecinos LLDP del router con caché de 45 s (tier lento, como los
// extras anti-parpadeo). Error o lldpd ausente → nil (sin datos; el FDB solo
// sigue mandando, comportamiento actual intacto). Devuelve además si lldpd
// NO está instalado (ErrLldpUnavailable) para que el view-model lo exponga
// (issue #247): hint "instala lldpd" en la UI.
func (l *Live) probeLldp(ctx context.Context, routerID string, client *OpenWrtClient) ([]LldpNeighbor, bool) {
	l.mu.Lock()
	e, ok := l.lldpCache[routerID]
	l.mu.Unlock()
	if ok && time.Since(e.at) < lldpCacheTTL {
		return e.neighbors, e.unavailable
	}
	neighbors, err := client.LldpNeighbors(ctx)
	unavailable := false
	if err != nil {
		if errors.Is(err, ErrLldpUnavailable) {
			unavailable = true
		} else {
			log.Printf("[netpulse] LLDP %s: %v", routerID, err)
		}
		neighbors = nil
	}
	l.mu.Lock()
	l.lldpCache[routerID] = lldpCacheEntry{neighbors: neighbors, unavailable: unavailable, at: time.Now()}
	l.mu.Unlock()
	return neighbors, unavailable
}

// getAdguardClient: kv (GL.iNet) con fallback a .env (AGH estándar).
func (l *Live) getAdguardClient() (std *AdGuardClient, gl *AdGuardGlinetClient) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := ""
	if l.db != nil {
		var host string
		if err := l.db.QueryRow("SELECT value FROM kv WHERE key='adguard_host'").Scan(&host); err == nil && host != "" {
			user := "root"
			_ = l.db.QueryRow("SELECT value FROM kv WHERE key='adguard_user'").Scan(&user)
			pass := ""
			_ = l.db.QueryRow("SELECT value FROM kv WHERE key='adguard_pass'").Scan(&pass)
			if pass != "" {
				key = "gl|" + host + "|" + user
				if l.agKey != key {
					l.agGL = NewAdGuardGlinetClient(host, user, pass, l.pool)
				}
			}
		}
	}
	if key == "" && l.cfg.Adguard != nil && l.cfg.Adguard.Pass != "" {
		key = "std|" + l.cfg.Adguard.URL
		if l.agKey != key {
			l.agStd = NewAdGuardClient(l.cfg.Adguard.URL, l.cfg.Adguard.User, l.cfg.Adguard.Pass)
		}
	}
	if key == "" {
		return nil, nil
	}
	if l.agKey != key {
		l.agKey = key
		if strings.HasPrefix(key, "gl|") {
			l.agStd = nil
		} else {
			l.agGL = nil
		}
	}
	return l.agStd, l.agGL
}

// histPoint es un punto de metricsHistory {t, down, up, cpu, ram, temp}.
type histPoint struct {
	t              string
	down, up       float64
	cpu, ram, temp int
}

var weekdayES = []string{"Dom", "Lun", "Mar", "Mié", "Jue", "Vie", "Sáb"}

// metricsHistory consulta las métricas históricas (index.js:122-147).
// metricsHistoryBuckets lee el histórico largo desde la escalera de retención
// (metrics_buckets, 5 min → 1 año). Usado para el rango 30d, cuyo raw ya se
// purgó (7 días). Agrega los buckets de 5 min a ventanas de `bucket` ms para
// limitar los puntos devueltos (~120 en 30d con ventanas de 6 h).
func (l *Live) metricsHistoryBuckets(routerID string, span, bucket int64, fmtLabel func(time.Time) string) []histPoint {
	if l.db == nil || routerID == "" {
		return []histPoint{}
	}
	now := time.Now().UnixMilli()
	rows, err := l.db.Query(
		`SELECT (bucket_ts / ?) AS win, AVG(rx_avg) AS rx, AVG(tx_avg) AS tx,
		        AVG(cpu_avg) AS cpu, AVG(ram_avg) AS ram, AVG(temp_avg) AS temp,
		        MIN(bucket_ts) AS t0
		 FROM metrics_buckets WHERE router_id = ? AND bucket_ts >= ?
		 GROUP BY win ORDER BY win`,
		bucket, routerID, now-span)
	if err != nil {
		return []histPoint{}
	}
	defer rows.Close()
	out := []histPoint{}
	for rows.Next() {
		var b, t0 int64
		var rx, tx, cpu, ram, temp sql.NullFloat64
		if err := rows.Scan(&b, &rx, &tx, &cpu, &ram, &temp, &t0); err != nil {
			continue
		}
		hp := histPoint{t: fmtLabel(time.UnixMilli(t0).Local())}
		if rx.Valid {
			hp.down = math.Round(rx.Float64/1e6*10) / 10
		}
		if tx.Valid {
			hp.up = math.Round(tx.Float64/1e6*10) / 10
		}
		if cpu.Valid {
			hp.cpu = int(math.Round(cpu.Float64))
		}
		if ram.Valid {
			hp.ram = int(math.Round(ram.Float64))
		}
		if temp.Valid {
			hp.temp = int(math.Round(temp.Float64))
		}
		out = append(out, hp)
	}
	return out
}

func (l *Live) metricsHistory(routerID, rang string) []histPoint {
	if l.db == nil || routerID == "" {
		return []histPoint{}
	}
	now := time.Now().UnixMilli()
	var span, bucket int64
	var fmtLabel func(time.Time) string
	switch rang {
	case "1h":
		span, bucket = 3600e3, 180e3
		fmtLabel = func(d time.Time) string { return fmt.Sprintf("%02d:%02d", d.Hour(), d.Minute()) }
	case "24h":
		span, bucket = 86400e3, 3600e3
		fmtLabel = func(d time.Time) string { return fmt.Sprintf("%02d", d.Hour()) }
	case "7d":
		span, bucket = 7*86400e3, 86400e3
		fmtLabel = func(d time.Time) string { return weekdayES[d.Weekday()] }
	case "30d":
		span, bucket = 30*86400e3, 6*3600e3
		fmtLabel = func(d time.Time) string { return fmt.Sprintf("%d", d.Day()) }
	default:
		return []histPoint{}
	}
	// Rango 30d: los raw se purgan a los 7 días, así que se lee de la escalera
	// de retención (metrics_buckets). El resto de rangos usa la tabla raw
	// (suficiente y más fino).
	if rang == "30d" {
		return l.metricsHistoryBuckets(routerID, span, bucket, fmtLabel)
	}
	rows, err := l.db.Query(
		`SELECT (ts / ?) AS bucket, AVG(rx_bps) AS rx, AVG(tx_bps) AS tx, AVG(cpu) AS cpu, AVG(ram) AS ram, AVG(temp) AS temp, MIN(ts) AS t0
		 FROM metrics WHERE router_id = ? AND ts >= ? GROUP BY bucket ORDER BY bucket`,
		bucket, routerID, now-span)
	if err != nil {
		return []histPoint{}
	}
	defer rows.Close()
	out := []histPoint{}
	for rows.Next() {
		var b, t0 int64
		var rx, tx, cpu, ram, temp sql.NullFloat64
		if err := rows.Scan(&b, &rx, &tx, &cpu, &ram, &temp, &t0); err != nil {
			continue
		}
		hp := histPoint{t: fmtLabel(time.UnixMilli(t0).Local())}
		if rx.Valid {
			hp.down = math.Round(rx.Float64/1e6*10) / 10
		}
		if tx.Valid {
			hp.up = math.Round(tx.Float64/1e6*10) / 10
		}
		if cpu.Valid {
			hp.cpu = int(math.Round(cpu.Float64))
		}
		if ram.Valid {
			hp.ram = int(math.Round(ram.Float64))
		}
		if temp.Valid {
			hp.temp = int(math.Round(temp.Float64))
		}
		out = append(out, hp)
	}
	return out
}

// wanDayStats rellena los campos del resumen WAN que solo el modo demo
// calculaba (issue #169): pico de hoy (Mbps + hora), media de bajada y total
// de 24 h, todo desde la tabla raw de métricas del gateway. La hora del pico
// usa el ts de la fila del máximo; el total estima bytes como
// SUM(rx_bps) × Δt (Δt = 86400/N s entre muestras, luego /8 bits→bytes).
// Sin BD, sin gateway o sin datos devuelve los valores cero/"—" de partida.
type wanDayStatsResult struct {
	peakMbps float64
	peakTime string
	avgMbps  float64
	totalStr string
}

func (l *Live) wanDayStats(gwID string) wanDayStatsResult {
	out := wanDayStatsResult{peakTime: "—", totalStr: "—"}
	if l.db == nil || gwID == "" {
		return out
	}
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
	start24h := now.Add(-24 * time.Hour).UnixMilli()

	// Pico de hoy: fila con MAX(rx_bps) y su ts (la hora del pico).
	row := l.db.QueryRow(
		`SELECT rx_bps, ts FROM metrics WHERE router_id = ? AND ts >= ? AND rx_bps IS NOT NULL
		 ORDER BY rx_bps DESC LIMIT 1`, gwID, startOfDay)
	var peakBps float64
	var peakTs int64
	if err := row.Scan(&peakBps, &peakTs); err == nil {
		out.peakMbps = math.Round(peakBps/1e6*10) / 10
		out.peakTime = time.UnixMilli(peakTs).Local().Format("15:04")
	}

	// Media 24h + total: AVG(rx_bps) y bytes estimados con el Δt medio.
	row = l.db.QueryRow(
		`SELECT AVG(rx_bps), SUM(rx_bps), COUNT(rx_bps) FROM metrics
		 WHERE router_id = ? AND ts >= ? AND rx_bps IS NOT NULL`, gwID, start24h)
	var avg, sum sql.NullFloat64
	var n sql.NullInt64
	if err := row.Scan(&avg, &sum, &n); err == nil && avg.Valid && sum.Valid && n.Valid && n.Int64 > 0 {
		out.avgMbps = math.Round(avg.Float64/1e6*10) / 10
		dt := float64(86400) / float64(n.Int64) // s entre muestras
		out.totalStr = fmtBytes(sum.Float64 * dt / 8)
	}
	return out
}

// pollRouter sondea un router; error si está inalcanzable. Si el router tiene
// agente nativo con payload fresco (Tier 2), el sondeo viene del último push
// y NO se toca SSH; si el agente expiró, se degrada a Tier 0 (SSH) con aviso.
func (l *Live) pollRouter(ctx context.Context, cfg RouterConfig) (*routerPolled, error) {
	if fresh, p := l.pollRouterAgent(cfg); fresh {
		// El agente no trae el estado WAN en el payload: si este router es el
		// gateway, lo sondeamos por SSH (issue #276, cache de 60 s).
		l.mu.Lock()
		gw := l.gatewayCfg
		client := l.clients[cfg.ID]
		l.mu.Unlock()
		if gw != nil && cfg.ID == gw.ID && client != nil {
			p.wanInfo = l.probeWanInfo(cfg.ID, client)
		}
		return p, nil
	}
	l.mu.Lock()
	client := l.clients[cfg.ID]
	gw := l.gatewayCfg
	layout, hasLayout := l.layoutCache[cfg.ID]
	board, hasBoard := l.boardCache[cfg.ID]
	cached := l.extrasCache[cfg.ID]
	l.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("router %s sin cliente", cfg.ID)
	}

	// Calienta la conexión SSH en serie (equivalente al `ssh true` del JS:
	// las llamadas de abajo multiplexan sobre la conexión ya establecida).
	if _, err := l.pool.Run(cfg.Host, "true", 0); err != nil {
		return nil, err
	}
	// Layout de puertos (board.json): se lee una vez y se cachea
	if !hasLayout {
		if lay, err := client.GetPortLayout(); err == nil && len(lay) > 0 {
			layout = lay
			l.mu.Lock()
			l.layoutCache[cfg.ID] = lay
			l.mu.Unlock()
		}
	}
	// sysInfo es la única sonda obligatoria (sin catch en el JS)
	sysInfo, err := client.GetSysInfo()
	if err != nil {
		return nil, err
	}
	cpu, _ := client.GetCPUPercent()
	temp, _ := client.GetTempC()
	net, nerr := client.GetNetDevBps()
	if nerr != nil || net == nil {
		net = &NetDevBps{}
	}
	leases := client.GetDhcpLeases()
	// gl-clients (GL.iNet): complementa la resolución de IP donde dnsmasq no
	// tiene lease (issue #5 bug 1). En routers sin el objeto ubus sale vacío
	// — coste: una llamada ubus local por poll.
	glClients := client.GetGlClients()
	wireless := client.GetWirelessClients()
	ports := client.GetEthPorts(layout)
	radios := client.GetRadios()
	fdb := client.GetBridgeFdb()
	brMac := client.GetBridgeMac()
	// Tier lento: backhaul (5 min) y vecinos LLDP (45 s), ambos cacheados y
	// tolerantes a error (router sin wifi/lldpd → campo ausente, sin romper).
	backhaul := l.probeBackhaul(cfg.ID, client)
	lldp, lldpUnavailable := l.probeLldp(ctx, cfg.ID, client)
	if !hasBoard {
		if b, err := client.GetBoard(); err == nil {
			board = b
			l.mu.Lock()
			l.boardCache[cfg.ID] = b
			l.mu.Unlock()
		}
	}

	// Anti-parpadeo: conserva la última lista buena en sondas puntuales
	// (null = sonda fallida; colección vacía = resultado real).
	if cached == nil {
		cached = &extrasSnapshot{ports: []EthPort{}, radios: []Radio{},
			wireless: map[string]WirelessClient{}, fdb: map[string]string{}}
	}
	portsGood := cached.ports
	if len(ports) > 0 {
		portsGood = ports
	}
	radiosGood := cached.radios
	if len(radios) > 0 {
		radiosGood = radios
	}
	wirelessGood := cached.wireless
	if wireless != nil {
		wirelessGood = wireless
	}
	fdbGood := cached.fdb
	if fdb != nil {
		fdbGood = fdb
	}
	luciGood := cached.luci
	if luci := client.GetLuCILabels(); luci != nil {
		luciGood = luci
	}
	l.mu.Lock()
	l.extrasCache[cfg.ID] = &extrasSnapshot{ports: portsGood, radios: radiosGood, wireless: wirelessGood, fdb: fdbGood, luci: luciGood}
	l.mu.Unlock()

	// Uso real de RAM como en la UI del router: used = total − available
	mem := sysInfo.Memory
	ramPct := 0
	if mem.Total > 0 {
		avail := mem.Available
		if avail == 0 {
			avail = mem.Free + mem.Buffered
		}
		ramPct = int(math.Round((mem.Total - avail) / mem.Total * 100))
	}

	isGw := gw != nil && cfg.ID == gw.ID
	var latencyMs, lossPct *float64
	if isGw {
		latencyMs, lossPct, _ = client.GetWanLatency("")
	} else if gw != nil {
		latencyMs, _ = client.GetGatewayLatency(gw.Host)
	}
	// Estado WAN solo en el gateway (issue #276), tier lento cacheado.
	var wanInfo probe.WanInfo
	if isGw {
		wanInfo = l.probeWanInfo(cfg.ID, client)
	}

	cpuV := 0
	if cpu != nil {
		cpuV = *cpu
	}
	tempV := 0
	if temp != nil {
		tempV = *temp
	}
	return &routerPolled{
		cfg: cfg, client: client, sysInfo: sysInfo, board: board,
		cpu: cpuV, ram: ramPct, temp: tempV,
		uptimeSec: sysInfo.Uptime, net: net, leases: leases, glClients: glClients,
		wireless: wirelessGood, ports: portsGood, radios: radiosGood,
		fdb: fdbGood, brMac: brMac, latencyMs: latencyMs, lossPct: lossPct,
		backhaul: backhaul, lldp: lldp, lldpUnavailable: lldpUnavailable,
		luci: luciGood, wanInfo: wanInfo,
	}, nil
}

// firmwareOutdated decide si el firmware instalado no cumple el target
// configurado por el admin (issue #241). Comparación tolerante e
// insensible a mayúsculas: el target debe aparecer (Contains) dentro de la
// descripción del firmware ("OpenWrt 25.12.5 r33051..." contiene "25.12.5").
// Sin target ("" tras trim) → false (no hay comprobación).
func firmwareOutdated(installed, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	return !strings.Contains(strings.ToLower(strings.TrimSpace(installed)), strings.ToLower(target))
}

// buildRouter construye el Router del contrato (index.js:221-249).
func (l *Live) buildRouter(p *routerPolled, history []histPoint) Router {
	l.mu.Lock()
	gw := l.gatewayCfg
	l.mu.Unlock()
	model := p.cfg.Name
	if model == "" {
		model = p.cfg.Host
	}
	if p.board != nil && p.board.Model != "" {
		model = p.board.Model
	}
	name := p.cfg.Name
	if name == "" {
		name = p.cfg.Host
	}
	if p.board != nil && p.board.Hostname != "" {
		name = p.board.Hostname
	}
	health := 100
	if p.cpu > 85 {
		health -= 20
	} else if p.cpu > 70 {
		health -= 10
	}
	if p.temp > 75 {
		health -= 25
	} else if p.temp > 65 {
		health -= 12
	}
	if p.ram > 90 {
		health -= 15
	}
	// issue #241: firmware fuera del target configurado penaliza la salud.
	outdatedFw := false
	if p.cfg.FirmwareTarget != "" && p.board != nil && firmwareOutdated(p.board.Release.Description, p.cfg.FirmwareTarget) {
		health -= 5
		outdatedFw = true
	}
	if health < 0 {
		health = 0
	}
	isGw := gw != nil && p.cfg.ID == gw.ID
	status := "online"
	if p.temp > 65 || p.cpu > 85 {
		status = "warn"
	}
	sparkline := make([]float64, 0, len(history))
	for _, h := range history {
		sparkline = append(sparkline, h.down)
	}
	r := Router{
		ID: p.cfg.ID, Name: name, Model: model, ModelShort: model,
		IP: p.cfg.Host, Status: status, Health: health,
		CPU: iptr(p.cpu), RAM: iptr(p.ram), Temp: iptr(p.temp),
		Uptime: fmtUptime(p.uptimeSec), Clients: len(p.leases),
		Sparkline: sparkline,
	}
	if isGw {
		r.Role, r.RoleBadge = "Gateway principal", "Principal"
	} else if p.cfg.AgentOnly {
		r.Role, r.RoleBadge = "Switch", "SW"
	} else {
		r.Role, r.RoleBadge = "Punto de acceso", "AP"
	}
	if p.brMac != "" {
		r.MAC = p.brMac
	}
	if p.backhaul != "" {
		r.Backhaul = p.backhaul
	}
	if uplink := l.uplinkLldp(p); uplink != nil {
		r.Lldp = uplink
	}
	if p.lldpUnavailable {
		r.LldpAvailable = bptr(false)
	}
	if p.board != nil {
		r.Firmware = p.board.Release.Description
	}
	if p.cfg.FirmwareTarget != "" {
		r.FirmwareTarget = p.cfg.FirmwareTarget
	}
	if p.cfg.AgentOnly {
		r.AgentOnly = true
	}
	if p.cfg.Type != "" {
		r.Type = p.cfg.Type
	}
	if outdatedFw {
		r.FirmwareOutdated = true
		// Alerta no urgente (category system); el engine aplica dedup 5 min.
		l.engine.Emit(AlertEvent{
			ID:       fmt.Sprintf("alert-firmware-%s-%d", p.cfg.ID, time.Now().UnixMilli()),
			Category: alerts.CatSystem, Urgent: false,
			Severity:    "warn",
			Title:       "Firmware desactualizado",
			Description: fmt.Sprintf("%s: firmware %q no coincide con el target %q", name, r.Firmware, p.cfg.FirmwareTarget),
			Time:        "ahora mismo", RouterID: p.cfg.ID,
		})
	}
	if p.temp > 65 {
		r.HotMetric = "temp"
	}
	return r
}

// uplinkLldp: vecino LLDP del router que es OTRO router conocido → el uplink
// está identificado por LLDP y la app muestra el sufijo "· LLDP". El vecino se
// casa por chassis-MAC = bridge MAC de otro router (matching original) o, si
// el chassis-ID que anuncia lldpd difiere de la br-lan (habitual en OpenWrt,
// issue #252), por su mgmt-IP o nombre del chasis. Si el FDB dice dónde se
// aprendió esa MAC, el anuncio debe llegar por ese puerto (uplink); sin FDB,
// la MAC ya es evidencia suficiente. nil si no hay dato.
func (l *Live) uplinkLldp(p *routerPolled) *LldpInfo {
	if len(p.lldp) == 0 {
		return nil
	}
	l.mu.Lock()
	polled := l.lastPolled
	l.mu.Unlock()
	routers := routerIdentities(polled)
	for i := range p.lldp {
		nb := &p.lldp[i]
		if neighborIsRouter(nb, routers, p.cfg.ID) == nil {
			continue
		}
		if port, ok := p.fdb[nb.ChassisMac]; ok && port != nb.Port {
			continue
		}
		return nb.info()
	}
	return nil
}

// offlineRouter: último bueno marcado offline o placeholder (index.js:251-272).
// issue #257: si el último fallo fue de ACCESO (el router responde pero la
// clave SSH no está autorizada), el estado es "unreachable" + accessMissing,
// no un "offline" de apagado/inalcanzable (config issue, no power issue).
func (l *Live) offlineRouter(cfg RouterConfig) Router {
	l.mu.Lock()
	prev := l.lastGood[cfg.ID]
	gw := l.gatewayCfg
	l.mu.Unlock()
	var r Router
	if prev != nil {
		r = *prev
	} else {
		model := "OpenWrt"
		if cfg.Type == "glinet" {
			model = "GL.iNet"
		}
		name := cfg.Name
		if name == "" {
			name = cfg.Host
		}
		r = Router{
			ID: cfg.ID, Name: name, Model: model, ModelShort: model,
			IP: cfg.Host, Health: 0,
			CPU: iptr(0), RAM: iptr(0), Temp: iptr(0),
			Uptime: "—", Clients: 0, Sparkline: []float64{},
		}
		if gw != nil && cfg.ID == gw.ID {
			r.Role, r.RoleBadge = "Gateway principal", "Principal"
		} else {
			r.Role, r.RoleBadge = "Punto de acceso", "AP"
		}
	}
	r.Status = "offline"
	if l.accessMissing(cfg.ID) {
		r.Status = "unreachable"
		r.AccessMissing = true
	}
	return r
}

// accessMissing: el último fallo de sondeo de este router fue de ACCESO
// (SSH responde pero la clave no está autorizada / ubus rechaza), es decir
// el router está vivo pero el servidor no puede entrar (issue #257).
func (l *Live) accessMissing(routerID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return isAccessError(l.lastErr[routerID])
}

// isAccessError: ¿el error indica que el router RESPONDE pero el acceso
// SSH/ubus no está configurado? (handshake SSH que rechaza la clave del
// servidor). Un fallo de conexión (refused/timeout/red) NO es un fallo de
// acceso: ahí el router puede estar apagado o inalcanzable (issue #257).
func isAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, hint := range []string{
		"unable to authenticate",
		"no supported methods",
		"permission denied",
		"authentication failed",
		"not authorized",
	} {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

// pollAll sondea todos los routers en paralelo (Promise.allSettled).
func (l *Live) pollAll(ctx context.Context) map[string]*routerPolled {
	l.mu.Lock()
	routers := append([]RouterConfig(nil), l.routers...)
	l.mu.Unlock()

	type result struct {
		cfg RouterConfig
		p   *routerPolled
		err error
	}
	results := make([]result, len(routers))
	var wg sync.WaitGroup
	for i, cfg := range routers {
		wg.Add(1)
		go func(i int, cfg RouterConfig) {
			defer wg.Done()
			p, err := l.pollRouter(ctx, cfg)
			results[i] = result{cfg, p, err}
		}(i, cfg)
	}
	wg.Wait()

	polled := map[string]*routerPolled{}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, res := range results {
		if res.err == nil {
			polled[res.cfg.ID] = res.p
			l.failCount[res.cfg.ID] = 0
			delete(l.lastErr, res.cfg.ID)
			// Persiste la bridgeMAC del router (issue #196): la exclusión de
			// "dispositivo desconocido" no debe depender de que este router se
			// sondee bien en el tick actual.
			if l.db != nil && res.p.brMac != "" {
				_, _ = l.db.Exec("UPDATE routers SET mac = ? WHERE id = ?", res.p.brMac, res.cfg.ID)
			}
			// Router recuperado (offline→online, SPEC-ALERTAS §1)
			if l.lastStatus[res.cfg.ID] == "offline" {
				name := res.cfg.Name
				if name == "" {
					name = res.cfg.Host
				}
				// issue #256: la alerta crítica de offline pendiente se resuelve
				// (marca leída) al volver online; sin esto queda "no leída" para
				// siempre y con APs que flapean se acumulan caídas fantasma.
				l.resolveOfflineAlerts(res.cfg.ID)
				l.emitRouterRecovered(res.cfg.ID, name)
			}
			l.lastStatus[res.cfg.ID] = "online"
			// WAN/Internet caído: gateway OK pero ping a internet con 100 %
			// de pérdida en 2 sondeos seguidos (mismo debounce que offline).
			if l.gatewayCfg != nil && res.cfg.ID == l.gatewayCfg.ID {
				l.trackWanDown(&res.cfg, res.p)
			}
			continue
		}
		fails := l.failCount[res.cfg.ID] + 1
		l.failCount[res.cfg.ID] = fails
		l.lastErr[res.cfg.ID] = res.err
		log.Printf("[netpulse] router %s inalcanzable (%d): %v", res.cfg.ID, fails, res.err)
		// Alerta solo tras 2 fallos seguidos (un fallo suelto no es una caída).
		// issue #257: un fallo de ACCESO (SSH responde pero la clave no está
		// autorizada) no es una caída — el router está vivo; la UI lo marca
		// como "sin acceso" y no merece una alerta crítica de offline.
		accessErr := isAccessError(res.err)
		if fails >= 2 && l.lastStatus[res.cfg.ID] != "offline" && !accessErr {
			name := res.cfg.Name
			if name == "" {
				name = res.cfg.Host
			}
			l.engine.Emit(AlertEvent{
				ID:       fmt.Sprintf("alert-offline-%s-%d", res.cfg.ID, time.Now().UnixMilli()),
				Category: alerts.CatRouter, Urgent: true,
				Severity:    "critical",
				Title:       name + " offline",
				Description: fmt.Sprintf("Sin respuesta de %s: %v", res.cfg.Host, res.err),
				Time:        "ahora mismo", RouterID: res.cfg.ID,
			})
		}
		if fails >= 2 {
			l.lastStatus[res.cfg.ID] = "offline"
		}
	}
	l.lastPolled = polled
	return polled
}

// emitRouterRecovered: evento "router recuperado" (offline→online;
// category router, urgent false, severity ok — SPEC-ALERTAS §1).
// Debe llamarse con l.mu tomado (mismo contexto que el resto de emisiones).
func (l *Live) emitRouterRecovered(routerID, name string) {
	l.engine.Emit(AlertEvent{
		ID:       fmt.Sprintf("alert-recovered-%s-%d", routerID, time.Now().UnixMilli()),
		Category: alerts.CatRouter, Urgent: false,
		Severity:    "ok",
		Title:       name + " recuperado",
		Description: fmt.Sprintf("%s vuelve a responder", name),
		Time:        "ahora mismo", RouterID: routerID,
	})
}

// resolveOfflineAlerts resuelve las alertas de offline pendientes del router
// (issue #256): al volver online se marcan como leídas, de modo que la caída
// crítica no queda "no leída para siempre" y un AP que flapea no acumula
// caídas fantasma. El evento "recuperado" (emitRouterRecovered) es el relevo
// positivo que sustituye la caída en el feed. Debe llamarse con l.mu tomado.
func (l *Live) resolveOfflineAlerts(routerID string) {
	ids := []string{}
	for _, ev := range l.engine.List() {
		if ev.RouterID == routerID && strings.HasPrefix(ev.ID, "alert-offline-") {
			ids = append(ids, ev.ID)
		}
	}
	if len(ids) > 0 {
		l.engine.MarkRead(ids...)
	}
}

// trackWanDown detecta "WAN/Internet caído" (category internet, urgent true,
// critical — SPEC-ALERTAS §1): el gateway responde por SSH pero su ping a
// internet da 100 % de pérdida en 2 sondeos seguidos (debounce como offline).
// Al recuperarse la WAN se resetea el estado (la próxima caída vuelve a
// alertar). Debe llamarse con l.mu tomado.
func (l *Live) trackWanDown(cfg *RouterConfig, p *routerPolled) {
	key := cfg.ID + ":wan"
	if p == nil || p.lossPct == nil || *p.lossPct < 100 {
		l.wanDown[cfg.ID] = 0
		l.lastStatus[key] = "up"
		return
	}
	l.wanDown[cfg.ID]++
	if l.wanDown[cfg.ID] >= 2 && l.lastStatus[key] != "down" {
		l.lastStatus[key] = "down"
		name := cfg.Name
		if name == "" {
			name = cfg.Host
		}
		l.engine.Emit(AlertEvent{
			ID:       fmt.Sprintf("alert-wan-%s-%d", cfg.ID, time.Now().UnixMilli()),
			Category: alerts.CatInternet, Urgent: true,
			Severity:    "critical",
			Title:       "Internet caído",
			Description: fmt.Sprintf("%s responde pero no alcanza internet (100 %% de pérdida)", name),
			Time:        "ahora mismo", RouterID: cfg.ID,
		})
	}
}

// trackUnknownDevices emite "dispositivo desconocido se conecta" cuando un
// cliente SIN nombre (Name == MAC, sin hostname DHCP ni alias — device_attrib
// no guarda alias) pasa de no-online a online y PERMANECE nameless durante
// `unknownGraceNum` ticks seguidos (issue #234): un dispositivo conocido que
// reconecta resuelve su lease DHCP en 1-2 ticks y nunca alcanza el umbral.
// La memoria per-MAC persistida (issue #248) garantiza que cada MAC desconocida
// alerta UNA sola vez en la vida del servidor, hasta que se confíe en
// Settings (known_macs) o se resete. El primer ciclo de sondeo del proceso
// NO alerta (evita la avalancha de arranque: todo lo ya conectado sería
// "nuevo"). Las MAC de la allowlist known_macs (issue #196) nunca alertan,
// haya alias o no. Toma l.mu internamente.
func (l *Live) trackUnknownDevices(devices []Device) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// issue #196: MACs de la allowlist (confiables) → nunca "desconocido".
	trusted := map[string]bool{}
	if l.db != nil {
		if rows, err := l.db.Query("SELECT mac FROM known_macs"); err == nil {
			for rows.Next() {
				var mac string
				if rows.Scan(&mac) == nil {
					trusted[mac] = true
				}
			}
			rows.Close()
		}
	}
	nowOnline := map[string]bool{}
	for _, d := range devices {
		if !d.Online {
			continue
		}
		nowOnline[d.MAC] = true
		if trusted[d.MAC] {
			delete(l.unknownGrace, d.MAC)
			continue
		}
		// issue #234: con hostname (lease resuelto) el dispositivo es
		// conocido; se resetea su gracia y nunca alerta.
		if d.Name != d.MAC {
			delete(l.unknownGrace, d.MAC)
			continue
		}
		// issue #248: esta MAC ya alertó alguna vez → no vuelve a hacerlo.
		if l.unknownAlerted[d.MAC] {
			delete(l.unknownGrace, d.MAC)
			continue
		}
		// Solo los dispositivos que ACABAN de conectarse (o que ya estaban en
		// gracia) acumulan ticks; los conectados antes del arranque no entran.
		_, counting := l.unknownGrace[d.MAC]
		if l.seenOnlineMacs && (counting || !l.onlineMacs[d.MAC]) {
			l.unknownGrace[d.MAC]++
			if l.unknownGrace[d.MAC] >= l.unknownGraceNum {
				l.emitUnknownDevice(d)
				l.unknownAlerted[d.MAC] = true
				l.persistUnknownAlerted(d.MAC)
				delete(l.unknownGrace, d.MAC)
			}
		}
	}
	// La gracia solo acumula en estado "online + sin nombre": cualquier otra
	// transición la resetea (una reconexión futura parte de cero).
	for mac := range l.unknownGrace {
		if !nowOnline[mac] {
			delete(l.unknownGrace, mac)
		}
	}
	l.onlineMacs = nowOnline
	l.seenOnlineMacs = true
}

// persistUnknownAlerted guarda en kv la memoria per-MAC (issue #248) para que
// sobreviva a reinicios del servidor. Con db nil (demo/tests) es no-op.
// Debe llamarse con l.mu tomado.
func (l *Live) persistUnknownAlerted(mac string) {
	if l.db == nil {
		return
	}
	_, _ = l.db.Exec("INSERT INTO kv (key, value) VALUES (?, '1') ON CONFLICT(key) DO NOTHING", "unknown_alerted:"+mac)
}

// emitUnknownDevice: evento "dispositivo desconocido se conecta" (category
// clients, warn, NO urgente — issue #196). "Desconocido" = sin nombre/alias:
// device_attrib no guarda alias, así que la señal práctica es un cliente sin
// hostname DHCP (Name == MAC). Debe llamarse con l.mu tomado.
func (l *Live) emitUnknownDevice(d Device) {
	l.engine.Emit(AlertEvent{
		ID:       fmt.Sprintf("alert-unknown-%s-%d", d.MAC, time.Now().UnixMilli()),
		Category: alerts.CatClients, Urgent: false,
		Severity:    "warn",
		Title:       "Dispositivo desconocido",
		Description: fmt.Sprintf("%s se ha conectado a %s", d.MAC, d.RouterID),
		Time:        "ahora mismo", RouterID: d.RouterID,
	})
}

// trackDevicePresence emite device_offline/device_online cuando una MAC
// wireless pasa de vista a no-vista (N ticks) o viceversa (issue #184).
// Solo se rastrean MACs wireless (Band != "cable"): el FDB de cableados es
// volátil y daría falsos offline. Anti-falsos positivos: un tick perdido no
// dispara — hace falta `presenceOfflineAfter` ticks seguidos sin verse.
// Anti-arranque: el primer ciclo solo puebla el estado (todo lo ya conectado
// en boot no genera online). Poda: una MAC offline desde hace muchos ticks
// (dispositivo que se fue para siempre) se elimina de los mapas para que no
// crezcan sin límite (issue #206). Toma l.mu.
func (l *Live) trackDevicePresence(devices []Device, nowMs int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	for _, d := range devices {
		if !d.Online || d.Band == "cable" {
			continue
		}
		seen[d.MAC] = true
		if l.presenceSeen && !l.devicePresence[d.MAC] {
			l.insertDeviceEvent(d.MAC, d.RouterID, deviceevents.StateOnline, d.SignalDbm, nowMs)
		}
		l.devicePresence[d.MAC] = true
		l.presenceMisses[d.MAC] = 0
	}
	for mac, wasOnline := range l.devicePresence {
		if !wasOnline {
			// Ya offline: la MAC no re-emite, pero el contador sigue
			// creciendo para que la poda pueda eliminarla (issue #206).
			l.presenceMisses[mac]++
			continue
		}
		if seen[mac] {
			continue
		}
		l.presenceMisses[mac]++
		if l.presenceMisses[mac] >= l.presenceOfflineAfter {
			l.insertDeviceEvent(mac, l.lastRouterFor(mac), deviceevents.StateOffline, nil, nowMs)
			l.devicePresence[mac] = false
		}
	}
	// Poda: MACs offline desde hace muchos ticks (se fueron para siempre) se
	// eliminan para que los mapas no crezcan sin límite (issue #206).
	// Zero-value (pruneAfter==0) = poda desactivada.
	if l.presencePruneAfter > 0 {
		for mac, misses := range l.presenceMisses {
			if misses >= l.presencePruneAfter {
				delete(l.presenceMisses, mac)
				delete(l.devicePresence, mac)
			}
		}
	}
	l.presenceSeen = true
}

// insertDeviceEvent persiste una transición de presencia. Con db nil
// (demo/tests sin BD) es no-op. Debe llamarse con l.mu tomado.
func (l *Live) insertDeviceEvent(mac, routerID, state string, signalDbm *int, nowMs int64) {
	if l.db == nil {
		return
	}
	_ = deviceevents.Insert(l.db.DB, deviceevents.Event{
		TsMs: nowMs, MAC: mac, RouterID: routerID, State: state,
		SignalDbm: signalDbm,
	})
}

// lastRouterFor devuelve el último router conocido de una MAC desde
// device_attrib (o vacío si no hay registro). Debe llamarse con l.mu tomado.
func (l *Live) lastRouterFor(mac string) string {
	if l.db == nil {
		return ""
	}
	var routerID string
	_ = l.db.QueryRow("SELECT router_id FROM device_attrib WHERE mac = ?", mac).Scan(&routerID)
	return routerID
}

// pollAdGuard: stats del cliente configurado; fallback inactivo si falla.
func (l *Live) pollAdGuard(ctx context.Context) *AdGuardStats {
	std, gl := l.getAdguardClient()
	if std == nil && gl == nil {
		return nil
	}
	var stats *AdGuardStats
	var err error
	host := ""
	if std != nil {
		u := std.url
		if i := strings.Index(u, "://"); i >= 0 {
			u = u[i+3:]
		}
		host = strings.SplitN(u, ":", 2)[0]
		stats, err = std.GetStats(ctx)
	} else {
		host = gl.Host
		stats, err = gl.GetStats(ctx)
	}
	if err != nil {
		log.Printf("[netpulse] AdGuard inalcanzable: %v", err)
		return &AdGuardStats{
			Host: host, Port: 3000, Status: "inactive",
			TopBlocked: []TopBlocked{},
		}
	}
	return stats
}

// pollWireGuard: stats WG del gateway + alerta de handshake nuevo.
func (l *Live) pollWireGuard(devices []Device) *WireGuardStats {
	l.mu.Lock()
	gw := l.gatewayCfg
	l.mu.Unlock()
	if gw == nil {
		return nil
	}
	peerNames := map[string]WGPeerName{}
	for _, d := range devices {
		if d.IP != "" {
			peerNames[d.IP] = WGPeerName{ID: d.ID, Name: d.Name, Type: d.Type}
		}
	}
	stats, err := GetWireGuardStats(l.pool, gw.Host, l.cfg.WGInterface, "", peerNames)
	if err != nil {
		log.Printf("[netpulse] WireGuard no disponible: %v", err)
		return &WireGuardStats{Interface: l.cfg.WGInterface, Subnet: "", Status: "inactive", Peers: []WGPeer{}}
	}
	// Alerta en handshake nuevo (peer pasa a activo)
	activeNow := map[string]bool{}
	for _, p := range stats.Peers {
		if p.Active {
			activeNow[p.ID] = true
		}
	}
	l.mu.Lock()
	for id := range activeNow {
		if !l.wgActive[id] && len(l.wgActive) > 0 {
			name := id
			for _, p := range stats.Peers {
				if p.ID == id {
					name = p.Name
				}
			}
			l.engine.Emit(AlertEvent{
				ID:       fmt.Sprintf("alert-wg-%s-%d", id, time.Now().UnixMilli()),
				Category: alerts.CatVPN, Urgent: false,
				Severity: "info", Title: "Handshake WireGuard",
				Description: name + " conectado",
				Time:        "ahora mismo", RouterID: gw.ID,
			})
		}
	}
	l.wgActive = activeNow
	l.mu.Unlock()
	return stats
}

// buildDevices: unión de leases + MACs vistas (wireless > FDB satélites >
// FDB gateway si no hay memoria) + device_attrib (index.js:396-460).
func (l *Live) buildDevices(polled map[string]*routerPolled) []Device {
	leasesByMac := map[string]DhcpLease{}
	glByMac := map[string]DhcpLease{}
	for _, p := range polled {
		for _, le := range p.leases {
			if le.MAC != "" {
				leasesByMac[le.MAC] = le
			}
		}
		// gl-clients: fallback de IP para MACs sin lease (dnsmasq sin ese
		// cliente). No crea dispositivos: solo enriquece los ya resueltos
		// por wireless/FDB (issue #5 bug 1).
		for _, le := range p.glClients {
			if le.MAC != "" && le.IP != "" {
				glByMac[le.MAC] = le
			}
		}
	}
	type knownInfo struct {
		routerID string
		band     string
		signal   *int
	}
	known := map[string]knownInfo{}
	if l.db != nil {
		rows, err := l.db.Query("SELECT mac, router_id, band, signal_dbm FROM device_attrib")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var mac, routerID, band string
				var signal sql.NullInt64
				if rows.Scan(&mac, &routerID, &band, &signal) == nil {
					k := knownInfo{routerID: routerID, band: band}
					if signal.Valid {
						v := int(signal.Int64)
						k.signal = &v
					}
					known[mac] = k
				}
			}
		}
	}
	now := time.Now().UnixMilli()

	type seenInfo struct {
		routerID string
		band     string
		signal   *int
	}
	seen := map[string]seenInfo{}
	// (1) wireless de cualquier router: la ÚNICA que se persiste
	for routerID, p := range polled {
		for mac, w := range p.wireless {
			sig := w.SignalDbm
			seen[mac] = seenInfo{routerID, w.Band, &sig}
			if l.db != nil {
				_, _ = l.db.Exec(
					`INSERT INTO device_attrib (mac, router_id, band, signal_dbm, last_seen) VALUES (?, ?, ?, ?, ?)
					 ON CONFLICT(mac) DO UPDATE SET router_id=excluded.router_id, band=excluded.band, signal_dbm=excluded.signal_dbm, last_seen=excluded.last_seen`,
					mac, routerID, w.Band, w.SignalDbm, now)
			}
		}
	}
	l.mu.Lock()
	gw := l.gatewayCfg
	l.mu.Unlock()
	gwID := ""
	if gw != nil {
		gwID = gw.ID
	}
	// (2) FDB de satélites: pista solo de ESTE tick (no se guarda).
	// REGLA DE RECONCILIACIÓN: si una MAC cableada aparece tanto en el FDB
	// del gateway como en el FDB de un satélite, el satélite la ve porque
	// está bridged al mismo segmento L2 — NO es un cliente directo del
	// satélite. Solo cuentan como clientes del satélite las MACs que el
	// gateway NO ve (dispositivos realmente tras ese satélite, sin bridge
	// al gateway).
	// EXCEPCIÓN: los routers agent-only (switches con agente propio) son la
	// fuente más específica — sus dispositivos se conservan con RouterID
	// del switch, aunque el gateway también los vea en su FDB.
	gwFDB := map[string]bool{}
	if gwID != "" {
		if gwPolled := polled[gwID]; gwPolled != nil {
			for mac := range gwPolled.fdb {
				gwFDB[mac] = true
			}
		}
	}
	for routerID, p := range polled {
		if routerID == gwID {
			continue
		}
		for mac := range p.fdb {
			if _, ok := seen[mac]; !ok {
				if gwFDB[mac] && !p.cfg.AgentOnly {
					continue // gateway bridge pasivo, no es cliente del satélite
				}
				seen[mac] = seenInfo{routerID, "cable", nil}
			}
		}
	}
	// (3) FDB del gateway: solo si no hay memoria (mala pista; no se guarda)
	if gwID != "" {
		if gwPolled := polled[gwID]; gwPolled != nil {
			for mac := range gwPolled.fdb {
				if _, ok := seen[mac]; !ok {
					if _, ok := known[mac]; !ok {
						seen[mac] = seenInfo{gwID, "cable", nil}
					}
				}
			}
		}
	}

	allMacs := map[string]bool{}
	for mac := range leasesByMac {
		allMacs[mac] = true
	}
	for mac := range seen {
		allMacs[mac] = true
	}
	for mac := range known {
		allMacs[mac] = true
	}
	routerMacs := map[string]bool{}
	// issue #196: MACs de bridge de los routers REGISTRADOS (persistidas),
	// no solo las de los routers que sondeó bien en este tick. Un router que
	// falló el sondeo sigue sin ser "cliente desconocido".
	if l.db != nil {
		if rows, err := l.db.Query("SELECT mac FROM routers WHERE mac IS NOT NULL AND mac != ''"); err == nil {
			for rows.Next() {
				var mac string
				if rows.Scan(&mac) == nil {
					routerMacs[mac] = true
				}
			}
			rows.Close()
		}
	}
	for _, p := range polled {
		if p.brMac != "" {
			routerMacs[p.brMac] = true
		}
	}
	// issue #196: allowlist de dispositivos confiables (mac → alias).
	knownMacs := map[string]string{}
	if l.db != nil {
		if rows, err := l.db.Query("SELECT mac, name FROM known_macs"); err == nil {
			for rows.Next() {
				var mac, name string
				if rows.Scan(&mac, &name) == nil {
					knownMacs[mac] = name
				}
			}
			rows.Close()
		}
	}
	devices := []Device{}
	for mac := range allMacs {
		if routerMacs[mac] {
			continue // los routers no son clientes
		}
		lease, hasLease := leasesByMac[mac]
		s, isSeen := seen[mac]
		d := Device{
			ID:  strings.ToLower(strings.ReplaceAll(mac, ":", "-")),
			MAC: mac, Manufacturer: "Desconocido",
			TrafficMbps: 0, Sparkline: []float64{},
			RouterID: gwID, Band: "—",
			Online: isSeen,
		}
		if hasLease {
			if lease.Hostname != "" {
				d.Name = lease.Hostname
			} else {
				d.Name = mac
			}
			d.IP = lease.IP
		} else {
			d.Name = mac
			// Fallback gl-clients (GL.iNet): el cliente no tiene lease pero el
			// firmware sí conoce su IP (y a veces nombre). (issue #5 bug 1)
			if gl, ok := glByMac[mac]; ok {
				d.IP = gl.IP
				if gl.Hostname != "" {
					d.Name = gl.Hostname
				}
			}
		}
		// issue #196: la allowlist manda sobre el nombre por defecto. Con
		// alias (Name != MAC) el dispositivo deja de ser "desconocido".
		if alias, ok := knownMacs[mac]; ok && alias != "" {
			d.Name = alias
		}
		// Tipo estimado por hostname (el DHCP/FDB no dice qué es el cliente).
		// Con nombre-MAC (sin hostname) queda "desconocido".
		d.Type = GuessDeviceType(d.Name, d.Manufacturer)
		if isSeen {
			d.RouterID = s.routerID
			d.Band = s.band
			d.SignalDbm = s.signal
		} else if k, ok := known[mac]; ok {
			d.RouterID = k.routerID
			d.Band = k.band
			d.SignalDbm = k.signal
		}
		devices = append(devices, d)
	}
	return devices
}

// computeHealth (index.js:462-486).
func computeHealth(routers []Router, adguard *AdGuardStats) HealthScore {
	score := 100
	breakdown := []HealthDelta{}
	for _, r := range routers {
		if r.Status == "offline" {
			score -= 30
			breakdown = append(breakdown, HealthDelta{Label: r.Name + " offline", Delta: -30})
		} else if r.Temp != nil && *r.Temp > 65 {
			score -= 8
			breakdown = append(breakdown, HealthDelta{Label: "temp. " + r.Name, Delta: -8})
		}
	}
	if adguard != nil && adguard.Status != "active" {
		score -= 5
		breakdown = append(breakdown, HealthDelta{Label: "AdGuard inactivo", Delta: -5})
	}
	if score < 0 {
		score = 0
	}
	label := "Atención"
	if score >= 85 {
		label = "Excelente"
	} else if score >= 65 {
		label = "Bueno"
	}
	note := "Sin penalizaciones."
	if len(breakdown) > 0 {
		labels := make([]string, len(breakdown))
		for i, b := range breakdown {
			labels[i] = b.Label
		}
		note = "Penalizado por: " + strings.Join(labels, ", ") + "."
	}
	return HealthScore{
		Score: score, Label: label, Caption: "Puntuación de salud de la red",
		Note: note, Breakdown: breakdown,
	}
}

// defaultWan (index.js:488-503).
func (l *Live) defaultWan(gw *RouterConfig) WAN {
	l.mu.Lock()
	var p *routerPolled
	if gw != nil {
		p = l.lastPolled[gw.ID]
	}
	l.mu.Unlock()
	wan := WAN{
		Plan: "—", PublicIP: "—", Isp: "—", PeakTodayTime: "—", Total24h: "—",
	}
	if p != nil {
		if p.net != nil {
			wan.DownMbps = mbps(p.net.RxBps)
			wan.UpMbps = mbps(p.net.TxBps)
		}
		if p.latencyMs != nil {
			wan.LatencyMs = *p.latencyMs
		}
		if p.lossPct != nil {
			wan.LossPct = *p.lossPct
		}
		// Conexión WAN real (issue #276): proto/IP/gateway/DNS desde ubus.
		if wi := p.wanInfo; wi.IP != "" || wi.Proto != "" {
			wan.Proto = wi.Proto
			if wi.IP != "" {
				wan.PublicIP = wi.IP
			}
			wan.Gateway = wi.Gateway
			wan.DNS = wi.DNS
		}
	}
	// Resumen del día desde la BD (issue #169): pico de hoy, media y total
	// 24h solo los poblaba el modo demo; aquí se calculan con la tabla raw.
	if gw != nil {
		ds := l.wanDayStats(gw.ID)
		wan.PeakTodayMbps = ds.peakMbps
		wan.PeakTodayTime = ds.peakTime
		wan.AvgDownMbps = ds.avgMbps
		wan.Total24h = ds.totalStr
	}
	return wan
}

// GetOverview: single-flight (los llamantes concurrentes comparten sondeo).
// Si buildOverview paniquea, el líder lo transforma en error y todos los
// seguidores reciben ese error en vez de bloquear para siempre.
func (l *Live) GetOverview(ctx context.Context) (ov *Overview, err error) {
	l.sfMu.Lock()
	if c := l.sfCall; c != nil {
		l.sfMu.Unlock()
		<-c.done
		return c.ov, c.err
	}
	c := &sfCall{done: make(chan struct{})}
	l.sfCall = c
	l.sfMu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic en buildOverview: %v\n%s", r, debug.Stack())
			ov = nil
		}
		c.ov, c.err = ov, err
		close(c.done)
		l.sfMu.Lock()
		l.sfCall = nil
		l.sfMu.Unlock()
	}()
	return l.buildOverview(ctx)
}

func (l *Live) buildOverview(ctx context.Context) (*Overview, error) {
	polled := l.pollAll(ctx)
	l.mu.Lock()
	routers := append([]RouterConfig(nil), l.routers...)
	gw := l.gatewayCfg
	l.mu.Unlock()

	routerList := make([]Router, 0, len(routers))
	for _, cfg := range routers {
		p := polled[cfg.ID]
		if p == nil {
			l.mu.Lock()
			prev := l.lastGood[cfg.ID]
			fails := l.failCount[cfg.ID]
			l.mu.Unlock()
			if prev != nil && fails < 2 {
				routerList = append(routerList, *prev)
			} else {
				routerList = append(routerList, l.offlineRouter(cfg))
			}
			continue
		}
		router := l.buildRouter(p, l.metricsHistory(cfg.ID, "24h"))
		l.mu.Lock()
		l.lastGood[cfg.ID] = &router
		// Alerta de temperatura UNA vez por proceso (flag, como el JS)
		if router.Temp != nil && *router.Temp > 65 && l.lastStatus[cfg.ID+":temp"] != "warn" {
			l.lastStatus[cfg.ID+":temp"] = "warn"
			l.engine.Emit(AlertEvent{
				ID:       fmt.Sprintf("alert-temp-%s-%d", cfg.ID, time.Now().UnixMilli()),
				Category: alerts.CatRouter, Urgent: true,
				Severity: "warn", Title: "Temperatura alta en " + router.Name,
				Description: fmt.Sprintf("%d °C, por encima del umbral (65 °C)", *router.Temp),
				Time:        "ahora mismo", RouterID: cfg.ID,
			})
		}
		l.mu.Unlock()
		routerList = append(routerList, router)
	}
	devices := l.buildDevices(polled)
	devices, distNodes := inferTopology(polled, devices)
	// Capa 2 manual (issue #142): overrides de topología tras el autodiscover.
	// Sin BD (tests/demo) → no-op.
	if l.db != nil {
		devices, distNodes = applyTopologyOverrides(devices, distNodes, loadTopologyOverrides(l.db))
	}
	// Aviso de señal débil (< -70 dBm): una alerta por dispositivo y día
	for _, d := range devices {
		if d.Online && d.SignalDbm != nil && *d.SignalDbm < -70 {
			l.mu.Lock()
			last := l.weakAlerted[d.MAC]
			if time.Now().UnixMilli()-last > 24*3600e3 {
				l.weakAlerted[d.MAC] = time.Now().UnixMilli()
				l.engine.Emit(AlertEvent{
					ID:       fmt.Sprintf("alert-weak-%s-%d", d.MAC, time.Now().UnixMilli()),
					Category: alerts.CatSignal, Urgent: false,
					Severity: "warn", Title: "Señal débil en " + d.Name,
					Description: fmt.Sprintf("%d dBm en %s — revisa cobertura o acerca un AP", *d.SignalDbm, d.RouterID),
					Time:        "ahora mismo", RouterID: d.RouterID,
				})
			}
			l.mu.Unlock()
		}
	}
	l.trackUnknownDevices(devices)
	l.trackDevicePresence(devices, time.Now().UnixMilli())
	// Clientes reales por router (atribución wireless/FDB, no leases)
	for i := range routerList {
		n := 0
		for _, d := range devices {
			if d.RouterID == routerList[i].ID && d.Online {
				n++
			}
		}
		routerList[i].Clients = n
	}
	adguard := l.pollAdGuard(ctx)
	wireguard := l.pollWireGuard(devices)
	gwID := ""
	if gw != nil {
		gwID = gw.ID
	}
	wan := l.defaultWan(gw)
	for _, r := range routerList {
		if r.ID == gwID && r.Status == "offline" {
			wan.PublicIP = "—"
		}
	}
	trafficOf := func(rang string) []TrafficPoint {
		h := l.metricsHistory(gwID, rang)
		out := make([]TrafficPoint, 0, len(h))
		for _, p := range h {
			out = append(out, TrafficPoint{T: p.t, Down: p.down, Up: p.up})
		}
		return out
	}
	ag := AdGuardStats{Status: "inactive", TopBlocked: []TopBlocked{}}
	if adguard != nil {
		ag = *adguard
	}
	wgStats := WireGuardStats{Interface: l.cfg.WGInterface, Subnet: "", Status: "inactive", Peers: []WGPeer{}}
	if wireguard != nil {
		wgStats = *wireguard
	}
	online := 0
	for _, d := range devices {
		if d.Online {
			online++
		}
	}
	top := append([]Device(nil), devices...)
	sort.SliceStable(top, func(i, j int) bool { return top[i].TrafficMbps > top[j].TrafficMbps })
	if len(top) > 5 {
		top = top[:5]
	}
	// El motor de alertas es el dueño de la lista y del read-state
	// (SPEC-ALERTAS §3-4): UnreadAlerts = no leídas que pasaron config.
	alertsCopy := l.engine.List()
	unread := l.engine.UnreadCount()
	return &Overview{
		Health:  computeHealth(routerList, adguard),
		WAN:     wan,
		Traffic: TrafficByRange{H1: trafficOf("1h"), H24: trafficOf("24h"), D7: trafficOf("7d"), D30: trafficOf("30d")},
		Adguard: ag, Wireguard: wgStats, Routers: routerList,
		DeviceTotals: DeviceTotals{
			Total: len(devices), Online: online, KnownOffline: len(devices) - online, NewToday: 0,
		},
		TopDevices: top, Alerts: alertsCopy, UnreadAlerts: unread,
		DistributionNodes: distNodes,
		Topology:          BuildTopoSemantics(routerList, devices, wgStats, distNodes), // SPEC-65 D65-3
		Devices:           devices,
		Dawn:              &DawnOverview{Available: l.dawnAvailableCached()},
		VM:                ViewModelVersion, // SPEC-65 D65-4
		Ts:                time.Now().Unix(),
	}, nil
}

// dawnAvailableCached devuelve si hay DAWN en la red (cacheado). Lanza un
// refresco asíncrono (1 SSH al gateway, TTL 30s) cuando el cache está stale,
// sin bloquear el overview: la primera llamada devuelve false y el flag llega
// en el overview siguiente una vez refrescado.
func (l *Live) dawnAvailableCached() bool {
	l.mu.Lock()
	avail := l.dawnAvailable
	checked := l.dawnCheckedAt
	checking := l.dawnChecking
	gw := l.gatewayCfg
	l.mu.Unlock()
	if gw == nil {
		return false
	}
	if checking || time.Since(checked) < 30*time.Second {
		return avail
	}
	l.mu.Lock()
	l.dawnChecking = true
	host := gw.Host
	l.mu.Unlock()
	go func() {
		out, err := l.pool.Run(host, "ubus call dawn get_network", 4*time.Second)
		l.mu.Lock()
		l.dawnChecking = false
		l.dawnCheckedAt = time.Now()
		l.dawnAvailable = err == nil && len(out) > 10
		l.mu.Unlock()
	}()
	return avail
}

// GetRouters: tarjetas desde la caché del último tick (index.js:600-610).
func (l *Live) GetRouters(context.Context) []Router {
	l.mu.Lock()
	routers := append([]RouterConfig(nil), l.routers...)
	polled := l.lastPolled
	l.mu.Unlock()
	out := make([]Router, 0, len(routers))
	for _, cfg := range routers {
		p := polled[cfg.ID]
		if p == nil {
			l.mu.Lock()
			prev := l.lastGood[cfg.ID]
			fails := l.failCount[cfg.ID]
			l.mu.Unlock()
			if prev != nil && fails < 2 {
				out = append(out, *prev)
			} else {
				out = append(out, l.offlineRouter(cfg))
			}
			continue
		}
		out = append(out, l.buildRouter(p, l.metricsHistory(cfg.ID, "24h")))
	}
	return out
}

// liveExtras es el objeto extras del detalle live (index.js:677-700).
type liveExtras struct {
	MAC                 string        `json:"mac"`
	Firmware            string        `json:"firmware"`
	FirmwareUpdated     bool          `json:"firmwareUpdated"`
	LastReboot          string        `json:"lastReboot"`
	Soc                 string        `json:"soc"`
	Flash               string        `json:"flash"`
	RamMb               int           `json:"ramMb"`
	BandSplit           demoBandSplit `json:"bandSplit"`
	TrafficNow          float64       `json:"trafficNow"`
	GatewayLatencySpark []float64     `json:"gatewayLatencySpark"`
	BackhaulSignal      []float64     `json:"backhaulSignal"`
	Radios              []Radio       `json:"radios"`
	Ports               []EthPort     `json:"ports"`
	EthPorts            []EthPort     `json:"ethPorts"`
}

// GetRouterDetail (index.js:612-715). Id desconocido → (nil, nil).
func (l *Live) GetRouterDetail(ctx context.Context, id string) (*RouterDetail, error) {
	l.mu.Lock()
	var cfg *RouterConfig
	for i := range l.routers {
		if l.routers[i].ID == id {
			cfg = &l.routers[i]
			break
		}
	}
	p := l.lastPolled[id]
	gw := l.gatewayCfg
	polledAll := l.lastPolled
	l.mu.Unlock()
	if cfg == nil {
		return nil, nil
	}
	var router Router
	if p != nil {
		router = l.buildRouter(p, l.metricsHistory(id, "24h"))
	} else {
		router = l.offlineRouter(*cfg)
	}
	// Mapa global MAC → lease + MAC de bridge → nombre de router
	leaseMap := map[string]DhcpLease{}
	routerByMac := map[string]string{}
	// Alias manuales (Settings > known_macs): fallback de nombre para MACs
	// sin lease (estáticas, equipos sin DHCP), mismo criterio que la lista
	// de dispositivos (#291: match MAC→nombre también en bocas de switch).
	aliasByMac := map[string]string{}
	if l.db != nil {
		if rows, err := l.db.Query("SELECT mac, name FROM known_macs"); err == nil {
			for rows.Next() {
				var mac, name string
				if rows.Scan(&mac, &name) == nil && name != "" {
					aliasByMac[mac] = name
				}
			}
			rows.Close()
		}
	}
	for _, polled := range polledAll {
		for _, le := range polled.leases {
			if le.MAC != "" {
				leaseMap[le.MAC] = le
			}
		}
		if polled.brMac != "" {
			name := polled.cfg.Name
			if name == "" {
				name = polled.cfg.Host
			}
			routerByMac[polled.brMac] = name
		}
	}
	// Boca → MACs aprendidas (vecino inmediato)
	portMacs := map[string][]string{}
	if p != nil {
		for mac, portName := range p.fdb {
			portMacs[portName] = append(portMacs[portName], mac)
		}
	}
	ports := []EthPort{}
	if p != nil {
		ports = p.ports
	}
	enriched := make([]EthPort, 0, len(ports))
	for _, port := range ports {
		if !port.Up {
			enriched = append(enriched, port)
			continue
		}
		all := portMacs[port.ID]
		// 1) ¿Otro router al otro lado? (uplink router↔router)
		neighbor := ""
		for _, mac := range all {
			if _, ok := routerByMac[mac]; ok {
				neighbor = mac
				break
			}
		}
		if neighbor != "" {
			port.ConnectedTo = routerByMac[neighbor]
			port.Detail = "enlace entre routers"
			// El vecino además se anuncia por LLDP → el frontend puede
			// mostrar el sufijo "· LLDP" en la etiqueta del uplink (C2).
			if nb := lldpNeighborOnPort(p.lldp, port.ID); nb != nil {
				port.Detail = "enlace entre routers · LLDP"
			}
			enriched = append(enriched, port)
			continue
		}
		// 2) Un solo dispositivo final
		if len(all) == 1 {
			mac := all[0]
			lease, ok := leaseMap[mac]
			switch {
			case ok && lease.Hostname != "":
				port.ConnectedTo = lease.Hostname
			case ok && lease.IP != "":
				port.ConnectedTo = lease.IP
			case aliasByMac[mac] != "":
				port.ConnectedTo = aliasByMac[mac]
			default:
				port.ConnectedTo = mac
			}
			port.DeviceMac = mac
			if ok && lease.IP != "" {
				port.Detail = lease.IP + " · full duplex"
			}
			enriched = append(enriched, port)
			continue
		}
		// 3) Varios: el vecino es un switch/hub/hipervisor
		if len(all) > 1 {
			// Muchas MACs detrás de una boca = agregación (hipervisor Proxmox
			// con CTs, switch tonto): nombrar UN CT al azar engaña (la MAC es
			// virtual, no el equipo enchufado). La label curada del puerto ya
			// identifica el físico; aquí se cuenta lo que hay detrás (#291).
			if len(all) > 3 {
				port.ConnectedTo = fmt.Sprintf("%d dispositivos", len(all))
				port.Detail = "agregación · ¿hipervisor o switch?"
				enriched = append(enriched, port)
				continue
			}
			// Si se anuncia por LLDP, esa identificación (chassis + mgmt-ip)
			// es mejor pista que el hostname DHCP.
			if nb := lldpNeighborOnPort(p.lldp, port.ID); nb != nil && nb.displayName() != "" {
				port.ConnectedTo = nb.displayName()
				if nb.Mgmt != "" {
					port.Detail = nb.Mgmt + " · LLDP"
				} else {
					port.Detail = "LLDP"
				}
				enriched = append(enriched, port)
				continue
			}
			infraMac := ""
			for _, mac := range all {
				if le, ok := leaseMap[mac]; ok && le.Hostname != "" {
					infraMac = mac
					break
				}
			}
			if infraMac == "" {
				// Sin hostname DHCP: primer alias manual como pista (#291).
				for _, mac := range all {
					if aliasByMac[mac] != "" {
						infraMac = mac
						break
					}
				}
			}
			if infraMac != "" {
				if lease, ok := leaseMap[infraMac]; ok && lease.Hostname != "" {
					port.ConnectedTo = lease.Hostname
					port.DeviceMac = infraMac
					if lease.IP != "" {
						port.Detail = lease.IP
					}
				} else if alias := aliasByMac[infraMac]; alias != "" {
					port.ConnectedTo = alias
					port.DeviceMac = infraMac
				}
			} else {
				port.ConnectedTo = "Switch"
			}
			enriched = append(enriched, port)
			continue
		}
		enriched = append(enriched, port)
	}
	radios := []Radio{}
	if p != nil && p.radios != nil {
		radios = p.radios
	}
	seriesOf := func(rang string) []PerfPoint {
		h := l.metricsHistory(id, rang)
		out := make([]PerfPoint, 0, len(h))
		for _, hp := range h {
			out = append(out, PerfPoint{T: hp.t, CPU: float64(hp.cpu), RAM: float64(hp.ram), Temp: float64(hp.temp)})
		}
		return out
	}
	clients := []Device{}
	detDevices, _ := inferTopology(polledAll, l.buildDevices(polledAll))
	for _, d := range detDevices {
		if d.RouterID == id {
			clients = append(clients, d)
		}
	}
	extras := liveExtras{
		MAC: "—", Firmware: "—", FirmwareUpdated: true, LastReboot: "—",
		Soc: "—", Flash: "—", RamMb: 0,
		BandSplit:           demoBandSplit{},
		GatewayLatencySpark: []float64{},
		BackhaulSignal:      []float64{},
		Radios:              radios,
		Ports:               enriched,
		EthPorts:            enriched,
	}
	if p != nil {
		if p.brMac != "" {
			extras.MAC = p.brMac
		}
		if p.board != nil {
			extras.Firmware = p.board.Release.Description
			extras.Soc = p.board.System
			if extras.Soc == "" {
				extras.Soc = "—"
			}
		}
		if p.uptimeSec > 0 {
			rb := time.Now().Add(-time.Duration(p.uptimeSec) * time.Second)
			extras.LastReboot = fmt.Sprintf("%02d/%02d/%d, %02d:%02d", rb.Day(), int(rb.Month()), rb.Year(), rb.Hour(), rb.Minute())
		}
		if p.sysInfo != nil && p.sysInfo.Memory.Total > 0 {
			extras.RamMb = int(math.Round(p.sysInfo.Memory.Total / 1e6))
		}
		if p.net != nil {
			extras.TrafficNow = mbps(p.net.RxBps)
		}
	}
	detail := &RouterDetail{
		Router: router, Ports: enriched, Radios: radios, Backhaul: nil,
		Series:  PerfSeries{H1: seriesOf("1h"), H24: seriesOf("24h"), D7: seriesOf("7d")},
		Clients: clients, Extras: extras,
	}
	if gw != nil && id == gw.ID {
		detail.Adguard = l.pollAdGuard(ctx)
		detail.Wireguard = l.pollWireGuard(clients)
	} else {
		// Backhaul real del AP: boca que enlaza con otro router + latencia
		var uplink *EthPort
		for i := range enriched {
			if enriched[i].Detail == "enlace entre routers" {
				uplink = &enriched[i]
				break
			}
		}
		if uplink == nil {
			for i := range enriched {
				if enriched[i].Up {
					uplink = &enriched[i]
					break
				}
			}
		}
		speed := "—"
		if uplink != nil && uplink.Speed != "" {
			speed = uplink.Speed
		}
		latency := 0.0
		if p != nil && p.latencyMs != nil {
			latency = *p.latencyMs
		}
		detail.Backhaul = demoBackhaul{
			Kind: "cable", Headline: "Cable · " + speed + " · full duplex", LatencyMs: latency,
		}
	}
	return detail, nil
}

// GetDevices: buildDevices sobre el último sondeo (+ inferencia FDB de
// topología: Port/AttachTo, como en buildOverview).
func (l *Live) GetDevices(context.Context) []Device {
	l.mu.Lock()
	polled := l.lastPolled
	l.mu.Unlock()
	devices, _ := inferTopology(polled, l.buildDevices(polled))
	return devices
}

// GetAlerts: copia de las alertas en memoria.
func (l *Live) GetAlerts(context.Context) []AlertEvent {
	return l.engine.List()
}

// AlertsEngine: motor de alertas del adapter (SPEC-ALERTAS §3).
func (l *Live) AlertsEngine() *alerts.Engine { return l.engine }

// GetMetricsRows: filas para el poller (index.js:725-739).
func (l *Live) GetMetricsRows(context.Context) []MetricsRow {
	l.mu.Lock()
	polled := l.lastPolled
	l.mu.Unlock()
	rows := []MetricsRow{}
	for id, p := range polled {
		row := MetricsRow{
			RouterID:  id,
			CPU:       fptr(float64(p.cpu)),
			RAM:       fptr(float64(p.ram)),
			Temp:      fptr(float64(p.temp)),
			LatencyMs: p.latencyMs,
		}
		if p.net != nil {
			row.RxBps = p.net.RxBps
			row.TxBps = p.net.TxBps
		}
		rows = append(rows, row)
	}
	return rows
}

// GetAdguardClients: solo GL tiene /control/clients (nil si no configurado).
func (l *Live) GetAdguardClients(ctx context.Context) ([]AdguardClient, error) {
	_, gl := l.getAdguardClient()
	if gl == nil {
		return nil, nil
	}
	return gl.QueryClients(ctx)
}

// dawnAPField es el conjunto de keys escalares que describen un AP en la
// salida de `ubus call dawn get_network`. El resto de keys de un BSSID cuyo
// valor es un objeto (con "signal") son clientes del hearing map.
var dawnAPField = map[string]bool{
	"channel": true, "freq": true, "channel_utilization": true,
	"num_sta": true, "ht_support": true, "vht_support": true,
	"local": true, "iface": true, "hostname": true,
	"neighbor_report": true, "op_class": true,
}

// GetDawn: red DAWN (roaming/band-steering). nil si ningún router responde.
//
// La salida de `ubus call dawn get_network` mezcla, por cada BSSID, los campos
// del AP (escalares) con los clientes anidados (MAC → {signal, ht, vht, ...}).
// DAWN distribuye el hearing map entre nodos, así que la respuesta del primer
// router que contesta ya contiene toda la malla (APs + clientes vistos por
// cada uno). Por eso solo usamos firstData.
func (l *Live) GetDawn(context.Context) (*Dawn, error) {
	l.mu.Lock()
	routers := append([]RouterConfig(nil), l.routers...)
	l.mu.Unlock()

	var firstData map[string]map[string]json.RawMessage
	mesh := []DawnMesh{}
	for _, cfg := range routers {
		name := cfg.Name
		if name == "" {
			name = cfg.ID
		}
		// Los routers agent-only (switches sin SSH, p.ej. switch16) no tienen
		// DAWN: saltar el SSH evita un timeout de probeTimeout (4s) por cada
		// llamada a GetDawn. Aparecen en el mesh como Dawn=false para que la
		// UI siga mostrándolos en la tabla.
		if cfg.AgentOnly {
			mesh = append(mesh, DawnMesh{RouterID: cfg.ID, Name: name, Dawn: false, ApsSeen: 0})
			continue
		}
		out, err := l.pool.Run(cfg.Host, "ubus call dawn get_network", 0)
		if err != nil {
			mesh = append(mesh, DawnMesh{RouterID: cfg.ID, Name: name, Dawn: false, ApsSeen: 0})
			continue
		}
		var data map[string]map[string]json.RawMessage
		if json.Unmarshal([]byte(out), &data) != nil {
			mesh = append(mesh, DawnMesh{RouterID: cfg.ID, Name: name, Dawn: false, ApsSeen: 0})
			continue
		}
		if firstData == nil {
			firstData = data
		}
		seen := 0
		for _, bssids := range data {
			seen += len(bssids)
		}
		mesh = append(mesh, DawnMesh{RouterID: cfg.ID, Name: name, Dawn: true, ApsSeen: seen})
	}
	if firstData == nil {
		return nil, nil
	}
	aps := dawnAPsFromNetwork(firstData)
	return &Dawn{APs: aps, Mesh: mesh}, nil
}

// dawnAPsFromNetwork parsea la salida de `ubus call dawn get_network` ya
// deserializada a json.RawMessage (SSID → BSSID → campos mixtos). Devuelve los
// APs con sus clientes del hearing map. Función pura para testear sin SSH.
func dawnAPsFromNetwork(firstData map[string]map[string]json.RawMessage) []DawnAP {
	aps := []DawnAP{}
	for ssid, bssids := range firstData {
		for bssid, raw := range bssids {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil {
				continue
			}
			// "channel" presente y no cero → es un AP (no un cliente suelto).
			chRaw, ok := fields["channel"]
			if !ok {
				continue
			}
			var channel int
			json.Unmarshal(chRaw, &channel)
			if channel == 0 {
				continue
			}
			ap := DawnAP{SSID: ssid, BSSID: bssid, Channel: channel, Clients: []DawnClient{}}
			var freq float64
			json.Unmarshal(fields["freq"], &freq)
			json.Unmarshal(fields["channel_utilization"], &ap.UtilizationPct)
			json.Unmarshal(fields["num_sta"], &ap.ClientCount)
			json.Unmarshal(fields["local"], &ap.Local)
			json.Unmarshal(fields["iface"], &ap.Iface)
			json.Unmarshal(fields["hostname"], &ap.Hostname)
			if freq >= 5000 {
				ap.Band = "5 GHz"
			} else {
				ap.Band = "2.4 GHz"
			}
			// Clientes anidados: cualquier key MAC cuyo valor sea un objeto
			// con "signal" negativa (en -dBm).
			for mac, valRaw := range fields {
				if dawnAPField[mac] {
					continue
				}
				s := strings.TrimSpace(string(valRaw))
				if !strings.HasPrefix(s, "{") {
					continue
				}
				var c struct {
					Signal int  `json:"signal"`
					HT     bool `json:"ht"`
					VHT    bool `json:"vht"`
				}
				if json.Unmarshal(valRaw, &c) == nil && c.Signal < 0 {
					ap.Clients = append(ap.Clients, DawnClient{
						MAC: strings.ToUpper(mac), Signal: c.Signal, HT: c.HT, VHT: c.VHT,
					})
				}
			}
			aps = append(aps, ap)
		}
	}
	sort.Slice(aps, func(i, j int) bool {
		if aps[i].Hostname != aps[j].Hostname {
			return aps[i].Hostname < aps[j].Hostname
		}
		return aps[i].Band < aps[j].Band
	})
	return aps
}

// ---------------------------------------------------------------------------
// 802.11r (Fast BSS Transition) — Fase 14.3
// ---------------------------------------------------------------------------

// parseUciWireless parsea la salida de `uci show wireless` (líneas
// `wireless.SECTION.FIELD='VALUE'` y `wireless.SECTION=TYPE`) y devuelve los
// wifi-iface con sus campos relevantes para 802.11r. Ignora wifi-device y
// cualquier otra sección que no sea wifi-iface (pero usa wifi-device para
// mapear device → {channel, band}). Función pura — fácil de testear.
//
// Formato uci: las comillas simples envuelven los valores; los campos lista
// (como rrm_nr_list) se emiten como varias líneas con el mismo nombre — aquí
// nos quedamos con el último valor escalar (no nos interesan las listas).
func parseUciWireless(out string) []Dot11rIface {
	type sec struct {
		typ    string
		fields map[string]string
	}
	sections := map[string]*sec{}
	order := []string{}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "wireless.") {
			continue
		}
		rest := line[len("wireless."):]
		// Caso 1: wireless.SECTION=TYPE (declaración de sección, sin punto en SECTION).
		if eq := strings.IndexByte(rest, '='); eq > 0 {
			head := rest[:eq]
			if !strings.Contains(head, ".") {
				if _, ok := sections[head]; !ok {
					sections[head] = &sec{fields: map[string]string{}}
					order = append(order, head)
				}
				sections[head].typ = unquoteUci(rest[eq+1:])
				continue
			}
		}
		// Caso 2: wireless.SECTION.FIELD='VALUE'
		parts := strings.SplitN(rest, ".", 2)
		if len(parts) != 2 {
			continue
		}
		section, field := parts[0], parts[1]
		if _, ok := sections[section]; !ok {
			sections[section] = &sec{fields: map[string]string{}}
			order = append(order, section)
		}
		if eq := strings.IndexByte(field, '='); eq > 0 {
			fname := field[:eq]
			fval := field[eq+1:]
			sections[section].fields[fname] = unquoteUci(fval)
		}
	}

	// device → {channel, band} desde las secciones wifi-device.
	type devInfo struct {
		channel int
		band    string
	}
	devMap := map[string]devInfo{}
	for _, name := range order {
		s := sections[name]
		if s.typ != "wifi-device" {
			continue
		}
		ch, _ := strconv.Atoi(s.fields["channel"])
		band := ""
		switch s.fields["band"] {
		case "2g":
			band = "2.4 GHz"
		case "5g":
			band = "5 GHz"
		case "6g":
			band = "6 GHz"
		case "60g":
			band = "60 GHz"
		}
		devMap[name] = devInfo{channel: ch, band: band}
	}

	ifaces := []Dot11rIface{}
	for _, name := range order {
		s := sections[name]
		if s.typ != "wifi-iface" {
			continue
		}
		f := s.fields
		dev := f["device"]
		di := devMap[dev]
		ifaces = append(ifaces, Dot11rIface{
			Section:            name,
			Device:             dev,
			Ifname:             f["ifname"],
			SSID:               f["ssid"],
			MAC:                f["macaddr"],
			Channel:            di.channel,
			Band:               di.band,
			Encryption:         f["encryption"],
			Dot11REnabled:      f["ieee80211r"] == "1",
			MobilityDomain:     f["mobility_domain"],
			FTOverDS:           f["ft_over_ds"] == "1",
			FTPSKGenerateLocal: f["ft_psk_generate_local"] == "1",
			PMKR1Push:          f["pmk_r1_push"] == "1",
			NASID:              f["nasid"],
			Dot11KEnabled:      f["ieee80211k"] == "1",
			Dot11VEnabled:      f["ieee80211v"] == "1",
			BSSTransition:      f["bss_transition"] == "1",
			MFP:                f["ieee80211w"] == "1",
		})
	}
	return ifaces
}

// unquoteUci quita las comillas simples que uci envuelve a los valores y
// desescapa \' → '. `uci show` SIEMPRE envuelve en comillas simples.
func unquoteUci(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], `\'`, `'`)
	}
	return s
}

// GetDot11r: estado 802.11r (Fast BSS Transition) por router y SSID. Recorre
// los routers (saltando agent-only, que son switches sin wifi) y hace SSH
// `uci show wireless` a cada uno. Devuelve (nil, nil) si ningún router con
// wifi tiene 802.11r habilitado → el handler responde 503.
func (l *Live) GetDot11r(ctx context.Context) (*Dot11rOverview, error) {
	l.mu.Lock()
	routers := append([]RouterConfig(nil), l.routers...)
	l.mu.Unlock()

	out := Dot11rOverview{Routers: []Dot11rRouter{}, SSIDs: []Dot11rSSID{}}
	type ifaceRef struct {
		routerID string
		iface    Dot11rIface
	}
	ssidIfaces := map[string][]ifaceRef{}

	for _, cfg := range routers {
		name := cfg.Name
		if name == "" {
			name = cfg.ID
		}
		r := Dot11rRouter{RouterID: cfg.ID, Name: name, Ifaces: []Dot11rIface{}}
		// Agent-only (switches sin SSH ni wifi) se listan como Available=false.
		if cfg.AgentOnly {
			out.Routers = append(out.Routers, r)
			continue
		}
		uciOut, err := l.pool.Run(cfg.Host, "uci show wireless", 0)
		if err != nil {
			out.Routers = append(out.Routers, r)
			continue
		}
		ifaces := parseUciWireless(uciOut)
		r.Available = true
		r.Ifaces = ifaces
		out.Routers = append(out.Routers, r)
		for _, ifc := range ifaces {
			if ifc.SSID == "" {
				continue
			}
			ssidIfaces[ifc.SSID] = append(ssidIfaces[ifc.SSID], ifaceRef{routerID: cfg.ID, iface: ifc})
		}
	}

	// Agregar por SSID.
	ssids := make([]Dot11rSSID, 0, len(ssidIfaces))
	for ssid, refs := range ssidIfaces {
		enabled := 0
		mobilityDomain := ""
		ftOverDS := false
		ftPSK := false
		routerSet := map[string]bool{}
		for _, ref := range refs {
			if ref.iface.Dot11REnabled {
				enabled++
				if mobilityDomain == "" {
					mobilityDomain = ref.iface.MobilityDomain
					ftOverDS = ref.iface.FTOverDS
					ftPSK = ref.iface.FTPSKGenerateLocal
				}
			}
			routerSet[ref.routerID] = true
		}
		routerIDs := make([]string, 0, len(routerSet))
		for id := range routerSet {
			routerIDs = append(routerIDs, id)
		}
		sort.Strings(routerIDs)
		ssids = append(ssids, Dot11rSSID{
			SSID:               ssid,
			EnabledEverywhere:  enabled > 0 && enabled == len(refs),
			EnabledCount:       enabled,
			TotalCount:         len(refs),
			MobilityDomain:     mobilityDomain,
			FTOverDS:           ftOverDS,
			FTPSKGenerateLocal: ftPSK,
			IfaceCount:         len(refs),
			RouterIDs:          routerIDs,
		})
	}
	sort.Slice(ssids, func(i, j int) bool { return ssids[i].SSID < ssids[j].SSID })
	out.SSIDs = ssids

	for _, s := range ssids {
		if s.EnabledCount > 0 {
			out.Available = true
			break
		}
	}
	if !out.Available {
		return nil, nil
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// WiFi Survey (canal utilization) — Fase 14.4
// ---------------------------------------------------------------------------

// freqToChannel mapea frecuencia MHz → número de canal IEEE 802.11.
// 2.4 GHz: 2412-2472 → 1-13; 2484 → 14. 5 GHz: 5000+5*N → N (5180=36, ...).
// 60 GHz: 56160+2160*N → N+1.
func freqToChannel(freq int) int {
	switch {
	case freq >= 2412 && freq <= 2472:
		return (freq-2412)/5 + 1
	case freq == 2484:
		return 14
	case freq >= 5160 && freq <= 5885:
		// Canales UNII: 5160=ch32 ... 5885=ch177. Fórmula general (freq-5000)/5.
		return (freq - 5000) / 5
	case freq >= 5940 && freq <= 7115:
		// 6 GHz (Wi-Fi 6E): 5950=ch1 ... 7115=ch233. Mismo (freq-5000)/5, con offset.
		return (freq-5950)/5 + 1
	case freq == 56160:
		return 1
	case freq >= 56160:
		return (freq-56160)/2160 + 1
	}
	return 0
}

// freqToBand devuelve "2.4 GHz"|"5 GHz"|"6 GHz"|"60 GHz"|"" según la frecuencia.
func freqToBand(freq int) string {
	switch {
	case freq >= 2412 && freq <= 2484:
		return "2.4 GHz"
	case freq >= 5160 && freq <= 5885:
		return "5 GHz"
	case freq >= 5945 && freq <= 7115:
		return "6 GHz"
	case freq >= 56160:
		return "60 GHz"
	}
	return ""
}

// parseIwSurvey parsea la salida de `iw dev wlanX survey dump` y devuelve
// un map device → lista de SurveyChannel (uno por frecuencia). Función pura
// para testear con fixtures reales sin SSH.
//
// Formato (ejemplo Flint2):
//
//	Survey data from wlan0
//		frequency:			2412 MHz [in use]
//		noise:				-90 dBm
//		channel active time:		3632796925 ms
//		channel busy time:		878259766 ms
//		channel receive time:		440957725 ms
//		channel transmit time:		340995043 ms
//	Survey data from wlan0
//		frequency:			2417 MHz
//		noise:				-92 dBm
//		channel active time:		72 ms
//		channel busy time:		20 ms
//		...
//
// Cada bloque empieza con "Survey data from <dev>". Los valores están
// tabulados y separados por ":". El "[in use]" marca el canal operativo.
func parseIwSurvey(out string) map[string][]SurveyChannel {
	type rawChannel struct {
		freq     int
		inUse    bool
		noise    int
		activeMs float64
		busyMs   float64
		rxMs     float64
		txMs     float64
	}
	result := map[string][]SurveyChannel{}
	var currentDev string
	var current *rawChannel

	flush := func() {
		if currentDev == "" || current == nil {
			return
		}
		ch := SurveyChannel{
			Freq:     current.freq,
			Channel:  freqToChannel(current.freq),
			InUse:    current.inUse,
			NoiseDbm: current.noise,
		}
		if current.activeMs > 0 {
			ch.BusyPct = current.busyMs / current.activeMs * 100
			ch.RxPct = current.rxMs / current.activeMs * 100
			ch.TxPct = current.txMs / current.activeMs * 100
		}
		result[currentDev] = append(result[currentDev], ch)
		current = nil
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Survey data from ") {
			flush()
			currentDev = strings.TrimPrefix(trimmed, "Survey data from ")
			continue
		}
		if currentDev == "" {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		// Solo inicializamos el canal si la línea tiene un campo válido.
		// Evita crear un canal vacío al final de cada bloque (línea "" final).
		if current == nil {
			current = &rawChannel{}
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "frequency":
			fmt.Sscanf(val, "%d MHz", &current.freq)
			if strings.Contains(val, "[in use]") {
				current.inUse = true
			}
		case "noise":
			fmt.Sscanf(val, "%d dBm", &current.noise)
		case "channel active time":
			fmt.Sscanf(val, "%f ms", &current.activeMs)
		case "channel busy time":
			fmt.Sscanf(val, "%f ms", &current.busyMs)
		case "channel receive time":
			fmt.Sscanf(val, "%f ms", &current.rxMs)
		case "channel transmit time":
			fmt.Sscanf(val, "%f ms", &current.txMs)
		}
	}
	flush()
	return result
}

// GetSurvey: utilización por canal wifi (iw survey dump) por router y radio.
// Recorre los routers (saltando agent-only, que no tienen wifi) y hace SSH
// `iw dev` para listar interfaces, luego `iw dev wlanX survey dump` por cada
// una. Devuelve (nil, nil) si ningún router responde → el handler 503.
func (l *Live) GetSurvey(ctx context.Context) (*SurveyOverview, error) {
	l.mu.Lock()
	routers := append([]RouterConfig(nil), l.routers...)
	l.mu.Unlock()

	out := SurveyOverview{Routers: []SurveyRouter{}}
	any := false
	for _, cfg := range routers {
		name := cfg.Name
		if name == "" {
			name = cfg.ID
		}
		r := SurveyRouter{RouterID: cfg.ID, Name: name, Radios: []SurveyRadio{}}
		if cfg.AgentOnly {
			out.Routers = append(out.Routers, r)
			continue
		}
		// Lista de interfaces wifi (wlanX).
		devsOut, err := l.pool.Run(cfg.Host, "iw dev 2>/dev/null | awk '/Interface/ {print $2}'", 0)
		if err != nil {
			out.Routers = append(out.Routers, r)
			continue
		}
		devs := []string{}
		for _, line := range strings.Split(devsOut, "\n") {
			d := strings.TrimSpace(line)
			// iw dev solo lista interfaces wifi, pero el nombre puede ser
			// wlan0/wlan1 (driver estándar) o phy0-ap0/phy1-ap0 (mt76,
			// Xiaomi AX6). Aceptamos cualquier token no vacío que awk haya
			// extraído de la línea "Interface <name>".
			if d != "" {
				devs = append(devs, d)
			}
		}
		if len(devs) == 0 {
			out.Routers = append(out.Routers, r)
			continue
		}
		r.Available = true
		any = true
		for _, dev := range devs {
			surveyOut, err := l.pool.Run(cfg.Host, "iw dev "+dev+" survey dump 2>/dev/null", 0)
			if err != nil {
				continue
			}
			channels := parseIwSurvey(surveyOut)[dev]
			if len(channels) == 0 {
				continue
			}
			band := ""
			// El canal in use define la banda del radio.
			for _, c := range channels {
				if c.InUse {
					band = freqToBand(c.Freq)
					break
				}
			}
			if band == "" {
				band = freqToBand(channels[0].Freq)
			}
			r.Radios = append(r.Radios, SurveyRadio{Device: dev, Band: band, Channels: channels})
		}
		out.Routers = append(out.Routers, r)
	}
	if !any {
		return nil, nil
	}
	out.Available = true
	return &out, nil
}
