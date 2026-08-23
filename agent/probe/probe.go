// Package probe — sondas del tier rápido OpenWrt: comandos (los LITERALES que
// internal/adapters/openwrt.go lanza por SSH) + parseo de sus salidas +
// shapes JSON del payload que el agente empuja a POST /api/ingest/agent.
//
// Es la ÚNICA fuente de verdad del parseo: server-go (internal/adapters) lo
// importa con un replace ../agent, y el binario netpulse-agent ejecuta los
// mismos comandos localmente. Solo stdlib (el binario del agente tiene
// objetivo ≤ 3 MB y CGO_ENABLED=0).
package probe

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Comandos (idénticos a los del sondeo SSH de openwrt.go)
// ---------------------------------------------------------------------------

const (
	CmdProcStat = "grep '^cpu ' /proc/stat"
	CmdTemp     = `for f in /sys/class/thermal/thermal_zone*/temp; do [ -r "$f" ] && cat "$f" && break; done`
	CmdNetDev   = "cat /proc/net/dev | tail -n +3"
	// CmdPingWan: latencia + pérdida a internet (solo gateway). %s = target.
	CmdPingWan = "ping -c 3 -W 2 %s 2>/dev/null | tail -2"
	// CmdPingGateway: ping corto al gateway desde un AP. %s = host gateway.
	CmdPingGateway = "ping -c 2 -W 2 %s 2>/dev/null | tail -1"
	CmdDhcpUbus    = "ubus call dhcp ipv4leases"
	CmdDhcpFile    = "cat /tmp/dhcp.leases 2>/dev/null || true"
	// CmdGlClients (GL.iNet): base de clientes completa del firmware. Es un
	// SUPERSET de dhcp.leases: incluye equipos con IP estática o sin lease
	// DHCP que el dnsmasq del Flint2 no lista (issue #5 bug 1).
	CmdGlClients = "ubus call gl-clients list 2>/dev/null || true"
	// CmdIwinfoAssoc: "<mac> <sig> <freq>" por cliente wifi asociado.
	CmdIwinfoAssoc = `for i in $(iwinfo 2>/dev/null | awk '/^[a-z]/ {print $1}'); do ` +
		`freq=$(iwinfo "$i" info 2>/dev/null | sed -n 's/.*Channel: [0-9]* (\([0-9.]*\) GHz).*/\1/p' | head -1); ` +
		`iwinfo "$i" assoclist 2>/dev/null | sed -n 's/^\([0-9A-Fa-f:]\{17\}\) *\(-[0-9]*\).*/\1 \2/p' | while read mac sig; do ` +
		`echo "$mac $sig $freq"; done; done`
	// CmdRadios: "freq|ch|ht|tx|n" por radio (se agrega por banda al parsear).
	CmdRadios = `for i in $(iwinfo 2>/dev/null | awk '/^[a-z]/ {print $1}'); do ` +
		`info=$(iwinfo "$i" info 2>/dev/null) || continue; ` +
		`echo "$info" | grep -q ESSID || continue; ` +
		`freq=$(echo "$info" | sed -n 's/.*Channel: [0-9]* (\([0-9.]*\) GHz).*/\1/p' | head -1); ` +
		`ch=$(echo "$info" | sed -n 's/.*Channel: \([0-9][0-9]*\).*/\1/p' | head -1); ` +
		`ht=$(echo "$info" | sed -n 's/.*HT [Mm]ode: \([A-Za-z0-9]*\).*/\1/p' | head -1); ` +
		`tx=$(echo "$info" | sed -n 's/.*Tx-Power: \([0-9]*\).*/\1/p' | head -1); ` +
		`n=$(iwinfo "$i" assoclist 2>/dev/null | grep -c '^[0-9A-Fa-f:]'); ` +
		`echo "$freq|$ch|$ht|$tx|$n"; done`
	// CmdPortStates: "<name> <operstate> <speed>" por interfaz.
	CmdPortStates = `for d in /sys/class/net/*; do i=$(basename "$d"); ` +
		`echo "$i $(cat $d/operstate 2>/dev/null) $(cat $d/speed 2>/dev/null || echo -1)"; done`
	CmdBoardJSON   = "cat /etc/board.json 2>/dev/null"
	CmdBrifMembers = "BR=br-lan; [ -d /sys/class/net/$BR ] || BR=br0; ls /sys/class/net/$BR/brif/ 2>/dev/null"
	// CmdBridgeFDB: "==PORTS==" (port_no ifname) + "==MACS==" (port mac).
	// No asume `br-lan` ni `brctl`: detecta el bridge real (br-lan o br0) y usa
	// `bridge fdb show` como fallback cuando brctl no está (OpenWrt/GLuON
	// modernos usan iproute2). Issue #253.
	CmdBridgeFDB = `BR=br-lan; [ -d /sys/class/net/$BR ] || BR=br0; ` +
		`echo "==PORTS=="; for d in /sys/class/net/$BR/brif/*; do [ -r "$d/port_no" ] && echo "$(cat $d/port_no) $(basename $d)"; done; ` +
		`echo "==MACS=="; if command -v brctl >/dev/null 2>&1; then ` +
		`brctl showmacs $BR 2>/dev/null | awk 'NR>1 && $3=="no" {print $1, $2}'; ` +
		`else bridge fdb show br $BR 2>/dev/null | awk 'NF>=3 && $1!="01:" && $1!="33:33:" && $1!="ff:ff:" && $1!="00:00:00:00:00:00" {for(i=1;i<=NF;i++) if($i=="dev"){print $(i+1), $1}}'; fi`
	CmdBridgeMAC       = "cat /sys/class/net/br-lan/address 2>/dev/null || cat /sys/class/net/br0/address 2>/dev/null"
	CmdUbusSystemBoard = "ubus call system board"
	CmdUbusSystemInfo  = "ubus call system info"
	CmdUbusWireless    = "ubus call network.wireless status"
)

