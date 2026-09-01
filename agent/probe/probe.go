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
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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
	// CmdHostapdClients (#368): clientes por AP vía ubus (una llamada por AP,
	// binario pequeñísimo, sin ucode ni libiwinfo). Emite "==AP==<obj>" antes
	// del JSON multi-línea de cada get_clients; el parser de Go trocea por la
	// marca. Mucho más barato que iwinfo en CPUs justas (MT7621: el ucode de
	// iwinfo quemaba 25% de CPU con churn de roaming).
	CmdHostapdClients = `for o in $(ubus list 'hostapd.*' 2>/dev/null); do ` +
		`echo "==AP==$o"; ubus call "$o" get_clients 2>/dev/null; done`
	// CmdWirelessCombined (#368): iwinfo en UNA pasada por interfaz (info una
	// vez + assoclist una vez) emitiendo clientes y resumen de radio juntos.
	// Sustituye a CmdIwinfoAssoc+CmdRadios cuando ubus no está: la mitad de
	// spawns. Líneas "C|mac|sig|freq" (clientes) y "R|freq|ch|ht|tx|n" (radio).
	CmdWirelessCombined = `for i in $(iwinfo 2>/dev/null | awk '/^[a-z]/ {print $1}'); do ` +
		`info=$(iwinfo "$i" info 2>/dev/null) || continue; ` +
		`echo "$info" | grep -q ESSID || continue; ` +
		`freq=$(echo "$info" | sed -n 's/.*Channel: [0-9]* (\([0-9.]*\) GHz).*/\1/p' | head -1); ` +
		`ch=$(echo "$info" | sed -n 's/.*Channel: \([0-9][0-9]*\).*/\1/p' | head -1); ` +
		`ht=$(echo "$info" | sed -n 's/.*HT [Mm]ode: \([A-Za-z0-9]*\).*/\1/p' | head -1); ` +
		`tx=$(echo "$info" | sed -n 's/.*Tx-Power: \([0-9]*\).*/\1/p' | head -1); ` +
		`al=$(iwinfo "$i" assoclist 2>/dev/null); ` +
		`n=$(echo "$al" | grep -c '^[0-9A-Fa-f:]'); ` +
		`echo "R|$freq|$ch|$ht|$tx|$n"; ` +
		`echo "$al" | sed -n "s/^\([0-9A-Fa-f:]\{17\}\) *\(-[0-9]*\).*/C|\1|\2|$freq/p"; done`
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
	// CmdProcArp (#377): tabla ARP del kernel, "IP dev mac ..." por línea.
	CmdProcArp     = "cat /proc/net/arp 2>/dev/null"
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
	// CmdLuCILabels: etiquetas de puertos/VLANs de LuCI (issue #258). OpenWrt
	// snapshots añaden `config switchvlan 'port_labels'` y `'vlan_labels'` en
	// /etc/config/luci con nombres amigables para la topología. Se lee el
	// fichero crudo (uci show luci sería más ruidoso: todo el paquete).
	CmdLuCILabels = "cat /etc/config/luci 2>/dev/null"
	// CmdWanStatus: estado de la interfaz WAN (solo gateway) vía ubus.
	// Da proto ("pppoe"), IP, gateway (ptpaddress/nexthop) y DNS (issue #276).
	CmdWanStatus  = "ubus call network.interface.wan status 2>/dev/null || true"
	CmdBridgeVlan = "bridge vlan show 2>/dev/null || true"

	// CmdMdnsBrowse (#338): mDNS service discovery via umdns (OpenWrt's
	// lightweight mDNS daemon). Returns JSON with hostname -> services.
	// Falls back to empty if umdns is not installed.
	CmdMdnsBrowse = "ubus call umdns browse 2>/dev/null || echo '{}'"
	CmdEthtoolSFP = "ethtool -m %s 2>/dev/null || true"
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

