// Package adapters — contrato entre el núcleo (handlers, poller, SSE) y los
// adapters de datos (demo / live OpenWrt).
//
// El tipo central es Snapshotter (SPEC §7): los handlers NUNCA hablan con
// routers directamente, solo con esta interfaz. Las formas JSON de este
// fichero son CONTRATO EXACTO con el frontend (camelCase, null vs ausente):
//   - Campos con omitempty = ausentes cuando no aplican (paridad `undefined`).
//   - Slices nil serializan como null; slices vacíos no-nil como [].
//     En RouterDetail.Radios la demo devuelve null si no hay radios y el live
//     devuelve [] (quirk del SPEC §7.8) — se expresa con nil vs no-nil.
//   - Epochs: ms en DB, SEGUNDOS en Overview.Ts.
package adapters

import "context"

// ---------------------------------------------------------------------------
// Bloques del Overview (SPEC §7.8)
// ---------------------------------------------------------------------------

// HealthDelta es una penalización del health score.
type HealthDelta struct {
	Label string `json:"label"`
	Delta int    `json:"delta"`
}

// HealthScore global (demo: canon; live: computeHealth).
type HealthScore struct {
	Score     int           `json:"score"`
	Label     string        `json:"label"` // "Excelente"|"Bueno"|"Atención"
	Caption   string        `json:"caption"`
	Note      string        `json:"note"`
	Breakdown []HealthDelta `json:"breakdown"`
}

// WAN stats (vía gateway).
type WAN struct {
	Plan          string  `json:"plan"`
	DownMbps      float64 `json:"downMbps"`
	UpMbps        float64 `json:"upMbps"`
	LatencyMs     float64 `json:"latencyMs"`
	LossPct       float64 `json:"lossPct"`
	PublicIP      string  `json:"publicIp"`
	Isp           string  `json:"isp"`
	PeakTodayMbps float64 `json:"peakTodayMbps"`
	PeakTodayTime string  `json:"peakTodayTime"`
	AvgDownMbps   float64 `json:"avgDownMbps"`
	Total24h      string  `json:"total24h"`
}

// TrafficPoint es un punto {t, down, up} de las series de tráfico WAN.
type TrafficPoint struct {
	T    string  `json:"t"`
	Down float64 `json:"down"`
	Up   float64 `json:"up"`
}

// TrafficByRange son las 4 series del gateway (claves fijas del contrato).
type TrafficByRange struct {
	H1  []TrafficPoint `json:"1h"`
	H24 []TrafficPoint `json:"24h"`
	D7  []TrafficPoint `json:"7d"`
	D30 []TrafficPoint `json:"30d"`
}

// TopBlocked es un dominio bloqueado por AdGuard.
type TopBlocked struct {
	Domain string `json:"domain"`
	Count  int64  `json:"count"`
}

// AdGuardStats (SPEC §7.3/§7.4). El fallback inactivo es el valor cero con
// Status "inactive" (host "" y port 0 si no configurado).
type AdGuardStats struct {
	Host            string       `json:"host"`
	Port            int          `json:"port"`
	Status          string       `json:"status"` // "active"|"inactive"
	Queries24h      int64        `json:"queries24h"`
	Blocked24h      int64        `json:"blocked24h"`
	BlockedPct      float64      `json:"blockedPct"`
	TrackersBlocked int64        `json:"trackersBlocked"`
	DNSLatencyMs    int          `json:"dnsLatencyMs"`
	ClientsUsing    int          `json:"clientsUsing"`
	ClientsTotal    int          `json:"clientsTotal"`
	TopBlocked      []TopBlocked `json:"topBlocked"`
	FilterLists     int          `json:"filterLists"`
	Rules           int          `json:"rules"`
}

// WGPeer es un peer WireGuard (bytes formateados ES: "1,2 GB").
type WGPeer struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	TunnelIP      string `json:"tunnelIp"`
	Active        bool   `json:"active"`
	LastHandshake string `json:"lastHandshake"`
	Rx            string `json:"rx"`
	Tx            string `json:"tx"`
}

// WireGuardStats (quirk: Status siempre "active" si `wg show` respondió).
type WireGuardStats struct {
	Interface string   `json:"interface"`
	Subnet    string   `json:"subnet"`
	Status    string   `json:"status"` // "active"|"inactive"
	Peers     []WGPeer `json:"peers"`
}