// ---------------------------------------------------------------------------
// Shapes parseados (salida de las sondas; contrato del payload del agente)
// ---------------------------------------------------------------------------

// SysInfo: ubus system info (uptime, memoria, load).
type SysInfo struct {
	Uptime float64   `json:"uptime"`
	Load   []float64 `json:"load"`
	Memory struct {
		Total     float64 `json:"total"`
		Free      float64 `json:"free"`
		Buffered  float64 `json:"buffered"`
		Available float64 `json:"available"`
	} `json:"memory"`
}

// BoardInfo: ubus system board (metadatos estáticos).
type BoardInfo struct {
	Model    string `json:"model"`
	Hostname string `json:"hostname"`
	System   string `json:"system"`
	Release  struct {
		Version     string `json:"version"`
		Description string `json:"description"`
	} `json:"release"`
}

// DhcpLease es {mac, ip, hostname} (mac en mayúsculas).
type DhcpLease struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
}

// WirelessClient es {signalDbm, band} por MAC.
type WirelessClient struct {
	SignalDbm int    `json:"signalDbm"`
	Band      string `json:"band"`
}

// PortState es {name, up, speed} de una interfaz (/sys operstate+speed).
type PortState struct {
	Name  string `json:"name"`
	Up    bool   `json:"up"`
	Speed string `json:"speed"` // "1 Gbps" | "100 Mbps" | "—"
}

// PortLayout es una boca del layout canónico (/etc/board.json).
type PortLayout struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label"`
	Role  string `json:"role"` // "lan" | "wan"
}

// EthPort es una boca ethernet lista para el detalle ({id, label, up, speed?}).
type EthPort struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Up    bool   `json:"up"`
	Speed string `json:"speed,omitempty"` // solo si up
}

// Radio agrega una banda wifi ({name, channel, widthMhz, powerDbm, clients}).
type Radio struct {
	Name     string  `json:"name"` // "5 GHz"|"2.4 GHz"
	Channel  int     `json:"channel"`
	WidthMhz int     `json:"widthMhz"`
	PowerDbm float64 `json:"powerDbm"`
	Clients  int     `json:"clients"`
}