// WanInfo: datos de la conexión WAN (issue #276). Solo el gateway lo rellena;
// los campos vacíos significan "sin datos WAN" (APs/desconocido).
type WanInfo struct {
	Proto   string   `json:"proto,omitempty"`   // "pppoe"|"dhcp"|"static"...
	Device  string   `json:"device,omitempty"`  // interfaz física (p.ej. "eth1.20")
	IP      string   `json:"ip,omitempty"`      // dirección IPv4 pública
	Gateway string   `json:"gateway,omitempty"` // puerta de enlace (nexthop/ptpaddress)
	DNS     []string `json:"dns,omitempty"`     // servidores DNS
}

// DhcpLease es {mac, ip, hostname} (mac en mayúsculas) + señales de huella
// DHCP (vendor class y client-id) y expiración del lease cuando se dispone.
type DhcpLease struct {
	MAC            string `json:"mac"`
	IP             string `json:"ip"`
	Hostname       string `json:"hostname"`
	VendorClass    string `json:"vendorClass,omitempty"`
	ClientID       string `json:"clientId,omitempty"`
	LeaseExpiresAt *int64 `json:"leaseExpiresAt,omitempty"` // Unix segundos; nil si no se conoce
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

// EthPort es una boca ethernet lista para el detalle ({id, label, up, speed?}
// + contadores por puerto #305: iface física, bytes/errores acumulados y
// rates instantáneos; omitempty para no engordar el contrato viejo).
type EthPort struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Up    bool   `json:"up"`
	Speed string `json:"speed,omitempty"` // solo si up
	// Iface física de la boca (/proc/net/dev); puede diferir del id en
	// plataformas swconfig ("1" vs "eth0.1").
	Iface string `json:"iface,omitempty"`
	// Contadores acumulados de la iface (issue #305).
	RxBytes uint64   `json:"rxBytes,omitempty"`
	TxBytes uint64   `json:"txBytes,omitempty"`
	RxErrs  uint64   `json:"rxErrors,omitempty"`
	TxErrs  uint64   `json:"txErrors,omitempty"`
	RxBps   *float64 `json:"rxBps,omitempty"`
	TxBps   *float64 `json:"txBps,omitempty"`
	// Sfp: diagnóstico digital (DDM/DOM) si la boca tiene módulo óptico
	// (#313). nil = sin SFP o sin datos; el servidor lo conserva.
	Sfp *SfpInfo `json:"sfp,omitempty"`
}

// SfpInfo: diagnóstico digital (DDM/DOM) de un módulo SFP (#313). Present
// indica si se leyeron datos (ethtool -m devolvió algo parseable).
type SfpInfo struct {
	Temperature float64 `json:"temperature"`       // grados Celsius
	Voltage     float64 `json:"voltage,omitempty"` // voltios (3.3 V típico)
	TxPower     float64 `json:"txPower"`           // dBm
	RxPower     float64 `json:"rxPower"`           // dBm (negativo)
	Vendor      string  `json:"vendor,omitempty"`  // "FS.COM", "Ubiquiti", ...
	PartNumber  string  `json:"partNumber,omitempty"`
	Present     bool    `json:"present"`
}

// IfCounters son los contadores acumulados de UNA interfaz de /proc/net/dev.
type IfCounters struct {
	Rx    uint64 // bytes
	Tx    uint64 // bytes
	RxErr uint64
	TxErr uint64
}

// IfRate añade a los contadores los rates instantáneos (delta entre muestras;
// punteros nil = primera muestra o iface nueva, sin rate todavía).
type IfRate struct {
	IfCounters
	RxBps *float64
	TxBps *float64
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

// ParseNetDevIfaces devuelve los contadores POR INTERFAZ de /proc/net/dev
// (sin filtrar: el consumidor decide qué ifaces le interesan; las bocas
// físicas se casan por nombre con el layout, issue #305).
func ParseNetDevIfaces(out string) map[string]IfCounters {
	res := map[string]IfCounters{}
	lineRe := regexp.MustCompile(`^\s*([\w.-]+):\s*(.*)$`)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		m := lineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(m[2]))
		num := func(i int) uint64 {
			if i < len(fields) {
				n, _ := strconv.ParseUint(fields[i], 10, 64)
				return n
			}
			return 0
		}
		res[m[1]] = IfCounters{
			Rx: num(0), Tx: num(8), // bytes
			RxErr: num(2), TxErr: num(10),
		}
	}
	return res
}

