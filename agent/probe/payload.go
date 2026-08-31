// payload.go — wire format de POST /api/ingest/agent (SPEC-AGENTE-PILOTO §1).
// `data` lleva los shapes PARSEADOS de las sondas de este package (los mismos
// que produce el sondeo SSH de internal/adapters/openwrt.go), para que el
// pipeline del servidor (snapshot → SSE → persistencia) no cambie.
package probe

// Payload es el cuerpo JSON único que el agente empuja cada intervalo.
type Payload struct {
	Router  string `json:"router"`  // slug del equipo (token agent.token.<slug>)
	Ts      int64  `json:"ts"`      // unix SEGUNDOS de la muestra
	Version string `json:"version"` // versión del agente (la muestra GET /api/agents)
	// Kind declara el tipo de pusher (#288): "" y "native" = agente nativo
	// netpulse-agent; "external" = pusher externo (scraper de un switch
	// gestionado, integración de terceros). El server lo usa para la UI y
	// para escalar ventanas de frescura.
	Kind string `json:"kind,omitempty"`
	// Interval declara la cadencia de push en SEGUNDOS (#288). 0 = default
	// del agente nativo (~30 s). Un pusher externo que empuja cada 5 min
	// declara 300 y el server amplía su TTL a 3x ese intervalo.
	Interval int         `json:"interval,omitempty"`
	Data     PayloadData `json:"data"`
}

// PayloadData agrupa las secciones del tier rápido. Punteros/mapas nil =
// sonda fallida en este push (el servidor conserva el último dato bueno,
// mismo anti-parpadeo que el pipeline SSH).
type PayloadData struct {
	System   *SystemData   `json:"system,omitempty"`
	Wireless *WirelessData `json:"wireless,omitempty"`
	DHCP     *DHCPData     `json:"dhcp,omitempty"`
	FDB      *FDBData      `json:"fdb,omitempty"`
	Dawn     *DawnData     `json:"dawn,omitempty"`   // Fase 14: DAWN roaming (solo si instalado; compat rollback)
	Usteer   *UsteerData   `json:"usteer,omitempty"` // usteer roaming (solo si instalado)
	// LuCI: etiquetas de puertos/VLANs del router (issue #258), si están
	// definidas en /etc/config/luci.
	LuCI *LuCILabels `json:"luci,omitempty"`
	// NetIf: contadores acumulados POR INTERFAZ de /proc/net/dev (issue
	// #305). El servidor calcula los rates por boca con el delta entre
	// payloads. nil = sonda fallida (conserva la última buena).
	NetIf map[string]IfCounters `json:"netIf,omitempty"`
	// Vlans: VLANs del bridge (issue #315, bridge vlan show). nil = sonda
	// fallida o router sin bridge vlan filtering.
	Vlans []VlanPort `json:"vlans,omitempty"`
	// Discovery: mDNS services and randomized MAC detection (#338).
	Discovery *DiscoveryData `json:"discovery,omitempty"`
}

// SystemData: salud del equipo + tráfico + latencias.
type SystemData struct {
	SysInfo   *SysInfo   `json:"sysinfo,omitempty"`
	Board     *BoardInfo `json:"board,omitempty"`
	CPU       *int       `json:"cpu"`  // null en la primera muestra (delta /proc/stat)
	Temp      *int       `json:"temp"` // null si no hay thermal zone
	RxBps     *float64   `json:"rxBps"`
	TxBps     *float64   `json:"txBps"`
	LatencyMs *float64   `json:"latencyMs"`           // gateway: ping WAN; AP: ping al gateway
	LossPct   *float64   `json:"lossPct"`             // solo gateway (ping WAN)
	Backhaul  string     `json:"backhaul,omitempty"`  // "cable"|"wifi" (ausente = desconocido)
	BridgeMAC string     `json:"bridgeMac,omitempty"` // MAC de br-lan (uplinks/topología)
}

// WirelessData: clientes asociados + radios agregadas por banda.
type WirelessData struct {
	Clients map[string]WirelessClient `json:"clients"` // {} = sin clientes; sección ausente = sonda fallida
	Radios  []Radio                   `json:"radios"`
}