// Router es la tarjeta de router (demo canon + live buildRouter).
// CPU/RAM/Temp son *int porque el live puede emitir null en el primer tick
// (primera muestra de /proc/stat; SPEC §7.2 getCpuPercent).
type Router struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Model      string    `json:"model"`
	ModelShort string    `json:"modelShort"`
	Role       string    `json:"role"`
	RoleBadge  string    `json:"roleBadge"`
	IP         string    `json:"ip"`
	MAC        string    `json:"mac,omitempty"`      // ausente si no se conoce (undefined)
	Firmware   string    `json:"firmware,omitempty"` // ausente si no se conoce
	Status     string    `json:"status"`             // "online"|"warn"|"offline"
	Health     int       `json:"health"`
	CPU        *int      `json:"cpu"`
	RAM        *int      `json:"ram"`
	Temp       *int      `json:"temp"`
	Uptime     string    `json:"uptime"` // "<d>d <h>h" | "—"
	Clients    int       `json:"clients"`
	HotMetric  string    `json:"hotMetric,omitempty"` // "temp" solo si temp>65
	Sparkline  []float64 `json:"sparkline"`
	// Backhaul: medio del uplink del router ("cable"|"wifi"). Ausente =
	// cable/desconocido (router sin wifi o sonda no disponible).
	Backhaul string `json:"backhaul,omitempty"`
	// Lldp: vecino LLDP del puerto de uplink cuando es OTRO router conocido
	// (uplink identificado por LLDP). Ausente si no hay dato — la app lo usa
	// para el sufijo "· LLDP" en la etiqueta del uplink.
	Lldp *LldpInfo `json:"lldp,omitempty"`
}

// DeviceTotals del overview (quirk: NewToday=0 en live).
type DeviceTotals struct {
	Total        int `json:"total"`
	Online       int `json:"online"`
	KnownOffline int `json:"knownOffline"`
	NewToday     int `json:"newToday"`
}

// Device es un cliente de red. Las 12 primeras claves son el contrato base
// (SPEC §2.9); el resto solo existe en demo (ausentes en live).
// SignalDbm es null cuando no hay medida (nunca ausente).
type Device struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Manufacturer string    `json:"manufacturer"`
	IP           string    `json:"ip"`
	MAC          string    `json:"mac"`
	RouterID     string    `json:"routerId"`
	Band         string    `json:"band"` // "5 GHz"|"2.4 GHz"|"cable"|"—"
	SignalDbm    *int      `json:"signalDbm"`
	TrafficMbps  float64   `json:"trafficMbps"`
	Online       bool      `json:"online"`
	Sparkline    []float64 `json:"sparkline"`
	// --- topología v5 (FDB/LLDP; omitempty: ausentes si no hay datos) ---
	// Port: puerto físico del bridge donde se aprende la MAC (cableados, FDB).
	Port string `json:"port,omitempty"`
	// Lldp: identificación del vecino cuando se anuncia por LLDP.
	Lldp *LldpInfo `json:"lldp,omitempty"`
	// AttachTo: hub del que cuelga en el mapa (router por defecto; id de
	// DistributionNode inferido o de otro Device — hipervisor/switch).
	AttachTo string `json:"attachTo,omitempty"`
	// --- extras demo (omitempty = ausentes en live) ---
	Hostname     string `json:"hostname,omitempty"`
	DHCPLease    string `json:"dhcpLease,omitempty"`
	FirstSeen    string `json:"firstSeen,omitempty"`
	Traffic24hRx string `json:"traffic24hRx,omitempty"`
	Traffic24hTx string `json:"traffic24hTx,omitempty"`
	Adguard      *bool  `json:"adguard,omitempty"` // puntero: demo emite true/false explícito; live lo omite (paridad Node)
	Group        string `json:"group,omitempty"`
	IsNew        bool   `json:"isNew,omitempty"`
	NewThisWeek  bool   `json:"newThisWeek,omitempty"`
	IconOverride string `json:"iconOverride,omitempty"`
}