// IfRates calcula los rates por interfaz con el delta de contadores entre
// dos muestras separadas dt segundos. Ifaces nuevas o dt <= 0 → nil rates.
// Contadores reseteados (reboot) → max0 dentro de NetDevBps da 0, no negativos.
func IfRates(prev, cur map[string]IfCounters, dt float64) map[string]IfRate {
	res := make(map[string]IfRate, len(cur))
	for name, c := range cur {
		r := IfRate{IfCounters: c}
		if p, ok := prev[name]; ok && dt > 0 {
			r.RxBps, r.TxBps = NetDevBps(float64(p.Rx), float64(p.Tx), float64(c.Rx), float64(c.Tx), dt)
		}
		res[name] = r
	}
	return res
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
		VendorID string `json:"vendorid"`
		ClientID string `json:"clientid"`
		Expires  int64  `json:"expires"` // segundos restantes o absoluto según firmware
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
		lease := DhcpLease{MAC: strings.ToUpper(l.MAC), IP: ip, Hostname: l.Hostname, VendorClass: l.VendorID, ClientID: l.ClientID}
		if l.Expires > 0 {
			lease.LeaseExpiresAt = &l.Expires
		}
		out = append(out, lease)
	}
	return out, nil
}

// ParseWanStatus parsea `ubus call network.interface.wan status` (issue #276).
// Extrae proto, interfaz física, IP pública, gateway (nexthop de la ruta por
// defecto, con fallback al ptpaddress) y DNS. Devuelve un WanInfo con los
// campos vacíos si el JSON no tiene datos utilizables.
func ParseWanStatus(raw []byte) WanInfo {
	var data struct {
		Proto  string `json:"proto"`
		Device string `json:"l3_device"`
		IPV4   []struct {
			Address    string `json:"address"`
			PtpAddress string `json:"ptpaddress"`
		} `json:"ipv4-address"`
		Route []struct {
			Target  string `json:"target"`
			Mask    int    `json:"mask"`
			Nexthop string `json:"nexthop"`
		} `json:"route"`
		DNS []string `json:"dns-server"`
	}
	info := WanInfo{}
	if json.Unmarshal(raw, &data) != nil {
		return info
	}
	info.Proto = data.Proto
	info.Device = data.Device
	if len(data.IPV4) > 0 {
		info.IP = data.IPV4[0].Address
		if data.IPV4[0].PtpAddress != "" {
			info.Gateway = data.IPV4[0].PtpAddress
		}
	}
	// El gateway real es el nexthop de la ruta por defecto (0.0.0.0/0).
	for _, r := range data.Route {
		if r.Target == "0.0.0.0" && r.Nexthop != "" {
			info.Gateway = r.Nexthop
			break
		}
	}
	info.DNS = data.DNS
	return info
}