// DHCPData: leases ipv4 (mac en mayúsculas) + clientes GL.iNet.
type DHCPData struct {
	Leases []DhcpLease `json:"leases"`
	// GlClients (GL.iNet): base de clientes del firmware, superset de las
	// leases — resuelve IPs de equipos con IP estática o sin lease en el
	// dnsmasq del Flint2 (issue #5 bug 1). Vacío/ausente en otros routers.
	GlClients []DhcpLease `json:"glClients,omitempty"`
}

// FDBData: MAC aprendida → puerto del bridge + puertos ethernet.
type FDBData struct {
	MACs  map[string]string `json:"macs"` // {} = sin entradas; ausente = sonda fallida
	Ports []EthPort         `json:"ports"`
}

// DawnData: estado de DAWN (roaming/band-steering, Fase 14). Solo si DAWN
// está instalado en el router. Trimmed del output de `ubus call dawn
// get_network` — solo lo que la UI necesita (APs + señal por cliente).
type DawnData struct {
	SSIDs map[string]DawnSSID `json:"ssids"`
}

// DawnSSID: por red WiFi (p. ej. "temiscira").
type DawnSSID struct {
	APs     []DawnAP              `json:"aps"`
	Clients map[string]DawnClient `json:"clients"` // MAC upper → cliente
}

// DawnAP: un punto de acceso (BSSID) visto por DAWN en este SSID.
type DawnAP struct {
	BSSID       string `json:"bssid"`
	Hostname    string `json:"hostname,omitempty"`
	Channel     int    `json:"channel"`
	Freq        int    `json:"freq"`
	Utilization int    `json:"utilization"` // %
	Clients     int    `json:"clients"`
	Local       bool   `json:"local"` // true si es este router
	HT          bool   `json:"ht"`
	VHT         bool   `json:"vht"`
}

// DawnClient: un cliente WiFi visto por DAWN con señal al AP asociado.
type DawnClient struct {
	BSSID  string `json:"bssid"`
	Signal int    `json:"signal"` // dBm (negativo)
	HT     bool   `json:"ht"`
	VHT    bool   `json:"vht"`
}

// UsteerData: estado de usteer (roaming/steering). Solo si usteer está
// instalado en el router. Trimmed del output de `ubus call usteer local_info`,
// `remote_info` y `connected_clients` — solo lo que la UI necesita (APs por
// SSID + clientes con señal).
type UsteerData struct {
	SSIDs map[string]UsteerSSID `json:"ssids"`
}

// UsteerSSID: por red WiFi (p. ej. "temiscira").
type UsteerSSID struct {
	APs     []UsteerAP              `json:"aps"`
	Clients map[string]UsteerClient `json:"clients"` // MAC upper → cliente
}

// UsteerAP: un punto de acceso (local o remoto) visto por usteer en este SSID.
type UsteerAP struct {
	BSSID    string `json:"bssid"`
	Hostname string `json:"hostname,omitempty"` // IP del AP remoto; vacío si local
	Freq     int    `json:"freq"`               // MHz (>= 5000 → 5 GHz)
	Load     int    `json:"load"`               // utilización % reportada por usteer
	Clients  int    `json:"clients"`            // n_assoc
	Local    bool   `json:"local"`              // true si es este router
}

// UsteerClient: un cliente WiFi visto por usteer con señal al AP asociado.
type UsteerClient struct {
	Signal int `json:"signal"` // dBm (negativo)
}

// DiscoveryData: mDNS service discovery + randomized MAC detection (#338).
// Source: umdns browse on OpenWrt; falls back to parsing /tmp/umdns/ if
// available. Best-effort: nil section if umdns is not installed.
type DiscoveryData struct {
	// Services: hostname -> list of mDNS service types advertised.
	// e.g. {"Apple-TV._airplay._tcp.local": ["_airplay._tcp", "_raop._tcp"]}
	Services map[string][]string `json:"services,omitempty"`
	// HostByIP: IP -> hostname from mDNS PTR records. Helps resolve devices
	// that have no DHCP hostname.
	HostByIP map[string]string `json:"hostByIp,omitempty"`
	// RandomMACs: MACs detected as locally-administered (bit 1 of byte 0 set).
	// These are typically iOS/Android private WiFi addresses that rotate.
	RandomMACs []string `json:"randomMacs,omitempty"`
}
