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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// fmtUptime: "<d>d <h>h" (index.js:32-36).
func fmtUptime(sec float64) string {
	d := int(sec) / 86400
	h := (int(sec) % 86400) / 3600
	return fmt.Sprintf("%dd %dh", d, h)
}

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
}

// extrasSnapshot es la caché anti-parpadeo por router.
type extrasSnapshot struct {
	ports    []EthPort
	radios   []Radio
	wireless map[string]WirelessClient
	fdb      map[string]string
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
type lldpCacheEntry struct {
	neighbors []LldpNeighbor
	at        time.Time
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

	lastGood      map[string]*Router
	lastStatus    map[string]string
	boardCache    map[string]*BoardInfo
	layoutCache   map[string][]PortLayout
	extrasCache   map[string]*extrasSnapshot
	lastPolled    map[string]*routerPolled
	failCount     map[string]int
	alerts        []AlertEvent
	wgActive      map[string]bool
	weakAlerted   map[string]int64
	backhaulCache map[string]backhaulCacheEntry
	lldpCache     map[string]lldpCacheEntry

	agStd *AdGuardClient
	agGL  *AdGuardGlinetClient
	agKey string

	sfMu       sync.Mutex
	sfInFlight chan sfResult
}

type sfResult struct {
	ov  *Overview
	err error
}

// NewLive crea el adapter live (db puede ser nil en tests).
func NewLive(cfg *config.Config, d *db.DB, initial []RouterConfig, pool *SSHPool) *Live {
	l := &Live{
		cfg:           cfg,
		db:            d,
		pool:          pool,
		lastGood:      map[string]*Router{},
		lastStatus:    map[string]string{},
		boardCache:    map[string]*BoardInfo{},
		layoutCache:   map[string][]PortLayout{},
		extrasCache:   map[string]*extrasSnapshot{},
		lastPolled:    map[string]*routerPolled{},
		failCount:     map[string]int{},
		wgActive:      map[string]bool{},
		weakAlerted:   map[string]int64{},
		backhaulCache: map[string]backhaulCacheEntry{},
		lldpCache:     map[string]lldpCacheEntry{},
	}
	// Migración una vez (attrib_v2): tabla limpia (index.js:385-394)
	if d != nil {
		var flag string
		if err := d.QueryRow("SELECT value FROM kv WHERE key = 'attrib_v2'").Scan(&flag); err == sql.ErrNoRows {
			_, _ = d.Exec("DELETE FROM device_attrib")
			_, _ = d.Exec("INSERT INTO kv (key, value) VALUES ('attrib_v2', '1')")
			log.Printf("[netpulse] device_attrib limpiada (attrib_v2: solo wireless persiste)")
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
		l.clients[c.ID] = NewOpenWrtClient(c, l.pool, "root", "")
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

// probeLldp: vecinos LLDP del router con caché de 45 s (tier lento, como los
// extras anti-parpadeo). Error o lldpd ausente → nil (sin datos; el FDB solo
// sigue mandando, comportamiento actual intacto).
func (l *Live) probeLldp(ctx context.Context, routerID string, client *OpenWrtClient) []LldpNeighbor {
	l.mu.Lock()
	e, ok := l.lldpCache[routerID]
	l.mu.Unlock()
	if ok && time.Since(e.at) < lldpCacheTTL {
		return e.neighbors
	}
	neighbors, err := client.LldpNeighbors(ctx)
	if err != nil {
		if !errors.Is(err, ErrLldpUnavailable) {
			log.Printf("[netpulse] LLDP %s: %v", routerID, err)
		}
		neighbors = nil
	}
	l.mu.Lock()
	l.lldpCache[routerID] = lldpCacheEntry{neighbors: neighbors, at: time.Now()}
	l.mu.Unlock()
	return neighbors
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
		span, bucket = 30*86400e3, 86400e3
		fmtLabel = func(d time.Time) string { return fmt.Sprintf("%d", d.Day()) }
	default:
		return []histPoint{}
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

// pollRouter sondea un router; error si está inalcanzable.
func (l *Live) pollRouter(ctx context.Context, cfg RouterConfig) (*routerPolled, error) {
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
	wireless := client.GetWirelessClients()
	ports := client.GetEthPorts(layout)
	radios := client.GetRadios()
	fdb := client.GetBridgeFdb()
	brMac := client.GetBridgeMac()
	// Tier lento: backhaul (5 min) y vecinos LLDP (45 s), ambos cacheados y
	// tolerantes a error (router sin wifi/lldpd → campo ausente, sin romper).
	backhaul := l.probeBackhaul(cfg.ID, client)
	lldp := l.probeLldp(ctx, cfg.ID, client)
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
	l.mu.Lock()
	l.extrasCache[cfg.ID] = &extrasSnapshot{ports: portsGood, radios: radiosGood, wireless: wirelessGood, fdb: fdbGood}
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
		uptimeSec: sysInfo.Uptime, net: net, leases: leases,
		wireless: wirelessGood, ports: portsGood, radios: radiosGood,
		fdb: fdbGood, brMac: brMac, latencyMs: latencyMs, lossPct: lossPct,
		backhaul: backhaul, lldp: lldp,
	}, nil
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
	if p.board != nil {
		r.Firmware = p.board.Release.Description
	}
	if p.temp > 65 {
		r.HotMetric = "temp"
	}
	return r
}

// uplinkLldp: vecino LLDP del router que es OTRO router conocido (su
// chassis-MAC es la bridge MAC de otro router de la config) → el uplink
// está identificado por LLDP y la app muestra el sufijo "· LLDP". Si el FDB
// dice dónde se aprendió esa MAC, el anuncio debe llegar por ese puerto
// (uplink); sin FDB, la MAC ya es evidencia suficiente. nil si no hay dato.
func (l *Live) uplinkLldp(p *routerPolled) *LldpInfo {
	if len(p.lldp) == 0 {
		return nil
	}
	l.mu.Lock()
	polled := l.lastPolled
	l.mu.Unlock()
	routerMacs := map[string]bool{}
	for id, other := range polled {
		if id != p.cfg.ID && other.brMac != "" {
			routerMacs[other.brMac] = true
		}
	}
	for i := range p.lldp {
		nb := &p.lldp[i]
		if nb.ChassisMac == "" || !routerMacs[nb.ChassisMac] {
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
	return r
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
			l.lastStatus[res.cfg.ID] = "online"
			continue
		}
		fails := l.failCount[res.cfg.ID] + 1
		l.failCount[res.cfg.ID] = fails
		log.Printf("[netpulse] router %s inalcanzable (%d): %v", res.cfg.ID, fails, res.err)
		// Alerta solo tras 2 fallos seguidos (un fallo suelto no es una caída)
		if fails >= 2 && l.lastStatus[res.cfg.ID] != "offline" {
			name := res.cfg.Name
			if name == "" {
				name = res.cfg.Host
			}
			l.alerts = append([]AlertEvent{{
				ID:       fmt.Sprintf("alert-offline-%s-%d", res.cfg.ID, time.Now().UnixMilli()),
				Severity: "critical",
				Title:    name + " offline",
				Description: fmt.Sprintf("Sin respuesta de %s: %v", res.cfg.Host, res.err),
				Time:     "ahora mismo", Read: false, RouterID: res.cfg.ID,
			}}, l.alerts...)
			if len(l.alerts) > 100 {
				l.alerts = l.alerts[:100]
			}
		}
		if fails >= 2 {
			l.lastStatus[res.cfg.ID] = "offline"
		}
	}
	l.lastPolled = polled
	return polled
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
			l.alerts = append([]AlertEvent{{
				ID: fmt.Sprintf("alert-wg-%s-%d", id, time.Now().UnixMilli()),
				Severity: "info", Title: "Handshake WireGuard",
				Description: name + " conectado",
				Time:        "ahora mismo", Read: false, RouterID: gw.ID,
			}}, l.alerts...)
			if len(l.alerts) > 100 {
				l.alerts = l.alerts[:100]
			}
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
	for _, p := range polled {
		for _, le := range p.leases {
			if le.MAC != "" {
				leasesByMac[le.MAC] = le
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
	// (2) FDB de satélites: pista solo de ESTE tick (no se guarda)
	for routerID, p := range polled {
		if routerID == gwID {
			continue
		}
		for mac := range p.fdb {
			if _, ok := seen[mac]; !ok {
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
	for _, p := range polled {
		if p.brMac != "" {
			routerMacs[p.brMac] = true
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
			ID: strings.ToLower(strings.ReplaceAll(mac, ":", "-")),
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
	}
	return wan
}

// GetOverview: single-flight (los llamantes concurrentes comparten sondeo).
func (l *Live) GetOverview(ctx context.Context) (*Overview, error) {
	l.sfMu.Lock()
	if l.sfInFlight != nil {
		ch := l.sfInFlight
		l.sfMu.Unlock()
		r := <-ch
		return r.ov, r.err
	}
	ch := make(chan sfResult, 1)
	l.sfInFlight = ch
	l.sfMu.Unlock()

	ov, err := l.buildOverview(ctx)
	ch <- sfResult{ov, err}
	close(ch)

	l.sfMu.Lock()
	l.sfInFlight = nil
	l.sfMu.Unlock()
	return ov, err
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
			l.alerts = append([]AlertEvent{{
				ID: fmt.Sprintf("alert-temp-%s-%d", cfg.ID, time.Now().UnixMilli()),
				Severity: "warn", Title: "Temperatura alta en " + router.Name,
				Description: fmt.Sprintf("%d °C, por encima del umbral (65 °C)", *router.Temp),
				Time:        "ahora mismo", Read: false, RouterID: cfg.ID,
			}}, l.alerts...)
			if len(l.alerts) > 100 {
				l.alerts = l.alerts[:100]
			}
		}
		l.mu.Unlock()
		routerList = append(routerList, router)
	}
	devices := l.buildDevices(polled)
	// Topología v5: puertos FDB + switches/hipervisores inferidos
	devices, distNodes := inferTopology(polled, devices)
	// Aviso de señal débil (< -70 dBm): una alerta por dispositivo y día
	for _, d := range devices {
		if d.Online && d.SignalDbm != nil && *d.SignalDbm < -70 {
			l.mu.Lock()
			last := l.weakAlerted[d.MAC]
			if time.Now().UnixMilli()-last > 24*3600e3 {
				l.weakAlerted[d.MAC] = time.Now().UnixMilli()
				l.alerts = append([]AlertEvent{{
					ID: fmt.Sprintf("alert-weak-%s-%d", d.MAC, time.Now().UnixMilli()),
					Severity: "warn", Title: "Señal débil en " + d.Name,
					Description: fmt.Sprintf("%d dBm en %s — revisa cobertura o acerca un AP", *d.SignalDbm, d.RouterID),
					Time:        "ahora mismo", Read: false, RouterID: d.RouterID,
				}}, l.alerts...)
				if len(l.alerts) > 100 {
					l.alerts = l.alerts[:100]
				}
			}
			l.mu.Unlock()
		}
	}
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
	l.mu.Lock()
	alertsCopy := append([]AlertEvent(nil), l.alerts...)
	l.mu.Unlock()
	unread := 0
	for _, a := range alertsCopy {
		if !a.Read {
			unread++
		}
	}
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
		Ts:                time.Now().Unix(),
	}, nil
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
		// 3) Varios: el vecino es un switch/hub
		if len(all) > 1 {
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
			if infraMac != "" {
				lease := leaseMap[infraMac]
				port.ConnectedTo = lease.Hostname
				port.DeviceMac = infraMac
				if lease.IP != "" {
					port.Detail = lease.IP
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
		Series: PerfSeries{H1: seriesOf("1h"), H24: seriesOf("24h"), D7: seriesOf("7d")},
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
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]AlertEvent(nil), l.alerts...)
}

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

// dawnNetwork es la respuesta de `ubus call dawn get_network`.
type dawnNetwork map[string]map[string]struct {
	Hostname           string  `json:"hostname"`
	Freq               float64 `json:"freq"`
	Channel            int     `json:"channel"`
	ChannelUtilization float64 `json:"channel_utilization"`
	NumSta             int     `json:"num_sta"`
	Local              bool    `json:"local"`
	Iface              string  `json:"iface"`
}

// GetDawn: red DAWN (roaming/band-steering). nil si ningún router responde.
func (l *Live) GetDawn(context.Context) (*Dawn, error) {
	l.mu.Lock()
	routers := append([]RouterConfig(nil), l.routers...)
	polled := l.lastPolled
	l.mu.Unlock()
	var firstData dawnNetwork
	mesh := []DawnMesh{}
	for _, cfg := range routers {
		p := polled[cfg.ID]
		if p == nil {
			continue
		}
		name := cfg.Name
		if name == "" {
			name = cfg.ID
		}
		out, err := l.pool.Run(cfg.Host, "ubus call dawn get_network", 0)
		if err != nil {
			mesh = append(mesh, DawnMesh{RouterID: cfg.ID, Name: name, Dawn: false, ApsSeen: 0})
			continue
		}
		var data dawnNetwork
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
	aps := []DawnAP{}
	for ssid, bssids := range firstData {
		for bssid, ap := range bssids {
			band := "2.4 GHz"
			if ap.Freq >= 5000 {
				band = "5 GHz"
			}
			aps = append(aps, DawnAP{
				SSID: ssid, BSSID: bssid, Hostname: ap.Hostname, Band: band,
				Channel: ap.Channel, UtilizationPct: ap.ChannelUtilization,
				Clients: ap.NumSta, Local: ap.Local, Iface: ap.Iface,
			})
		}
	}
	sort.Slice(aps, func(i, j int) bool {
		if aps[i].Hostname != aps[j].Hostname {
			return aps[i].Hostname < aps[j].Hostname
		}
		return aps[i].Band < aps[j].Band
	})
	return &Dawn{APs: aps, Mesh: mesh}, nil
}