// ---------------------------------------------------------------------------
// Sistema: /proc/stat, thermal, /proc/net/dev, ping
// ---------------------------------------------------------------------------

// CPUSample es la muestra agregada de "cpu " de /proc/stat.
type CPUSample struct{ Total, IdleAll float64 }

// ParseProcStat parsea "cpu  user nice system idle iowait irq softirq ...".
func ParseProcStat(out string) (CPUSample, error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 5 || fields[0] != "cpu" {
		return CPUSample{}, fmt.Errorf("/proc/stat inesperado: %q", out)
	}
	nums := make([]float64, 0, len(fields)-1)
	for _, f := range fields[1:] {
		n, err := strconv.ParseFloat(f, 64)
		if err != nil {
			n = 0
		}
		nums = append(nums, n)
	}
	get := func(i int) float64 {
		if i < len(nums) {
			return nums[i]
		}
		return 0
	}
	idleAll := get(3) + get(4)                            // idle + iowait
	nonIdle := get(0) + get(1) + get(2) + get(5) + get(6) // user+nice+system+irq+softirq
	return CPUSample{Total: idleAll + nonIdle, IdleAll: idleAll}, nil
}

// CPUPercent: % de CPU por delta de muestras (nil si no hay delta válido:
// primera muestra o contadores reseteados).
func CPUPercent(prev, cur CPUSample) *int {
	dTotal := cur.Total - prev.Total
	dIdle := cur.IdleAll - prev.IdleAll
	if dTotal <= 0 {
		return nil
	}
	v := int(float64(dTotal-dIdle)/float64(dTotal)*100 + 0.5)
	return &v
}

// ParseTempC: °C del primer thermal zone (nil si la salida no es un entero).
func ParseTempC(out string) *int {
	milli, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return nil
	}
	v := milli / 1000
	if milli%1000 >= 500 {
		v++
	}
	return &v
}

var netdevSkipRe = regexp.MustCompile(`^(lo|br-|ifb|wg|tun|tap)`)

// ParseNetDev suma rx/tx de las interfaces físicas (excluye lo, bridges,
// ifb, wg, tun/tap para no duplicar contadores).
func ParseNetDev(out string) (rx, tx float64) {
	lineRe := regexp.MustCompile(`^\s*([\w.-]+):\s*(.*)$`)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		m := lineRe.FindStringSubmatch(line)
		if m == nil || netdevSkipRe.MatchString(m[1]) {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(m[2]))
		num := func(i int) float64 {
			if i < len(fields) {
				n, _ := strconv.ParseFloat(fields[i], 64)
				return n
			}
			return 0
		}
		rx += num(0)
		tx += num(8)
	}
	return rx, tx
}

// NetDevBps: bps por delta de contadores en dt segundos (nil si dt <= 0 o
// contadores reseteados).
func NetDevBps(prevRx, prevTx, rx, tx, dt float64) (rxBps, txBps *float64) {
	if dt <= 0 {
		return nil, nil
	}
	rb := round0(max0((rx - prevRx) * 8 / dt))
	tb := round0(max0((tx - prevTx) * 8 / dt))
	return &rb, &tb
}

var pingLossRe = regexp.MustCompile(`(\d+(?:\.\d+)?)% packet loss`)
var pingRTTRe = regexp.MustCompile(`= [\d.]+/([\d.]+)/[\d.]+/[\d.]+ ms`)

// ParsePingSummary extrae (latencia avg redondeada, pérdida %) del resumen de
// ping; cada una es nil si su línea no está.
func ParsePingSummary(out string) (latency, loss *float64) {
	if m := pingRTTRe.FindStringSubmatch(out); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		v = float64(int(v + 0.5))
		latency = &v
	}
	if m := pingLossRe.FindStringSubmatch(out); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		loss = &v
	}
	return latency, loss
}

// ---------------------------------------------------------------------------
// Clientes (DHCP + wireless)
// ---------------------------------------------------------------------------