// LldpInfo identifica un vecino que se anuncia por LLDP (switch gestionado,
// AP, host…). Campos del `lldpcli -f json show neighbors`.
type LldpInfo struct {
	Chassis  string `json:"chassis,omitempty"`
	Mgmt     string `json:"mgmt,omitempty"`
	Caps     string `json:"caps,omitempty"`
	PortDesc string `json:"portDesc,omitempty"`
}

// DistributionNode: puerto físico con varias MACs aprendidas en el FDB.
// "inferred" = OUI heterogéneo (switch o bridge desconocido, sin IP);
// "hypervisor" = OUI de hipervisor (Proxmox/VMware/Hyper-V/KVM) → sus
// CTs/VMs se anidan bajo el host en el mapa;
// "managed" = identificado por LLDP (vecino anunciado en ese puerto cuya
// chassis-MAC está entre las aprendidas): lleva Name (chassis), Ip (mgmt)
// y Lldp con las capacidades/puerto remoto anunciados.
type DistributionNode struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"` // "inferred"|"hypervisor"|"managed"
	RouterID string    `json:"routerId"`
	Port     string    `json:"port"`
	MacCount int       `json:"macCount"`
	// HostDeviceID: hipervisor → id del Device host (Proxmox…), si es cliente.
	HostDeviceID string    `json:"hostDeviceId,omitempty"`
	Name         string    `json:"name,omitempty"`
	Ip           string    `json:"ip,omitempty"` // managed: mgmt-ip anunciada por LLDP
	// Mac: chassis-MAC del vecino cuando kind='managed' (SPEC-CANON D1). La
	// app la usa para excluir del mapa el chip del Device del switch (que
	// existe como Device Y como nodo managed, sin duplicar el render).
	Mac  string    `json:"mac,omitempty"`
	Lldp *LldpInfo `json:"lldp,omitempty"`
}

// AlertEvent (máx 100 en memoria, más recientes primero).
type AlertEvent struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"` // "warn"|"critical"|"info"|"ok"
	Title       string `json:"title"`
	Description string `json:"description"`
	Time        string `json:"time"` // relativo ES ("hace 12 min") — el frontend lo muestra tal cual
	Read        bool   `json:"read"`
	RouterID    string `json:"routerId"`
}

// Overview es el bundle completo (SPEC §7.8). Ts en SEGUNDOS.
type Overview struct {
	Health       HealthScore    `json:"health"`
	WAN          WAN            `json:"wan"`
	Traffic      TrafficByRange `json:"traffic"`
	Adguard      AdGuardStats   `json:"adguard"`   // fallback inactivo si no configurado (nunca null en live)
	Wireguard    WireGuardStats `json:"wireguard"` // fallback inactivo si error SSH
	Routers      []Router       `json:"routers"`
	DeviceTotals DeviceTotals   `json:"deviceTotals"`
	TopDevices   []Device       `json:"topDevices"` // 5 por trafficMbps desc
	Alerts       []AlertEvent   `json:"alerts"`
	UnreadAlerts int            `json:"unreadAlerts"`
	// DistributionNodes: switches/hipervisores inferidos del FDB (topología
	// v5). Vacío/ausente si aún no hay datos FDB: el mapa cuelga los
	// cableados del router (degradación amable).
	DistributionNodes []DistributionNode `json:"distributionNodes,omitempty"`
	Ts                int64              `json:"ts"` // floor(now/1000) — SEGUNDOS
}

// ---------------------------------------------------------------------------
// RouterDetail (GET /api/routers/:id; SPEC §7.8 y demo §7.1)
// ---------------------------------------------------------------------------

// PerfPoint es un punto de las series de rendimiento {t, cpu, ram, temp}.
type PerfPoint struct {
	T    string  `json:"t"`
	CPU  float64 `json:"cpu"`
	RAM  float64 `json:"ram"`
	Temp float64 `json:"temp"`
}

// PerfSeries son las 3 series del detalle (claves fijas).
type PerfSeries struct {
	H1  []PerfPoint `json:"1h"`
	H24 []PerfPoint `json:"24h"`
	D7  []PerfPoint `json:"7d"`
}

