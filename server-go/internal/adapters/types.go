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

import (
	"context"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

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
	// ContractDownMbps/ContractUpMbps: velocidad contratada declarada por el
	// admin en Ajustes (issue #151). Punteros: ausentes (null) si no está
	// configurado. Los inyecta el server en handleOverview desde el kv — el
	// adapter no los conoce.
	ContractDownMbps *float64 `json:"contractDownMbps,omitempty"`
	ContractUpMbps   *float64 `json:"contractUpMbps,omitempty"`
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
	// FirmwareTarget: versión objetivo configurada por el admin (issue #241).
	// Ausente si no está configurado (sin comprobación).
	FirmwareTarget string `json:"firmwareTarget,omitempty"`
	// FirmwareOutdated: true si hay target y el firmware instalado no lo
	// cumple (live buildRouter). Ausente en el resto de casos.
	FirmwareOutdated bool `json:"firmwareOutdated,omitempty"`
	// AgentOnly: el router está configurado para funcionar SOLO con agente
	// (sin SSH). El frontend lo usa para marcar "agente no instalado" cuando
	// no hay agente registrado (certeza: el router no es sondeable por SSH).
	AgentOnly bool `json:"agentOnly,omitempty"`
	// Type: "glinet"|"openwrt"|"managed-switch"|"external". El frontend lo usa
	// para NO ofrecer reinstall/upgrade de agentes en dispositivos que no usan
	// el agente nativo (scrapers de switches, etc.).
	Type string `json:"type,omitempty"`
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
	// Infra: rol de infraestructura sellado server-side (Fase 4). La app NO
	// infiere: pinta badge si viene. "hypervisor" (host Proxmox/VMware/…),
	// "ct" (CT/VM anidado bajo hipervisor), "managed-switch" (switch con gestión
	// identificado por LLDP — hoy switch-netgear).
	Infra string `json:"infra,omitempty"` // "hypervisor"|"ct"|"managed-switch"
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
	ID       string `json:"id"`
	Kind     string `json:"kind"` // "inferred"|"hypervisor"|"managed"
	RouterID string `json:"routerId"`
	Port     string `json:"port"`
	MacCount int    `json:"macCount"`
	// HostDeviceID: hipervisor → id del Device host (Proxmox…), si es cliente.
	HostDeviceID string `json:"hostDeviceId,omitempty"`
	Name         string `json:"name,omitempty"`
	Ip           string `json:"ip,omitempty"` // managed: mgmt-ip anunciada por LLDP
	// Mac: chassis-MAC del vecino cuando kind='managed' (SPEC-CANON D1). La
	// app la usa para excluir del mapa el chip del Device del switch (que
	// existe como Device Y como nodo managed, sin duplicar el render).
	Mac  string    `json:"mac,omitempty"`
	Lldp *LldpInfo `json:"lldp,omitempty"`
}

// AlertEvent vive en internal/alerts (SPEC-ALERTAS §1: Category/Urgent/Ts);
// el alias mantiene el contrato adapters.AlertEvent para handlers y tests.
type AlertEvent = alerts.AlertEvent

// ViewModelVersion es la versión del view-model de presentación (SPEC-65
// D65-4). La API es un view-model versionado: un cliente debe rechazar/avisar
// si `vm` supera la versión que soporta. Bump = cualquier cambio incompatible
// de forma (añadir campos opcionales NO bumpea).
const ViewModelVersion = 1