// ParseDhcpLeasesFile parsea /tmp/dhcp.leases:
// <expiry> <mac> <ip> <hostname> <clientid>
func ParseDhcpLeasesFile(out string) []DhcpLease {
	leases := []DhcpLease{}
	now := time.Now().Unix()
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
		clientID := ""
		if len(p) > 4 && p[4] != "*" {
			clientID = p[4]
		}
		var exp *int64
		if secs, err := strconv.ParseInt(p[0], 10, 64); err == nil && secs > now {
			exp = &secs
		}
		leases = append(leases, DhcpLease{MAC: strings.ToUpper(p[1]), IP: p[2], Hostname: hostname, ClientID: clientID, LeaseExpiresAt: exp})
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
// N+1); sin layout, fallback heurístico. ifaces = contadores/rates por iface
// física (issue #305; nil = sin datos, las bocas salen sin stats). Literal de
// openwrt.go GetEthPorts.
func BuildEthPorts(layout []PortLayout, states []PortState, brMembers map[string]bool, ifaces map[string]IfRate) []EthPort {
	applyStats := func(ep *EthPort, iface string) {
		st, ok := ifaces[iface]
		if !ok {
			return
		}
		ep.Iface = iface
		ep.RxBytes, ep.TxBytes = st.Rx, st.Tx
		ep.RxErrs, ep.TxErrs = st.RxErr, st.TxErr
		ep.RxBps, ep.TxBps = st.RxBps, st.TxBps
	}
	byName := map[string]PortState{}
	for _, p := range states {
		byName[p.Name] = p
	}

	used := map[string]bool{}
	ports := make([]EthPort, 0, len(states))

	if len(layout) > 0 {
		lanCount := 0
		for _, p := range layout {
			if p.Role == "lan" {
				lanCount++
			}
		}
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
			applyStats(&ep, p.Name)
			ports = append(ports, ep)
			used[p.Name] = true
		}
	} else {
		// Fallback sin config
		lanRe := regexp.MustCompile(`^lan\d+$`)
		lans := []PortState{}
		for _, p := range states {
			if lanRe.MatchString(p.Name) {
				lans = append(lans, p)
			}
		}
		sort.Slice(lans, func(i, j int) bool { return NaturalLess(lans[i].Name, lans[j].Name) })
		for _, p := range lans {
			ep := EthPort{ID: p.Name, Label: strings.Replace(p.Name, "lan", "LAN ", 1), Up: p.Up}
			if p.Up {
				ep.Speed = p.Speed
			}
			applyStats(&ep, p.Name)
			ports = append(ports, ep)
			used[p.Name] = true
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
			applyStats(&ep, wanName)
			ports = append([]EthPort{ep}, ports...)
			used[wanName] = true
		}
	}

	// Mejora #413/#416: añadir interfaces físicas que no estén ya cubiertas
	// por el layout/fallback (p. ej. eth0, sfp+ en BPI-R4/UniFi).
	phyRe := regexp.MustCompile(`^(eth|sfp|en|swp)[0-9a-zA-Z_\-]*$|^(lan|wan)[0-9]+$`)
	skipRe := regexp.MustCompile(`^(lo|br[-_]?|br-lan|br0|docker|veth|wg|tun|tap|ifb|pppoe|wlan|wpan|phy|gre|gretap|erspan|ip6tnl|sit|teql|bond|dummy|nat64|rmnet|usb|wwan)`)
	extras := make([]EthPort, 0)
	for _, st := range states {
		if used[st.Name] {
			continue
		}
		if skipRe.MatchString(st.Name) {
			continue
		}
		if !phyRe.MatchString(st.Name) {
			continue
		}
		label := st.Name
		if strings.HasPrefix(st.Name, "eth") {
			label = "ETH " + strings.TrimPrefix(st.Name, "eth")
		} else if strings.HasPrefix(st.Name, "sfp") {
			label = "SFP " + strings.TrimPrefix(st.Name, "sfp")
		}
		ep := EthPort{ID: st.Name, Label: label, Up: st.Up}
		if st.Up {
			ep.Speed = st.Speed
		}
		applyStats(&ep, st.Name)
		extras = append(extras, ep)
		used[st.Name] = true
	}
	if len(extras) > 0 {
		sort.Slice(extras, func(i, j int) bool { return NaturalLess(extras[i].ID, extras[j].ID) })
		// Mantener WAN primero, luego LANs, luego extras.
		ports = append(ports, extras...)
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

// LuCILabels: etiquetas de puertos y VLANs de LuCI (issue #258). OpenWrt
// snapshots las definen en /etc/config/luci como secciones switchvlan:
//
//	config switchvlan 'port_labels'
//		option lan1 'Router/Fritzbox'
//	config switchvlan 'vlan_labels'
//		option 1 'LAN'
//
// PortLabels mapea interfaz (lan1…) → etiqueta; VlanLabels mapea id de
// VLAN → etiqueta.
type LuCILabels struct {
	PortLabels map[string]string `json:"portLabels,omitempty"`
	VlanLabels map[string]string `json:"vlanLabels,omitempty"`
}

var (
	luciSectionRe = regexp.MustCompile(`^config\s+(\S+)\s+['"]?([A-Za-z0-9_]+)['"]?`)
	luciOptionRe  = regexp.MustCompile(`^option\s+(\S+)\s+['"]?([^'"]*)['"]?$`)
)

// ParseLuCILabels parsea /etc/config/luci y extrae las secciones
// port_labels/vlan_labels. Devuelve nil si no hay ninguna etiqueta.
func ParseLuCILabels(out string) *LuCILabels {
	var labels LuCILabels
	var section string
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := luciSectionRe.FindStringSubmatch(line); m != nil {
			section = m[2]
			continue
		}
		if section != "port_labels" && section != "vlan_labels" {
			continue
		}
		if m := luciOptionRe.FindStringSubmatch(line); m != nil {
			if m[2] == "" {
				continue
			}
			if section == "port_labels" {
				if labels.PortLabels == nil {
					labels.PortLabels = map[string]string{}
				}
				labels.PortLabels[m[1]] = m[2]
			} else {
				if labels.VlanLabels == nil {
					labels.VlanLabels = map[string]string{}
				}
				labels.VlanLabels[m[1]] = m[2]
			}
		}
	}
	if len(labels.PortLabels) == 0 && len(labels.VlanLabels) == 0 {
		return nil
	}
	return &labels
}

// VlanEntry: una VLAN de un puerto del bridge (issue #315). ID es el número
// de VLAN (1-4094); Tagged=false + Untagged en el wire (Egress Untagged);
// PVID=true indica que este puerto es el puerto nativo de esta VLAN.
type VlanEntry struct {
	ID     int  `json:"id"`
	Tagged bool `json:"tagged"`
	PVID   bool `json:"pvid"`
}

// VlanPort: puerto del bridge con sus VLANs (issue #315).
type VlanPort struct {
	Port  string      `json:"port"`
	Vlans []VlanEntry `json:"vlans"`
}

var vlanIDRe = regexp.MustCompile(`^(\d+)(.*)$`)

// ParseBridgeVlan parsea la salida de `bridge vlan show` (issue #315).
// Formato: líneas "port  vlan-id [flags]" o "        vlan-id [flags]" (continuation).
// Flags: "PVID" = puerto nativo; "Egress Untagged" = untagged en salida.
func ParseBridgeVlan(out string) []VlanPort {
	var ports []VlanPort
	var cur *VlanPort
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r\n")
		if line == "" || strings.HasPrefix(line, "port") {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		isContinuation := len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		var portName string
		var vlanField string
		if isContinuation {
			if cur == nil {
				continue
			}
			vlanField = fields[0]
		} else {
			portName = fields[0]
			if len(fields) < 2 {
				continue
			}
			vlanField = fields[1]
		}
		m := vlanIDRe.FindStringSubmatch(vlanField)
		if m == nil {
			continue
		}
		id, err := strconv.Atoi(m[1])
		if err != nil || id < 1 || id > 4094 {
			continue
		}
		rest := strings.ToLower(m[2])
		if len(fields) > 2 {
			rest += " " + strings.ToLower(strings.Join(fields[2:], " "))
		}
		pvid := strings.Contains(rest, "pvid")
		tagged := !strings.Contains(rest, "egress untagged")
		entry := VlanEntry{ID: id, Tagged: tagged, PVID: pvid}
		if isContinuation {
			cur.Vlans = append(cur.Vlans, entry)
		} else {
			ports = append(ports, VlanPort{Port: portName, Vlans: []VlanEntry{entry}})
			cur = &ports[len(ports)-1]
		}
	}
	return ports
}

var nonDigitRe = regexp.MustCompile(`\D`)

// ParseRadios: líneas "freq|ch|ht|tx|n" agregadas por banda (suma clientes).
// hostapdClientsJSON es el shape de `ubus call hostapd.<if> get_clients`.
type hostapdClientsJSON struct {
	Freq    float64 `json:"freq"`
	Clients map[string]struct {
		Auth       bool `json:"auth"`
		Assoc      bool `json:"assoc"`
		Authorized bool `json:"authorized"`
		Signal     int  `json:"signal"`
	} `json:"clients"`
}

// ParseHostapdClients (#368) parsea el output de CmdHostapdClients: bloques
// "==AP==hostapd.phy0-ap0" seguidos del JSON de get_clients. Solo cuenta
// estaciones asociadas y autorizadas (equivalente a iwinfo assoclist); la
// banda sale del "freq" del bloque (>=5 GHz = "5 GHz"). Mismo shape que
// ParseWirelessClients.
func ParseHostapdClients(out string) map[string]WirelessClient {
	m := map[string]WirelessClient{}
	for _, chunk := range strings.Split(out, "==AP==") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		// La marca lleva el objeto ubus delante; el JSON empieza en "{".
		i := strings.Index(chunk, "{")
		if i < 0 {
			continue
		}
		var j hostapdClientsJSON
		if err := json.Unmarshal([]byte(chunk[i:]), &j); err != nil || j.Freq == 0 {
			continue
		}
		freq := j.Freq
		if freq >= 1000 { // ubus get_clients da MHz; iwinfo da GHz
			freq /= 1000
		}
		band := "2.4 GHz"
		if freq >= 5 {
			band = "5 GHz"
		}
		for mac, st := range j.Clients {
			if !st.Assoc || !st.Authorized {
				continue
			}
			m[strings.ToUpper(mac)] = WirelessClient{SignalDbm: st.Signal, Band: band}
		}
	}
	return m
}