// ParseDhcpUbus parsea la respuesta de `ubus call dhcp ipv4leases`
// ({lease: [...]} o {leases: [...]}). Error si no es JSON con esas claves.
func ParseDhcpUbus(raw []byte) ([]DhcpLease, error) {
	type leaseJSON struct {
		MAC      string `json:"mac"`
		IPAddr   string `json:"ip-address"`
		IP       string `json:"ip"`
		Hostname string `json:"hostname"`
	}
	var data struct {
		Lease  []leaseJSON `json:"lease"`
		Leases []leaseJSON `json:"leases"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	leases := data.Lease
	if leases == nil {
		leases = data.Leases
	}
	if leases == nil {
		return nil, fmt.Errorf("ubus dhcp: sin clave lease/leases")
	}
	out := make([]DhcpLease, 0, len(leases))
	for _, l := range leases {
		ip := l.IPAddr
		if ip == "" {
			ip = l.IP
		}
		out = append(out, DhcpLease{MAC: strings.ToUpper(l.MAC), IP: ip, Hostname: l.Hostname})
	}
	return out, nil
}

// ParseDhcpLeasesFile parsea /tmp/dhcp.leases:
// <expiry> <mac> <ip> <hostname> <clientid>
func ParseDhcpLeasesFile(out string) []DhcpLease {
	leases := []DhcpLease{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		p := strings.Fields(line)
		if len(p) < 3 {
			continue
		}
		hostname := ""
		if len(p) > 3 && p[3] != "*" {
			hostname = p[3]
		}
		leases = append(leases, DhcpLease{MAC: strings.ToUpper(p[1]), IP: p[2], Hostname: hostname})
	}
	return leases
}

// ParseGlClients parsea la respuesta de `ubus call gl-clients list`
// (GL.iNet Flint2 y similares): {"clients": {"MAC": {mac, ip, name, online, ...}}}.
// Error si no es JSON con esa forma. Solo devuelve entradas ONLINE con IP:
// la base del firmware acumula historial (cientos de entradas) y las offline
// solo servirían para inflar la lista de dispositivos; los equipos que
// necesitan resolución de IP son los que están en la red ahora (issue #5
// bug 1).
func ParseGlClients(raw []byte) ([]DhcpLease, error) {
	type clientJSON struct {
		MAC      string `json:"mac"`
		IP       string `json:"ip"`
		Name     string `json:"name"`
		Hostname string `json:"hostname"`
		Online   bool   `json:"online"`
	}
	var data struct {
		Clients map[string]clientJSON `json:"clients"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if data.Clients == nil {
		return nil, fmt.Errorf("gl-clients: sin clave clients")
	}
	out := make([]DhcpLease, 0, len(data.Clients))
	for key, c := range data.Clients {
		mac := c.MAC
		if mac == "" {
			mac = key
		}
		if mac == "" || c.IP == "" || !c.Online {
			continue
		}
		hostname := c.Hostname
		if hostname == "" {
			hostname = c.Name
		}
		out = append(out, DhcpLease{MAC: strings.ToUpper(mac), IP: c.IP, Hostname: hostname})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MAC < out[j].MAC })
	return out, nil
}

// ParseWirelessClients parsea líneas "<mac> <sig> <freq>" del bucle iwinfo.
func ParseWirelessClients(out string) map[string]WirelessClient {
	m := map[string]WirelessClient{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		p := strings.Fields(line)
		if len(p) < 2 {
			continue
		}
		freq := 0.0
		if len(p) > 2 {
			freq, _ = strconv.ParseFloat(p[2], 64)
		}
		sig, _ := strconv.Atoi(p[1])
		band := "2.4 GHz"
		if freq >= 5 {
			band = "5 GHz"
		}
		m[strings.ToUpper(p[0])] = WirelessClient{SignalDbm: sig, Band: band}
	}
	return m
}