// EthPort es una boca ethernet ({id, label, up, speed?} + enriquecimiento de
// vecino en el detalle: connectedTo/deviceMac/detail, SPEC §7.8).
type EthPort struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Up          bool   `json:"up"`
	Speed       string `json:"speed,omitempty"` // solo si up ("1 Gbps"|"100 Mbps")
	ConnectedTo string `json:"connectedTo,omitempty"`
	DeviceMac   string `json:"deviceMac,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// Radio agrega una banda wifi ({name, channel, widthMhz, powerDbm, clients}).
type Radio struct {
	Name      string  `json:"name"` // "5 GHz"|"2.4 GHz"
	Channel   int     `json:"channel"`
	WidthMhz  int     `json:"widthMhz"`
	PowerDbm  float64 `json:"powerDbm"`
	Clients   int     `json:"clients"`
	Congested bool    `json:"congested,omitempty"` // solo demo
}

// Backhaul del AP (null en gateway): {kind:'cable', headline, latencyMs} o el
// objeto wireless de la demo (routerExtras.backhaul). Por eso es `any`.
// El detalle de gateway emite null (nil).

// RouterDetail es la respuesta de GET /api/routers/:id.
// Radios: nil → null (demo sin radios), no-nil vacío → [] (live). Quirk SPEC.
// Backhaul y Extras son `any`: la demo tiene objetos ricos canónicos
// (routerExtras, dataset.js) y el live construye los suyos (SPEC §7.8).
type RouterDetail struct {
	Router   Router     `json:"router"`
	Ports    []EthPort  `json:"ports"`
	Radios   []Radio    `json:"radios"` // nil → null (demo), [] (live)
	Backhaul any        `json:"backhaul"`
	Series   PerfSeries `json:"series"`
	Clients  []Device   `json:"clients"`
	Extras   any        `json:"extras"`
	// --- solo gateway (demo: solo flint2; live: solo el gateway) ---
	Adguard          *AdGuardStats  `json:"adguard,omitempty"`
	Wireguard        *WireGuardStats `json:"wireguard,omitempty"`
	AdguardSeries24h []AdGuardHour  `json:"adguardSeries24h,omitempty"`
	WANLatency       *WANLatency    `json:"wanLatency,omitempty"`
	WGPeerExtras     any            `json:"wgPeerExtras,omitempty"`
	WGTotals30d      *WGTotals      `json:"wgTotals30d,omitempty"`
}

// AdGuardHour es un punto de la serie horaria AdGuard del detalle del gateway
// (demo; shape literal de dataset.js buildAdGuardSeries: {t,permitidas,bloqueadas}).
type AdGuardHour struct {
	T          string `json:"t"`
	Permitidas int64  `json:"permitidas"`
	Bloqueadas int64  `json:"bloqueadas"`
}

// WANLatency (solo detalle del gateway, demo).
type WANLatency struct {
	Last24h []float64       `json:"last24h"`
	Stats   WANLatencyStats `json:"stats"`
}

// WANLatencyStats {avgMs, jitterMs, lossPct}.
type WANLatencyStats struct {
	AvgMs    float64 `json:"avgMs"`
	JitterMs float64 `json:"jitterMs"`
	LossPct  float64 `json:"lossPct"`
}

// WGTotals {rx, tx} formateados ES.
type WGTotals struct {
	Rx string `json:"rx"`
	Tx string `json:"tx"`
}

// ---------------------------------------------------------------------------
// DAWN (GET /api/dawn; SPEC §7.6) y AdGuard clients (§2.13)
// ---------------------------------------------------------------------------

// DawnAP es un punto de acceso visto por DAWN.
type DawnAP struct {
	SSID           string  `json:"ssid"`
	BSSID          string  `json:"bssid"`
	Hostname       string  `json:"hostname"`
	Band           string  `json:"band"` // freq >= 5000 ? "5 GHz" : "2.4 GHz"
	Channel        int     `json:"channel"`
	UtilizationPct float64 `json:"utilizationPct"`
	Clients        int     `json:"clients"`
	Local          bool    `json:"local"`
	Iface          string  `json:"iface"`
}

// DawnMesh marca por router si tiene DAWN y cuántos APs ve.
type DawnMesh struct {
	RouterID string `json:"routerId"`
	Name     string `json:"name"`
	Dawn     bool   `json:"dawn"`
	ApsSeen  int    `json:"apsSeen"`
}

// Dawn es la respuesta de /api/dawn (503 si ningún router tiene DAWN).
type Dawn struct {
	APs  []DawnAP   `json:"aps"`
	Mesh []DawnMesh `json:"mesh"`
}

// AdguardClient es un cliente configurado en AdGuard GL.iNet (§2.13).
type AdguardClient struct {
	Name              string `json:"name"`
	IP                string `json:"ip"`
	UseGlobalSettings bool   `json:"useGlobalSettings"`
	BlockedServices   int    `json:"blockedServices"`
}

// ---------------------------------------------------------------------------
// Filas de persistencia (poller → DB; NO es JSON de API)
// ---------------------------------------------------------------------------

// MetricsRow es una fila de la tabla metrics (ts lo pone el poller, epoch ms).
// Los *float64 admiten null (primera muestra de cpu/bps, SPEC §7.2).
type MetricsRow struct {
	RouterID  string
	CPU       *float64
	RAM       *float64
	Temp      *float64
	LatencyMs *float64
	RxBps     *float64
	TxBps     *float64
}

// RouterConfig es una fila de la tabla routers (fuente de verdad; is_gateway
// como booleano, orden is_gateway DESC, created_at ASC — SPEC §8.2).
type RouterConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Type      string `json:"type"` // "glinet"|"openwrt"
	IsGateway bool   `json:"is_gateway"`
	CreatedAt int64  `json:"created_at"` // epoch ms
}

// ---------------------------------------------------------------------------
// Snapshotter — la interfaz que consumen handlers, poller y SSE
// ---------------------------------------------------------------------------

// Snapshotter es el contrato del adapter de datos (paridad con la interfaz JS
// de SPEC §7: mode/tick/setRouters/getOverview/getRouters/getRouterDetail/
// getDevices/getAlerts/getMetricsRows/getDawn/getAdguardClients/close;
// getAdguardRow() está muerto en Node y NO se porta).
//
// Convenciones de retorno (consumidas por internal/httpapi):
//   - GetRouterDetail: (nil, nil) → 404 {"error":"not_found"}.
//   - GetDawn: (nil, nil) → 503 {"error":"unavailable"}.
//   - GetAdguardClients: (nil, nil) → 404 {"error":"not_configured"};
//     (x, err) → 502 {"error":"adguard_error","message":err}.
//   - GetOverview nunca devuelve nil sin error; el handler lo serializa tal
//     cual (shape Overview exacto).
type Snapshotter interface {
	// Mode es "demo" | "live" (lo reportan /api/health y /api/auth/me).
	Mode() string
	// Tick avanza el estado interno (demo: random walk; live: no-op, el
	// sondeo ocurre en GetOverview). Lo llama el poller cada 5 s.
	Tick(ctx context.Context) error
	// SetRouters resincroniza la configuración de routers en caliente (CRUD
	// de /api/config/routers). En demo es no-op.
	SetRouters(list []RouterConfig)
	// GetOverview construye el snapshot completo (single-flight en live).
	GetOverview(ctx context.Context) (*Overview, error)
	// GetRouters lista las tarjetas de router actuales.
	GetRouters(ctx context.Context) []Router
	// GetRouterDetail devuelve el detalle o (nil, nil) si el id no existe.
	GetRouterDetail(ctx context.Context, id string) (*RouterDetail, error)
	// GetDevices lista todos los dispositivos (sin paginar; la paginación y
	// filtros los aplica el handler de /api/devices).
	GetDevices(ctx context.Context) []Device
	// GetAlerts lista las alertas (más recientes primero, máx 100).
	GetAlerts(ctx context.Context) []AlertEvent
	// GetMetricsRows devuelve las filas para la tabla metrics del tick
	// actual (el poller solo persiste si Mode() != "demo").
	GetMetricsRows(ctx context.Context) []MetricsRow
	// GetDawn devuelve la malla DAWN o (nil, nil) si no hay DAWN.
	GetDawn(ctx context.Context) (*Dawn, error)
	// GetAdguardClients devuelve los clientes AdGuard, (nil, nil) si no hay
	// cliente configurado o no soporta queryClients, o error → 502.
	GetAdguardClients(ctx context.Context) ([]AdguardClient, error)
	// Close libera recursos (conexiones SSH, clientes HTTP).
	Close() error
}
