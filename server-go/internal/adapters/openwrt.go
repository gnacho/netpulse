// openwrt.go — Cliente live para routers OpenWrt / GL.iNet (port de
// src/adapters/openwrt.js, SPEC §7.2):
//
//   1. ubus JSON-RPC sobre HTTP (POST http://<host>/ubus, session login con
//      token cacheado y un solo re-login ante sesión caducada).
//   2. Fallback SSH con pool persistente (sshpool.go; equivalente al
//      ControlMaster del JS).
//
// Comandos y parseo literales del JS; timeouts cortos y errores controlados:
// el caller (live) marca el router offline y sigue con el resto.
package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ubusHTTPTimeout = 4 * time.Second
	iwinfoTimeout   = 8 * time.Second
)

// OpenWrtClient es el cliente de un router (uno por router del config).
type OpenWrtClient struct {
	Host     string
	User     string // para ubus HTTP (default root)
	Password string

	pool *SSHPool

	sid        string     // sesión ubus HTTP cacheada
	lastStat   *cpuSample // /proc/stat previo
	lastNetDev *netSample // /proc/net/dev previo
	// lldpDownUntil: indisponibilidad de lldpd cacheada (≥5 min) para no
	// martillear con un comando que no existe. Solo lo toca el sondeo.
	lldpDownUntil time.Time
}

type cpuSample struct{ total, idleAll float64 }
type netSample struct {
	rx, tx float64
	at     time.Time
}

// NewOpenWrtClient crea el cliente de un router.
func NewOpenWrtClient(cfg RouterConfig, pool *SSHPool, user, password string) *OpenWrtClient {
	if user == "" {
		user = "root"
	}
	return &OpenWrtClient{Host: cfg.Host, User: user, Password: password, pool: pool}
}

// ---------------------------------------------------------------------------
// Transporte ubus HTTP (JSON-RPC)
// ---------------------------------------------------------------------------

type ubusRPCError struct{ msg string }

func (e *ubusRPCError) Error() string { return e.msg }