// ParseWirelessUplink: alguna interfaz con config.mode "sta" activa (radio
// up e ifname asignado = asociada) → uplink inalámbrico. Tolerante con
// radios/entradas raras; error si la respuesta no es JSON.
func ParseWirelessUplink(raw []byte) (bool, error) {
	var radios map[string]struct {
		Up         bool `json:"up"`
		Interfaces []struct {
			Ifname string `json:"ifname"`
			Config struct {
				Mode string `json:"mode"`
			} `json:"config"`
		} `json:"interfaces"`
	}
	if err := json.Unmarshal(raw, &radios); err != nil {
		return false, err
	}
	for _, r := range radios {
		if !r.Up {
			continue
		}
		for _, itf := range r.Interfaces {
			if itf.Config.Mode == "sta" && itf.Ifname != "" {
				return true, nil
			}
		}
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// Puertos
// ---------------------------------------------------------------------------

// ParsePortStates parsea líneas "<name> <operstate> <speed>".
func ParsePortStates(out string) []PortState {
	ports := []PortState{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		p := strings.Fields(line)
		if len(p) < 3 {
			continue
		}
		mbps, _ := strconv.Atoi(p[2])
		speed := "—"
		if mbps > 0 {
			if mbps >= 1000 {
				speed = strconv.Itoa(mbps/1000) + " Gbps"
			} else {
				speed = strconv.Itoa(mbps) + " Mbps"
			}
		}
		ports = append(ports, PortState{Name: p[0], Up: p[1] == "up", Speed: speed})
	}
	return ports
}

// ParsePortLayout parsea board.json → [{id, name, label, role}] (WAN
// normalizado a id 'wan').
func ParsePortLayout(out string) ([]PortLayout, error) {
	var board struct {
		Network struct {
			Lan struct {
				Ports []string `json:"ports"`
			} `json:"lan"`
			Wan struct {
				Device string `json:"device"`
			} `json:"wan"`
		} `json:"network"`
	}
	if err := json.Unmarshal([]byte(out), &board); err != nil {
		return nil, err
	}
	ports := []PortLayout{}
	for _, name := range board.Network.Lan.Ports {
		ports = append(ports, PortLayout{ID: name, Name: name, Label: strings.Replace(name, "lan", "LAN ", 1), Role: "lan"})
	}
	if wanDev := board.Network.Wan.Device; wanDev != "" {
		ports = append([]PortLayout{{ID: "wan", Name: wanDev, Label: "WAN", Role: "wan"}}, ports...)
	}
	return ports, nil
}

// BuildEthPorts: layout + estado /sys → []EthPort listo para el detalle.
// brMembers = miembros del bridge br-lan (AP en bridge re-etiqueta wan→LAN
// N+1); sin layout, fallback heurístico. Literal de openwrt.go GetEthPorts.
func BuildEthPorts(layout []PortLayout, states []PortState, brMembers map[string]bool) []EthPort {
	byName := map[string]PortState{}
	for _, p := range states {
		byName[p.Name] = p
	}
	if len(layout) > 0 {
		lanCount := 0
		for _, p := range layout {
			if p.Role == "lan" {
				lanCount++
			}
		}
		ports := make([]EthPort, 0, len(layout))
		for _, p := range layout {
			st, ok := byName[p.Name]
			up := ok && st.Up
			label := p.Label
			if p.Role == "wan" && brMembers[p.Name] {
				label = "LAN " + strconv.Itoa(lanCount+1)
			}
			ep := EthPort{ID: p.ID, Label: label, Up: up}
			if up {
				ep.Speed = st.Speed
			}
			ports = append(ports, ep)
		}
		return ports
	}
	// Fallback sin config
	lanRe := regexp.MustCompile(`^lan\d+$`)
	lans := []PortState{}
	for _, p := range states {
		if lanRe.MatchString(p.Name) {
			lans = append(lans, p)
		}
	}
	sort.Slice(lans, func(i, j int) bool { return NaturalLess(lans[i].Name, lans[j].Name) })
	ports := make([]EthPort, 0, len(lans)+1)
	for _, p := range lans {
		ep := EthPort{ID: p.Name, Label: strings.Replace(p.Name, "lan", "LAN ", 1), Up: p.Up}
		if p.Up {
			ep.Speed = p.Speed
		}
		ports = append(ports, ep)
	}
	wanName := ""
	if _, ok := byName["wan"]; ok {
		wanName = "wan"
	} else if _, ok := byName["pppoe-wan"]; ok {
		wanName = "eth1"
	} else if _, ok := byName["eth1"]; ok {
		wanName = "eth1"
	}
	if wanName != "" {
		st := byName[wanName]
		ep := EthPort{ID: "wan", Label: "WAN", Up: st.Up}
		if st.Up {
			ep.Speed = st.Speed
		}
		ports = append([]EthPort{ep}, ports...)
	}
	return ports
}

// NaturalLess ordena lan2 < lan10 (localeCompare numeric del JS).
func NaturalLess(a, b string) bool {
	re := regexp.MustCompile(`^(.*?)(\d+)$`)
	ma, mb := re.FindStringSubmatch(a), re.FindStringSubmatch(b)
	if ma != nil && mb != nil && ma[1] == mb[1] {
		na, _ := strconv.Atoi(ma[2])
		nb, _ := strconv.Atoi(mb[2])
		return na < nb
	}
	return a < b
}

var fdbPortRe = regexp.MustCompile(`^(lan\d+|wan|eth\d+|swp\d+|en[a-z0-9]+)$`)

// ParseBridgeFdb parsea la salida ==PORTS==/==MACS== → MAC aprendida → puerto.
// Acepta dos formatos en ==MACS==: brctl (`<port_no> <mac>`) y
// `bridge fdb show` (`<ifname> <mac>`, emitido por CmdBridgeFDB).
func ParseBridgeFdb(out string) map[string]string {
	m := map[string]string{}
	portNames := map[string]string{} // port_no → ifname
	section := byte(0)
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "==PORTS==") {
			section = 'p'
			continue
		}
		if strings.HasPrefix(t, "==MACS==") {
			section = 'm'
			continue
		}
		p := strings.Fields(t)
		if len(p) < 2 {
			continue
		}
		switch section {
		case 'p':
			no, name := p[0], p[1]
			if strings.HasPrefix(no, "0x") {
				if n, err := strconv.ParseInt(no[2:], 16, 64); err == nil {
					no = strconv.FormatInt(n, 10)
				}
			}
			portNames[no] = name
			portNames[name] = name // puerto también localizable por nombre (bridge fdb)
		case 'm':
			no, mac := p[0], p[1]
			if port, ok := portNames[no]; ok && mac != "" && fdbPortRe.MatchString(port) {
				m[strings.ToUpper(mac)] = port
			}
		}
	}
	return m
}