// TopologyOverride: capa 2 manual sobre el autodiscover (issue #142). El
// builder la aplica tras inferTopology y antes de BuildTopoSemantics.
//   - Kind "hypervisor" (MAC): ese host es un hipervisor; las MACs con OUI
//     de hipervisor del mismo puerto+router se anidan bajo él.
//   - Kind "switch" (MAC): ese equipo es un switch gestionado (aunque sin
//     LLDP); su puerto se convierte en distnode managed.
//   - Kind "attach" (MAC + Parent): el target cuelga de `parent` (hipervisor
//     o switch manual).
type TopologyOverride struct {
	ID        string `json:"id"`
	MAC       string `json:"mac"` // target normalizado (minúsculas, ':')
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	Parent    string `json:"parent,omitempty"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// TopoSemantics: modelo semántico de la topología (Fase 4). La app conserva
// SOLO la geometría de píxeles; asignaciones de anillo, enlaces y conteos de
// peers ocultos llegan calculados.
type TopoSemantics struct {
	Links []TopoLink `json:"links"`
	// Rings: routerId → ids de Device en su anillo (orden: cableados primero,
	// luego por banda 5GHz/2.4GHz, estable).
	Rings map[string][]string `json:"rings"`
	// HiddenPeers: routerId → nº de clientes no pintados como chip (el "+N").
	HiddenPeers map[string]int `json:"hiddenPeers,omitempty"`
}

// TopoLink es un enlace semántico del mapa (sin geometría).
type TopoLink struct {
	From string `json:"from"` // id: router | device | distnode | "internet" | "peer-<wgPeerId>"
	To   string `json:"to"`
	Kind string `json:"kind"`           // "wan"|"uplink"|"wired"|"dist"|"wg"
	Port string `json:"port,omitempty"` // puerto físico si aplica
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
	// Topology: semántica de topología precalculada (SPEC-65 D65-3). Puntero:
	// ausente en snapshots viejos → la app usa su cálculo actual como fallback.
	Topology *TopoSemantics `json:"topology,omitempty"`
	// Devices: lista completa de dispositivos (misma snapshot que topology.rings).
	// Omitido en snapshots viejos → el frontend sigue usando /api/devices.
	// Cuando está presente, el frontend lo usa en vez de fetch independiente
	// para garantizar consistencia con los anillos de topología.
	Devices []Device `json:"devices,omitempty"`
	// Dawn: disponibilidad de DAWN (roaming/band-steering) para decidir si la
	// app muestra la entrada /roaming. Puntero: ausente en snapshots viejos y
	// cuando ningún router tiene DAWN → nil = no mostrar la página.
	Dawn *DawnOverview `json:"dawn,omitempty"`
	// Orchestration: el menú de orquestación (escritura en routers) está
	// oculto por defecto y solo se muestra si el admin lo activa en Ajustes
	// (#121). Default false (omitempty).
	Orchestration bool `json:"orchestration,omitempty"`
	// VM: versión del view-model (SPEC-65 D65-4). SIEMPRE presente.
	VM int   `json:"vm"`
	Ts int64 `json:"ts"` // floor(now/1000) — SEGUNDOS
}

// DawnOverview indica si la red tiene DAWN (roaming/band-steering) disponible.
// Lo usa el frontend para mostrar/ocultar la entrada /roaming del menú.
type DawnOverview struct {
	Available bool `json:"available"`
}

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
	Adguard          *AdGuardStats   `json:"adguard,omitempty"`
	Wireguard        *WireGuardStats `json:"wireguard,omitempty"`
	AdguardSeries24h []AdGuardHour   `json:"adguardSeries24h,omitempty"`
	WANLatency       *WANLatency     `json:"wanLatency,omitempty"`
	WGPeerExtras     any             `json:"wgPeerExtras,omitempty"`
	WGTotals30d      *WGTotals       `json:"wgTotals30d,omitempty"`
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

// DawnClient es un cliente visto por un AP en el hearing map de DAWN.
// Signal en -dBm (más cercano a 0 = mejor). Un cliente puede aparecer bajo
// varios BSSIDs: cada AP que lo ve reporta su propia medición de señal.
type DawnClient struct {
	MAC    string `json:"mac"`
	Signal int    `json:"signal"` // -dBm (ej. -65)
	HT     bool   `json:"ht"`
	VHT    bool   `json:"vht"`
}

// DawnAP es un punto de acceso visto por DAWN.
type DawnAP struct {
	SSID           string        `json:"ssid"`
	BSSID          string        `json:"bssid"`
	Hostname       string        `json:"hostname"`
	Band           string        `json:"band"` // freq >= 5000 ? "5 GHz" : "2.4 GHz"
	Channel        int           `json:"channel"`
	UtilizationPct float64       `json:"utilizationPct"`
	ClientCount    int           `json:"clientCount"` // num_sta reportado por DAWN
	Clients        []DawnClient  `json:"clients"`     // clientes vistos por este AP (hearing map)
	Local          bool          `json:"local"`
	Iface          string        `json:"iface"`
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

// ---------------------------------------------------------------------------
// 802.11r (Fast BSS Transition) — Fase 14.3
// ---------------------------------------------------------------------------

// Dot11rIface es una sección wifi-iface de `uci show wireless` con los campos
// relevantes para 802.11r (FT) y sus estándares compañeros (k/v/w) que together
// habilitan el roaming suave en una malla DAWN/hostapd.
type Dot11rIface struct {
	Section string `json:"section"` // uci section name (wifi2g, wifi5g, guest, ...)
	Device  string `json:"device"`  // radio0, radio1
	Ifname  string `json:"ifname"`  // wlan0, wlan1 (puede estar vacío en configs muy nuevas)
	SSID    string `json:"ssid"`
	MAC     string `json:"mac"`               // macaddr (BSSID del BSS)
	Channel int   `json:"channel,omitempty"` // mapeado desde radio.channel
	Band    string `json:"band,omitempty"`   // "2.4 GHz"|"5 GHz" desde radio.band

	Encryption string `json:"encryption,omitempty"` // psk2/sae/psk2-mixed...

	// 802.11r (Fast BSS Transition)
	Dot11REnabled      bool   `json:"dot11rEnabled"`
	MobilityDomain     string `json:"mobilityDomain,omitempty"` // 4 hex
	FTOverDS           bool   `json:"ftOverDs"`                 // true=over-the-ds, false=over-the-air
	FTPSKGenerateLocal bool   `json:"ftPskGenerateLocal"`       // true=local PSK, false=RADIUS externo
	PMKR1Push          bool   `json:"pmkR1Push,omitempty"`      // push PMK R1 a otros APs
	NASID              string `json:"nasid,omitempty"`          // solo configs con RADIUS externo

	// 802.11k (Radio Resource Measurement) — vecino de 802.11r para que el
	// cliente sepa a qué AP saltar (beacon report, neighbor report).
	Dot11KEnabled bool `json:"dot11kEnabled,omitempty"`
	// 802.11v (BSS Transition Management) — el AP sugiere al cliente saltar.
	Dot11VEnabled bool `json:"dot11vEnabled,omitempty"`
	BSSTransition bool `json:"bssTransition,omitempty"`
	// 802.11w (Management Frame Protection / PMF) — required por WPA3.
	MFP bool `json:"mfp,omitempty"`
}

// Dot11rRouter es el estado 802.11r de un router: lista de ifaces wifi-iface
// parseadas de su `uci show wireless`. Available=false si SSH falló.
type Dot11rRouter struct {
	RouterID  string         `json:"routerId"`
	Name      string         `json:"name"`
	Available bool           `json:"available"`
	Ifaces    []Dot11rIface `json:"ifaces"`
}

// Dot11rSSID agrega el estado 802.11r por SSID. Lo construye el servidor a
// partir de los ifaces de todos los routers: EnabledEverywhere=true solo si
// TODOS los ifaces con ese SSID tienen ieee80211r=1.
type Dot11rSSID struct {
	SSID               string   `json:"ssid"`
	EnabledEverywhere  bool     `json:"enabledEverywhere"`
	EnabledCount       int      `json:"enabledCount"`
	TotalCount         int      `json:"totalCount"`
	MobilityDomain     string   `json:"mobilityDomain,omitempty"`
	FTOverDS           bool     `json:"ftOverDs"`
	FTPSKGenerateLocal bool     `json:"ftPskGenerateLocal"`
	IfaceCount         int      `json:"ifaceCount"`
	RouterIDs          []string `json:"routerIds"`
}

// Dot11rOverview es la respuesta de GET /api/dot11r. Available=false si ningún
// router tiene 802.11r (el handler devuelve 503 en ese caso, igual que /dawn).
type Dot11rOverview struct {
	Available bool           `json:"available"`
	SSIDs     []Dot11rSSID   `json:"ssids"`
	Routers   []Dot11rRouter `json:"routers"`
}

// ---------------------------------------------------------------------------
// WiFi Survey (canal utilization) — Fase 14.4
// ---------------------------------------------------------------------------

// SurveyChannel es un canal visto por `iw dev wlanX survey dump`: noise floor
// + contadores busy/active/rx/tx. BusyPct = busy/active (uso del canal),
// RxPct/TxPct desglosan parte de ese uso.
type SurveyChannel struct {
	Freq     int     `json:"freq"`     // MHz (2412, 5180, ...)
	Channel  int     `json:"channel"`  // 1, 6, 11, 36, ... (computado desde freq)
	InUse    bool    `json:"inUse"`    // true si la radio está operando en este canal
	NoiseDbm int     `json:"noiseDbm"` // -90, -76, ... (más cercano a 0 = peor)
	BusyPct  float64 `json:"busyPct"`  // busy_time / active_time * 100
	RxPct    float64 `json:"rxPct"`
	TxPct    float64 `json:"txPct"`
}

// SurveyRadio agrupa los canales survey de un device wifi (wlan0, wlan1).
type SurveyRadio struct {
	Device   string           `json:"device"` // wlan0, wlan1
	Band     string           `json:"band"`   // "2.4 GHz"|"5 GHz" (inferido del primer canal)
	Channels []SurveyChannel  `json:"channels"`
}

// SurveyRouter es el survey de un router: lista de radios wifi-iface.
// Available=false si SSH falló.
type SurveyRouter struct {
	RouterID  string         `json:"routerId"`
	Name      string         `json:"name"`
	Available bool           `json:"available"`
	Radios    []SurveyRadio  `json:"radios"`
}

// SurveyOverview es la respuesta de GET /api/survey. Available=false si
// ningún router responde → el handler devuelve 503.
type SurveyOverview struct {
	Available bool            `json:"available"`
	Routers   []SurveyRouter  `json:"routers"`
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
	AgentOnly bool   `json:"agent_only"`
	CreatedAt int64  `json:"created_at"` // epoch ms
	// FirmwareTarget: versión objetivo configurada por el admin (issue #241).
	// "" = sin comprobar (el live no emite aviso de firmware).
	FirmwareTarget string `json:"firmware_target,omitempty"`
}

// ---------------------------------------------------------------------------
// Snapshotter — la interfaz que consumen handlers, poller y SSE
// ---------------------------------------------------------------------------

// Snapshotter es el contrato del adapter de datos (paridad con la interfaz JS
// de SPEC §7: mode/tick/setRouters/getOverview/getRouters/getRouterDetail/
// getDevices/getAlerts/getMetricsRows/getDawn/getDot11r/getSurvey/getAdguardClients/close;
// getAdguardRow() está muerto en Node y NO se porta).
//
// Convenciones de retorno (consumidas por internal/httpapi):
//   - GetRouterDetail: (nil, nil) → 404 {"error":"not_found"}.
//   - GetDawn: (nil, nil) → 503 {"error":"unavailable"}.
//   - GetDot11r: (nil, nil) → 503 {"error":"unavailable"}.
//   - GetSurvey: (nil, nil) → 503 {"error":"unavailable"}.
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
	// AlertsEngine expone el motor de alertas (SPEC-ALERTAS §3): config,
	// read-state y UnreadCount viven ahí (server truth).
	AlertsEngine() *alerts.Engine
	// GetMetricsRows devuelve las filas para la tabla metrics del tick
	// actual (el poller solo persiste si Mode() != "demo").
	GetMetricsRows(ctx context.Context) []MetricsRow
	// GetDawn devuelve la malla DAWN o (nil, nil) si no hay DAWN.
	GetDawn(ctx context.Context) (*Dawn, error)
	// GetDot11r devuelve el estado 802.11r (FT) por router y SSID, o
	// (nil, nil) si ningún router lo soporta → el handler responde 503.
	GetDot11r(ctx context.Context) (*Dot11rOverview, error)
	// GetSurvey devuelve la utilización por canal wifi (iw survey dump)
	// por router y radio, o (nil, nil) si ningún router responde → 503.
	GetSurvey(ctx context.Context) (*SurveyOverview, error)
	// GetAdguardClients devuelve los clientes AdGuard, (nil, nil) si no hay
	// cliente configurado o no soporta queryClients, o error → 502.
	GetAdguardClients(ctx context.Context) ([]AdguardClient, error)
	// Close libera recursos (conexiones SSH, clientes HTTP).
	Close() error
}