// ubusHTTPRpc: POST /ubus; devuelve result[1] si result[0] == 0.
func (c *OpenWrtClient) ubusHTTPRpc(sid, object, method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "call",
		"params": []any{sid, object, method, params},
	})
	ctx, cancel := context.WithTimeout(context.Background(), ubusHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+c.Host+"/ubus", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, &ubusRPCError{fmt.Sprintf("ubus HTTP %d", res.StatusCode)}
	}
	var rpc struct {
		Error  json.RawMessage `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &rpc); err != nil {
		return nil, &ubusRPCError{"ubus: respuesta no JSON"}
	}
	if len(rpc.Error) > 0 && string(rpc.Error) != "null" {
		return nil, &ubusRPCError{"ubus RPC error: " + string(rpc.Error)}
	}
	// ubus devuelve [exitCode, payload]
	var result []json.RawMessage
	if err := json.Unmarshal(rpc.Result, &result); err != nil || len(result) != 2 {
		return nil, &ubusRPCError{"ubus call falló: " + string(rpc.Result)}
	}
	var code int
	if err := json.Unmarshal(result[0], &code); err != nil || code != 0 {
		return nil, &ubusRPCError{"ubus call falló: " + string(rpc.Result)}
	}
	return result[1], nil
}

// ubusLogin: session login con sid '0'×32 → ubus_rpc_session.
func (c *OpenWrtClient) ubusLogin() error {
	payload, err := c.ubusHTTPRpc("00000000000000000000000000000000", "session", "login",
		map[string]any{"username": c.User, "password": c.Password})
	if err != nil {
		return err
	}
	var res struct {
		Session string `json:"ubus_rpc_session"`
	}
	if err := json.Unmarshal(payload, &res); err != nil || res.Session == "" {
		return &ubusRPCError{"ubus login: sin ubus_rpc_session"}
	}
	c.sid = res.Session
	return nil
}

// UbusCall: ubus call con HTTP + login cacheado; un solo re-login si la
// sesión caducó; fallback a SSH si HTTP no está disponible.
func (c *OpenWrtClient) UbusCall(object, method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	payload, err := func() (json.RawMessage, error) {
		if c.sid == "" {
			if err := c.ubusLogin(); err != nil {
				return nil, err
			}
		}
		payload, err := c.ubusHTTPRpc(c.sid, object, method, params)
		if err != nil {
			// Sesión caducada → un solo re-login
			c.sid = ""
			if lerr := c.ubusLogin(); lerr != nil {
				return nil, lerr
			}
			return c.ubusHTTPRpc(c.sid, object, method, params)
		}
		return payload, nil
	}()
	if err == nil {
		return payload, nil
	}
	// Fallback SSH: ubus call <object> <method> '<json>'
	paramsJSON, _ := json.Marshal(params)
	esc := strings.ReplaceAll(string(paramsJSON), `'`, `'\''`)
	out, serr := c.pool.Run(c.Host, fmt.Sprintf("ubus call %s %s '%s'", object, method, esc), 0)
	if serr != nil {
		return nil, serr
	}
	if !json.Valid([]byte(out)) {
		return nil, &ubusRPCError{"ubus ssh: respuesta no JSON"}
	}
	return json.RawMessage(out), nil
}

// ---------------------------------------------------------------------------
// Sonda de sistema
// ---------------------------------------------------------------------------

// BoardInfo: ubus system board (metadatos estáticos; lo cachea el live).
type BoardInfo struct {
	Model    string `json:"model"`
	Hostname string `json:"hostname"`
	System   string `json:"system"`
	Release  struct {
		Version     string `json:"version"`
		Description string `json:"description"`
	} `json:"release"`
}

// GetBoard: modelo, release, kernel → metadatos estáticos del router.
func (c *OpenWrtClient) GetBoard() (*BoardInfo, error) {
	raw, err := c.UbusCall("system", "board", nil)
	if err != nil {
		return nil, err
	}
	var b BoardInfo
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

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

// GetSysInfo: uptime, memoria, load.
func (c *OpenWrtClient) GetSysInfo() (*SysInfo, error) {
	raw, err := c.UbusCall("system", "info", nil)
	if err != nil {
		return nil, err
	}
	var s SysInfo
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ---------------------------------------------------------------------------
// CPU / temperatura / netdev / ping (SSH; parseo literal del JS)
// ---------------------------------------------------------------------------

// GetCPUPercent: CPU % por delta de /proc/stat. Primera muestra: nil.
func (c *OpenWrtClient) GetCPUPercent() (*int, error) {
	out, err := c.pool.Run(c.Host, "grep '^cpu ' /proc/stat", 0)
	if err != nil {
		return nil, err
	}
	cur, err := parseProcStat(out)
	if err != nil {
		return nil, err
	}
	prev := c.lastStat
	c.lastStat = &cur
	if prev == nil {
		return nil, nil
	}
	dTotal := cur.total - prev.total
	dIdle := cur.idleAll - prev.idleAll
	if dTotal <= 0 {
		return nil, nil
	}
	v := int(float64(dTotal-dIdle)/float64(dTotal)*100 + 0.5)
	return &v, nil
}

// parseProcStat parsea "cpu  user nice system idle iowait irq softirq ...".
func parseProcStat(out string) (cpuSample, error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuSample{}, fmt.Errorf("/proc/stat inesperado: %q", out)
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
	idleAll := get(3) + get(4)                          // idle + iowait
	nonIdle := get(0) + get(1) + get(2) + get(5) + get(6) // user+nice+system+irq+softirq
	return cpuSample{total: idleAll + nonIdle, idleAll: idleAll}, nil
}

// GetTempC: °C del primer thermal zone disponible (nil si no hay).
func (c *OpenWrtClient) GetTempC() (*int, error) {
	out, err := c.pool.Run(c.Host,
		`for f in /sys/class/thermal/thermal_zone*/temp; do [ -r "$f" ] && cat "$f" && break; done`, 0)
	if err != nil {
		return nil, err
	}
	milli, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return nil, nil
	}
	v := milli / 1000
	if milli%1000 >= 500 {
		v++
	}
	return &v, nil
}

// NetDevBps es el resultado del delta de /proc/net/dev.
type NetDevBps struct {
	RxBps *float64
	TxBps *float64
}

var netdevSkipRe = regexp.MustCompile(`^(lo|br-|ifb|wg|tun|tap)`)

// GetNetDevBps: tráfico agregado por delta (excluye lo, bridges, ifb, wg,
// tun/tap para no duplicar contadores). Primera muestra: null/null.
func (c *OpenWrtClient) GetNetDevBps() (*NetDevBps, error) {
	out, err := c.pool.Run(c.Host, "cat /proc/net/dev | tail -n +3", 0)
	if err != nil {
		return nil, err
	}
	rx, tx := parseNetDev(out)
	now := time.Now()
	prev := c.lastNetDev
	c.lastNetDev = &netSample{rx: rx, tx: tx, at: now}
	res := &NetDevBps{}
	if prev == nil {
		return res, nil
	}
	dt := now.Sub(prev.at).Seconds()
	if dt <= 0 {
		return res, nil
	}
	rxBps := round0(max0((rx - prev.rx) * 8 / dt))
	txBps := round0(max0((tx - prev.tx) * 8 / dt))
	res.RxBps = &rxBps
	res.TxBps = &txBps
	return res, nil
}

// parseNetDev suma rx/tx de las interfaces físicas.
func parseNetDev(out string) (rx, tx float64) {
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

var pingLossRe = regexp.MustCompile(`(\d+(?:\.\d+)?)% packet loss`)
var pingRTTRe = regexp.MustCompile(`= [\d.]+/([\d.]+)/[\d.]+/[\d.]+ ms`)

// GetWanLatency: ping a internet (solo gateway) → latencia avg + pérdida.
func (c *OpenWrtClient) GetWanLatency(target string) (latency *float64, loss *float64, err error) {
	if target == "" {
		target = "1.1.1.1"
	}
	out, err := c.pool.Run(c.Host, fmt.Sprintf("ping -c 3 -W 2 %s 2>/dev/null | tail -2", target), 0)
	if err != nil {
		return nil, nil, err
	}
	if m := pingRTTRe.FindStringSubmatch(out); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		v = float64(int(v + 0.5))
		latency = &v
	}
	if m := pingLossRe.FindStringSubmatch(out); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		loss = &v
	}
	return latency, loss, nil
}

// GetGatewayLatency: ping corto al gateway desde un AP (avg, redondeado).
func (c *OpenWrtClient) GetGatewayLatency(gatewayHost string) (*float64, error) {
	out, err := c.pool.Run(c.Host, fmt.Sprintf("ping -c 2 -W 2 %s 2>/dev/null | tail -1", gatewayHost), 0)
	if err != nil {
		return nil, err
	}
	if m := pingRTTRe.FindStringSubmatch(out); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		v = float64(int(v + 0.5))
		return &v, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Clientes (DHCP + wireless)
// ---------------------------------------------------------------------------

// DhcpLease es {mac, ip, hostname} (mac en mayúsculas).
type DhcpLease struct {
	MAC      string
	IP       string
	Hostname string
}

// GetDhcpLeases: ubus dhcp ipv4leases; fallback /tmp/dhcp.leases.
func (c *OpenWrtClient) GetDhcpLeases() []DhcpLease {
	type leaseJSON struct {
		MAC      string `json:"mac"`
		IPAddr   string `json:"ip-address"`
		IP       string `json:"ip"`
		Hostname string `json:"hostname"`
	}
	raw, err := c.UbusCall("dhcp", "ipv4leases", nil)
	if err == nil {
		var data struct {
			Lease  []leaseJSON `json:"lease"`
			Leases []leaseJSON `json:"leases"`
		}
		if json.Unmarshal(raw, &data) == nil {
			leases := data.Lease
			if leases == nil {
				leases = data.Leases
			}
			out := make([]DhcpLease, 0, len(leases))
			for _, l := range leases {
				ip := l.IPAddr
				if ip == "" {
					ip = l.IP
				}
				out = append(out, DhcpLease{MAC: strings.ToUpper(l.MAC), IP: ip, Hostname: l.Hostname})
			}
			return out
		}
	}
	out, serr := c.pool.Run(c.Host, "cat /tmp/dhcp.leases 2>/dev/null || true", 0)
	if serr != nil {
		return []DhcpLease{}
	}
	return parseDhcpLeasesFile(out)
}

// parseDhcpLeasesFile parsea /tmp/dhcp.leases:
// <expiry> <mac> <ip> <hostname> <clientid>
func parseDhcpLeasesFile(out string) []DhcpLease {
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

// WirelessClient es {signalDbm, band} por MAC.
type WirelessClient struct {
	SignalDbm int
	Band      string
}

// GetWirelessClients: iwinfo assoclist por radio → mapa MAC → cliente.
func (c *OpenWrtClient) GetWirelessClients() map[string]WirelessClient {
	out, err := c.pool.Run(c.Host,
		`for i in $(iwinfo 2>/dev/null | awk '/^[a-z]/ {print $1}'); do `+
			`freq=$(iwinfo "$i" info 2>/dev/null | sed -n 's/.*Channel: [0-9]* (\([0-9.]*\) GHz).*/\1/p' | head -1); `+
			`iwinfo "$i" assoclist 2>/dev/null | sed -n 's/^\([0-9A-Fa-f:]\{17\}\) *\(-[0-9]*\).*/\1 \2/p' | while read mac sig; do `+
			`echo "$mac $sig $freq"; done; done`, iwinfoTimeout)
	if err != nil {
		return map[string]WirelessClient{}
	}
	return parseWirelessClients(out)
}

// parseWirelessClients parsea líneas "<mac> <sig> <freq>" del bucle iwinfo.
func parseWirelessClients(out string) map[string]WirelessClient {
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

// GetWirelessUplink: true si el router tiene uplink inalámbrico (alguna
// interfaz STA activa/asociada en `ubus call network.wireless status`).
// Error si el router no soporta la llamada (sin wifi → el caller omite el
// campo backhaul, nunca rompe el sondeo).
func (c *OpenWrtClient) GetWirelessUplink() (bool, error) {
	raw, err := c.UbusCall("network.wireless", "status", nil)
	if err != nil {
		return false, err
	}
	return parseWirelessUplink(raw)
}

// parseWirelessUplink: alguna interfaz con config.mode "sta" activa (radio
// up e ifname asignado = asociada). Tolerante con radios/entradas raras.
func parseWirelessUplink(raw json.RawMessage) (bool, error) {
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

// PortState es {name, up, speed} de una interfaz (/sys operstate+speed).
type PortState struct {
	Name  string
	Up    bool
	Speed string // "1 Gbps" | "100 Mbps" | "—"
}

// GetPortStates: estado de interfaces desde /sys (SSH).
func (c *OpenWrtClient) GetPortStates() []PortState {
	out, err := c.pool.Run(c.Host,
		`for d in /sys/class/net/*; do i=$(basename "$d"); `+
			`echo "$i $(cat $d/operstate 2>/dev/null) $(cat $d/speed 2>/dev/null || echo -1)"; done`, 0)
	if err != nil {
		return []PortState{}
	}
	return parsePortStates(out)
}

// parsePortStates parsea líneas "<name> <operstate> <speed>".
func parsePortStates(out string) []PortState {
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

// PortLayout es una boca del layout canónico (/etc/board.json).
type PortLayout struct {
	ID    string
	Name  string
	Label string
	Role  string // "lan" | "wan"
}

// GetPortLayout: layout canónico desde /etc/board.json (estático; lo cachea
// el live). WAN normalizado a id 'wan'.
func (c *OpenWrtClient) GetPortLayout() ([]PortLayout, error) {
	out, err := c.pool.Run(c.Host, "cat /etc/board.json 2>/dev/null", 0)
	if err != nil {
		return nil, err
	}
	return parsePortLayout(out)
}

// parsePortLayout parsea board.json → [{id, name, label, role}].
func parsePortLayout(out string) ([]PortLayout, error) {
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

// GetEthPorts: layout + estado /sys; AP en bridge re-etiqueta wan→LAN N+1;
// fallback heurístico sin config. Devuelve []EthPort listo para el detalle.
func (c *OpenWrtClient) GetEthPorts(layout []PortLayout) []EthPort {
	states := c.GetPortStates()
	byName := map[string]PortState{}
	for _, p := range states {
		byName[p.Name] = p
	}
	if len(layout) > 0 {
		// Miembros del bridge: si "wan" está en br-lan, este router es un AP
		brif, _ := c.pool.Run(c.Host, "ls /sys/class/net/br-lan/brif/ 2>/dev/null", 0)
		members := map[string]bool{}
		for _, l := range strings.Split(strings.TrimSpace(brif), "\n") {
			if l != "" {
				members[l] = true
			}
		}
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
			if p.Role == "wan" && members[p.Name] {
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
	sort.Slice(lans, func(i, j int) bool { return naturalLess(lans[i].Name, lans[j].Name) })
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

// naturalLess ordena lan2 < lan10 (localeCompare numeric del JS).
func naturalLess(a, b string) bool {
	re := regexp.MustCompile(`^(.*?)(\d+)$`)
	ma, mb := re.FindStringSubmatch(a), re.FindStringSubmatch(b)
	if ma != nil && mb != nil && ma[1] == mb[1] {
		na, _ := strconv.Atoi(ma[2])
		nb, _ := strconv.Atoi(mb[2])
		return na < nb
	}
	return a < b
}

// GetBridgeFdb: MAC aprendida → puerto (brctl showmacs + port_no).
func (c *OpenWrtClient) GetBridgeFdb() map[string]string {
	out, err := c.pool.Run(c.Host,
		`echo "==PORTS=="; for d in /sys/class/net/br-lan/brif/*; do [ -r "$d/port_no" ] && echo "$(cat $d/port_no) $(basename $d)"; done; `+
			`echo "==MACS=="; brctl showmacs br-lan 2>/dev/null | awk 'NR>1 && $3=="no" {print $1, $2}'`, 0)
	if err != nil {
		return map[string]string{}
	}
	return parseBridgeFdb(out)
}

var fdbPortRe = regexp.MustCompile(`^(lan\d+|wan)$`)

// parseBridgeFdb parsea la salida ==PORTS==/==MACS== del JS.
func parseBridgeFdb(out string) map[string]string {
	m := map[string]string{}
	portNames := map[string]string{}
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
		case 'm':
			no, mac := p[0], p[1]
			if port, ok := portNames[no]; ok && mac != "" && fdbPortRe.MatchString(port) {
				m[strings.ToUpper(mac)] = port
			}
		}
	}
	return m
}

// GetBridgeMac: MAC del bridge br-lan (para reconocer uplinks).
func (c *OpenWrtClient) GetBridgeMac() string {
	out, err := c.pool.Run(c.Host, "cat /sys/class/net/br-lan/address 2>/dev/null", 0)
	if err != nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(out))
}

// ---------------------------------------------------------------------------
// Radios WiFi
// ---------------------------------------------------------------------------

// GetRadios: radios activas agregadas por banda (iwinfo info + assoclist).
func (c *OpenWrtClient) GetRadios() []Radio {
	out, err := c.pool.Run(c.Host,
		`for i in $(iwinfo 2>/dev/null | awk '/^[a-z]/ {print $1}'); do `+
			`info=$(iwinfo "$i" info 2>/dev/null) || continue; `+
			`echo "$info" | grep -q ESSID || continue; `+
			`freq=$(echo "$info" | sed -n 's/.*Channel: [0-9]* (\([0-9.]*\) GHz).*/\1/p' | head -1); `+
			`ch=$(echo "$info" | sed -n 's/.*Channel: \([0-9][0-9]*\).*/\1/p' | head -1); `+
			`ht=$(echo "$info" | sed -n 's/.*HT [Mm]ode: \([A-Za-z0-9]*\).*/\1/p' | head -1); `+
			`tx=$(echo "$info" | sed -n 's/.*Tx-Power: \([0-9]*\).*/\1/p' | head -1); `+
			`n=$(iwinfo "$i" assoclist 2>/dev/null | grep -c '^[0-9A-Fa-f:]'); `+
			`echo "$freq|$ch|$ht|$tx|$n"; done`, iwinfoTimeout)
	if err != nil {
		return []Radio{}
	}
	return parseRadios(out)
}

var nonDigitRe = regexp.MustCompile(`\D`)

// parseRadios: líneas "freq|ch|ht|tx|n" agregadas por banda (suma clientes).
func parseRadios(out string) []Radio {
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