var nonDigitRe = regexp.MustCompile(`\D`)

// ParseRadios: líneas "freq|ch|ht|tx|n" agregadas por banda (suma clientes).
func ParseRadios(out string) []Radio {
	byBand := map[string]*Radio{}
	order := []string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, "|")
		if len(p) < 5 {
			continue
		}
		freq, _ := strconv.ParseFloat(p[0], 64)
		if freq == 0 {
			continue
		}
		band := "2.4 GHz"
		if freq >= 5 {
			band = "5 GHz"
		}
		width, _ := strconv.Atoi(nonDigitRe.ReplaceAllString(p[2], ""))
		if width == 0 {
			width = 20
		}
		ch, _ := strconv.Atoi(p[1])
		tx, _ := strconv.Atoi(p[3])
		n, _ := strconv.Atoi(p[4])
		if cur, ok := byBand[band]; ok {
			cur.Clients += n
		} else {
			byBand[band] = &Radio{Name: band, Channel: ch, WidthMhz: width, PowerDbm: float64(tx), Clients: n}
			order = append(order, band)
		}
	}
	radios := []Radio{}
	for _, b := range order {
		radios = append(radios, *byBand[b])
	}
	return radios
}

func round0(v float64) float64 { return float64(int64(v + 0.5)) }
func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}