// ParseWirelessCombined (#368) parsea CmdWirelessCombined: líneas "C|mac|sig|
// freq" (clientes, mismo shape que ParseWirelessClients) y "R|freq|ch|ht|tx|n"
// (radios, mismo shape que ParseRadios, que se reutiliza).
func ParseWirelessCombined(out string) (map[string]WirelessClient, []Radio) {
	clients := map[string]WirelessClient{}
	var radioLines []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "C|"):
			p := strings.Split(line, "|")
			if len(p) < 4 {
				continue
			}
			sig, _ := strconv.Atoi(p[2])
			freq, _ := strconv.ParseFloat(p[3], 64)
			band := "2.4 GHz"
			if freq >= 5 {
				band = "5 GHz"
			}
			clients[strings.ToUpper(p[1])] = WirelessClient{SignalDbm: sig, Band: band}
		case strings.HasPrefix(line, "R|"):
			radioLines = append(radioLines, strings.TrimPrefix(line, "R|"))
		}
	}
	return clients, ParseRadios(strings.Join(radioLines, "\n"))
}

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

var (
	sfpTempRe   = regexp.MustCompile(`(?i)Module temperature\s*:\s*(-?[\d.]+)`)
	sfpVoltRe   = regexp.MustCompile(`(?i)Module voltage\s*:\s*([\d.]+)`)
	sfpTxPwrRe  = regexp.MustCompile(`(?i)Laser output power\s*:.*?/\s*(-?[\d.]+)\s*dBm`)
	sfpRxPwrRe  = regexp.MustCompile(`(?i)Laser receiver power\s*:.*?/\s*(-?[\d.]+)\s*dBm`)
	sfpVendorRe = regexp.MustCompile(`(?i)Vendor [Nn]ame\s*:\s*(.+)`)
	sfpPNRe     = regexp.MustCompile(`(?i)Vendor [Pp]art [Nn]umber\s*:\s*(.+)`)
)

