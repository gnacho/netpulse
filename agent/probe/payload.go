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

// PayloadData agrupa las 4 secciones del tier rápido. Punteros/mapas nil =
// sonda fallida en este push (el servidor conserva el último dato bueno,
// mismo anti-parpadeo que el pipeline SSH).
type PayloadData struct {
	System   *SystemData   `json:"system,omitempty"`
	Wireless *WirelessData `json:"wireless,omitempty"`
	DHCP     *DHCPData     `json:"dhcp,omitempty"`
	FDB      *FDBData      `json:"fdb,omitempty"`
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
