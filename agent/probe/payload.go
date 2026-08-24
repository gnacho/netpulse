// payload.go — wire format de POST /api/ingest/agent (SPEC-AGENTE-PILOTO §1).
// `data` lleva los shapes PARSEADOS de las sondas de este package (los mismos
// que produce el sondeo SSH de internal/adapters/openwrt.go), para que el
// pipeline del servidor (snapshot → SSE → persistencia) no cambie.
package probe

// Payload es el cuerpo JSON único que el agente empuja cada intervalo.
type Payload struct {
	Router  string      `json:"router"`  // slug del equipo (token agent.token.<slug>)
	Ts      int64       `json:"ts"`      // unix SEGUNDOS de la muestra
	Version string      `json:"version"` // versión del agente (la muestra GET /api/agents)
	Data    PayloadData `json:"data"`
}

// PayloadData agrupa las secciones del tier rápido. Punteros/mapas nil =
// sonda fallida en este push (el servidor conserva el último dato bueno,
// mismo anti-parpadeo que el pipeline SSH).
type PayloadData struct {
	System   *SystemData   `json:"system,omitempty"`
	Wireless *WirelessData `json:"wireless,omitempty"`
	DHCP     *DHCPData     `json:"dhcp,omitempty"`
	FDB      *FDBData      `json:"fdb,omitempty"`
	Dawn     *DawnData     `json:"dawn,omitempty"` // Fase 14: DAWN roaming (solo si instalado)
	// LuCI: etiquetas de puertos/VLANs del router (issue #258), si están
	// definidas en /etc/config/luci.
	LuCI *LuCILabels `json:"luci,omitempty"`
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