// ParseEthtoolSFP parsea la salida de `ethtool -m <iface>` y extrae los
// valores DDM/DOM del módulo (#313). Devuelve nil si la salida está vacía o
// no contiene datos de SFP.
func ParseEthtoolSFP(out string) *SfpInfo {
	if strings.TrimSpace(out) == "" {
		return nil
	}
	info := &SfpInfo{}
	if m := sfpTempRe.FindStringSubmatch(out); m != nil {
		info.Temperature, _ = strconv.ParseFloat(m[1], 64)
		info.Present = true
	}
	if m := sfpVoltRe.FindStringSubmatch(out); m != nil {
		info.Voltage, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := sfpTxPwrRe.FindStringSubmatch(out); m != nil {
		info.TxPower, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := sfpRxPwrRe.FindStringSubmatch(out); m != nil {
		info.RxPower, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := sfpVendorRe.FindStringSubmatch(out); m != nil {
		info.Vendor = strings.TrimSpace(m[1])
	}
	if m := sfpPNRe.FindStringSubmatch(out); m != nil {
		info.PartNumber = strings.TrimSpace(m[1])
	}
	if !info.Present {
		return nil
	}
	return info
}

func round0(v float64) float64 { return float64(int64(v + 0.5)) }
func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

// ParseMdnsBrowse (#338): parsea la salida de `ubus call umdns browse`.
// Formato típico (JSON):
//
//	{ "hostname._service._tcp.local": { "port": 1234, "txt": [...] }, ... }
//
// Extrae: hostname (de la clave, antes del primer punto) y tipos de servicio
// (el segmento _service._tcp). También recoge IPs si aparecen en registros A.
func ParseMdnsBrowse(raw []byte) *DiscoveryData {
	if len(raw) == 0 {
		return nil
	}
	// umdns browse devuelve un objeto JSON con claves "fqdn" y valores que
	// contienen "port", "txt", "ipv4", etc. Parseamos como map genérico.
	var browse map[string]json.RawMessage
	if json.Unmarshal(raw, &browse) != nil {
		return nil
	}
	if len(browse) == 0 {
		return nil
	}

	dd := &DiscoveryData{
		Services: map[string][]string{},
		HostByIP: map[string]string{},
	}
	for fqdn, val := range browse {
		// Extraer hostname: primera parte antes del "."
		hostname := fqdn
		if idx := strings.Index(fqdn, "."); idx > 0 {
			hostname = fqdn[:idx]
		}
		// Extraer tipo de servicio: segundo segmento "_svc._tcp" o "_svc._udp"
		parts := strings.SplitN(fqdn, ".", 3)
		if len(parts) >= 2 && strings.HasPrefix(parts[1], "_") {
			svcType := parts[1]
			if len(parts) >= 3 {
				svcType = parts[1] + "." + strings.SplitN(parts[2], ".", 2)[0]
			}
			dd.Services[hostname] = appendUnique(dd.Services[hostname], svcType)
		}
		// Extraer IP si disponible
		var entry struct {
			IPv4 string `json:"ipv4"`
		}
		if json.Unmarshal(val, &entry) == nil && entry.IPv4 != "" {
			dd.HostByIP[entry.IPv4] = hostname
		}
	}
	if len(dd.Services) == 0 && len(dd.HostByIP) == 0 {
		return nil
	}
	return dd
}

func appendUnique(slice []string, s string) []string {
	for _, x := range slice {
		if x == s {
			return slice
		}
	}
	return append(slice, s)
}

// IsRandomizedMAC returns true if the MAC address has the locally-administered
// bit set (bit 1 of byte 0). Apple, Google, and Samsung use this for WiFi
// privacy features — the device appears as a new MAC periodically.
func IsRandomizedMAC(mac string) bool {
	if len(mac) < 2 {
		return false
	}
	// Byte 0: first two hex chars
	b, err := parseHexByte(mac[0], mac[1])
	if err != nil {
		return false
	}
	return b&0x02 != 0
}

func parseHexByte(hi, lo byte) (byte, error) {
	h, err := hexNibble(hi)
	if err != nil {
		return 0, err
	}
	l, err := hexNibble(lo)
	if err != nil {
		return 0, err
	}
	return h<<4 | l, nil
}

func hexNibble(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	default:
		return 0, fmt.Errorf("invalid hex nibble: %c", c)
	}
}

// ParseArp parsea /proc/net/arp (#377): cabecera + líneas
// "IP net/arp HW flags MAC mask device". Devuelve MAC (upper) → IP,
// ignorando entradas incompletas (flags 0x0) y MACs nulas.
func ParseArp(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] == "IP" {
			continue
		}
		mac := strings.ToUpper(f[3])
		if mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		// flags 0x0 = incomplete: el kernel retiene la IP sin vecino válido.
		if f[2] == "0x0" {
			continue
		}
		if net.ParseIP(f[0]) == nil {
			continue
		}
		m[mac] = f[0]
	}
	return m
}
