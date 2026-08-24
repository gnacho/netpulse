// openwrt.go — Cliente live para routers OpenWrt / GL.iNet (port de
// src/adapters/openwrt.js, SPEC §7.2):
//
//  1. ubus JSON-RPC sobre HTTP (POST http://<host>/ubus, session login con
//     token cacheado y un solo re-login ante sesión caducada).
//  2. Fallback SSH con pool persistente (sshpool.go; equivalente al
//     ControlMaster del JS).
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
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
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

	// mu protege el estado mutable del cliente (sid, lastStat, lastNetDev,
	// lldpDownUntil): los handlers HTTP pueden sondear (p.ej. GetSurvey) en
	// paralelo con el poller.
	mu sync.Mutex

	sid        string     // sesión ubus HTTP cacheada
	lastStat   *cpuSample // /proc/stat previo
	lastNetDev *netSample // /proc/net/dev previo
	// lldpDownUntil: indisponibilidad de lldpd cacheada (≥5 min) para no
	// martillear con un comando que no existe. Acceso serializado por mu.
	lldpDownUntil time.Time
}

type cpuSample struct{ total, idleAll float64 }
type netSample struct {
	rx, tx float64
	at     time.Time
}

// Aliases a los shapes compartidos de agent/probe: el agente nativo parsea
// LOCALMENTE con el mismo package (misma fuente de verdad) y empuja esos
// shapes al endpoint de ingesta (SPEC-AGENTE-PILOTO §1-2).
type (
	SysInfo        = probe.SysInfo
	BoardInfo      = probe.BoardInfo
	DhcpLease      = probe.DhcpLease
	WirelessClient = probe.WirelessClient
	PortState      = probe.PortState
	PortLayout     = probe.PortLayout
)

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
	out, err := c.pool.Run(c.Host, probe.CmdProcStat, 0)
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
	return probe.CPUPercent(probe.CPUSample{Total: prev.total, IdleAll: prev.idleAll},
		probe.CPUSample{Total: cur.total, IdleAll: cur.idleAll}), nil
}

// parseProcStat parsea "cpu  user nice system idle iowait irq softirq ..."
// (delega en el parseo compartido con el agente nativo).
func parseProcStat(out string) (cpuSample, error) {
	s, err := probe.ParseProcStat(out)
	return cpuSample{total: s.Total, idleAll: s.IdleAll}, err
}

// GetTempC: °C del primer thermal zone disponible (nil si no hay).
func (c *OpenWrtClient) GetTempC() (*int, error) {
	out, err := c.pool.Run(c.Host, probe.CmdTemp, 0)
	if err != nil {
		return nil, err
	}
	return probe.ParseTempC(out), nil
}

// NetDevBps es el resultado del delta de /proc/net/dev.
type NetDevBps struct {
	RxBps *float64
	TxBps *float64
}

// GetNetDevBps: tráfico agregado por delta (excluye lo, bridges, ifb, wg,
// tun/tap para no duplicar contadores). Primera muestra: null/null.
func (c *OpenWrtClient) GetNetDevBps() (*NetDevBps, error) {
	out, err := c.pool.Run(c.Host, probe.CmdNetDev, 0)
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
	res.RxBps, res.TxBps = probe.NetDevBps(prev.rx, prev.tx, rx, tx, now.Sub(prev.at).Seconds())
	return res, nil
}

// parseNetDev suma rx/tx de las interfaces físicas (compartido con el agente).
func parseNetDev(out string) (rx, tx float64) {
	return probe.ParseNetDev(out)
}

// GetWanLatency: ping a internet (solo gateway) → latencia avg + pérdida.
func (c *OpenWrtClient) GetWanLatency(target string) (latency *float64, loss *float64, err error) {
	if target == "" {
		target = "1.1.1.1"
	}
	out, err := c.pool.Run(c.Host, fmt.Sprintf(probe.CmdPingWan, target), 0)
	if err != nil {
		return nil, nil, err
	}
	latency, loss = probe.ParsePingSummary(out)
	return latency, loss, nil
}

// GetGatewayLatency: ping corto al gateway desde un AP (avg, redondeado).
func (c *OpenWrtClient) GetGatewayLatency(gatewayHost string) (*float64, error) {
	out, err := c.pool.Run(c.Host, fmt.Sprintf(probe.CmdPingGateway, gatewayHost), 0)
	if err != nil {
		return nil, err
	}
	latency, _ := probe.ParsePingSummary(out)
	return latency, nil
}

// ---------------------------------------------------------------------------
// Clientes (DHCP + wireless)
// ---------------------------------------------------------------------------

// GetDhcpLeases: ubus dhcp ipv4leases; fallback /tmp/dhcp.leases.
func (c *OpenWrtClient) GetDhcpLeases() []DhcpLease {
	raw, err := c.UbusCall("dhcp", "ipv4leases", nil)
	if err == nil {
		if leases, perr := probe.ParseDhcpUbus(raw); perr == nil {
			return leases
		}
	}
	out, serr := c.pool.Run(c.Host, probe.CmdDhcpFile, 0)
	if serr != nil {
		return []DhcpLease{}
	}
	return parseDhcpLeasesFile(out)
}

// GetGlClients: base de clientes del firmware GL.iNet (`ubus call gl-clients
// list`). SUPERSET de dhcp.leases: incluye equipos con IP estática o sin
// lease que el dnsmasq del Flint2 no lista. En routers sin el objeto ubus
// gl-clients devuelve vacío (la salida del comando es vacía) — el caller lo
// usa solo para enriquecer IPs de dispositivos ya conocidos, nunca para
// crearlos (issue #5 bug 1).
func (c *OpenWrtClient) GetGlClients() []DhcpLease {
	out, err := c.pool.Run(c.Host, probe.CmdGlClients, 0)
	if err != nil || strings.TrimSpace(out) == "" {
		return []DhcpLease{}
	}
	leases, perr := probe.ParseGlClients([]byte(out))
	if perr != nil {
		return []DhcpLease{}
	}
	return leases
}

// parseDhcpLeasesFile parsea /tmp/dhcp.leases:
// <expiry> <mac> <ip> <hostname> <clientid>
func parseDhcpLeasesFile(out string) []DhcpLease {
	return probe.ParseDhcpLeasesFile(out)
}

// GetWirelessClients: iwinfo assoclist por radio → mapa MAC → cliente.
func (c *OpenWrtClient) GetWirelessClients() map[string]WirelessClient {
	out, err := c.pool.Run(c.Host, probe.CmdIwinfoAssoc, iwinfoTimeout)
	if err != nil {
		return map[string]WirelessClient{}
	}
	return parseWirelessClients(out)
}

// parseWirelessClients parsea líneas "<mac> <sig> <freq>" del bucle iwinfo.
func parseWirelessClients(out string) map[string]WirelessClient {
	return probe.ParseWirelessClients(out)
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
	return probe.ParseWirelessUplink(raw)
}

// ---------------------------------------------------------------------------
// Puertos
// ---------------------------------------------------------------------------

// GetPortStates: estado de interfaces desde /sys (SSH).
func (c *OpenWrtClient) GetPortStates() []PortState {
	out, err := c.pool.Run(c.Host, probe.CmdPortStates, 0)
	if err != nil {
		return []PortState{}
	}
	return parsePortStates(out)
}

// parsePortStates parsea líneas "<name> <operstate> <speed>".
func parsePortStates(out string) []PortState {
	return probe.ParsePortStates(out)
}

// GetPortLayout: layout canónico desde /etc/board.json (estático; lo cachea
// el live). WAN normalizado a id 'wan'.
func (c *OpenWrtClient) GetPortLayout() ([]PortLayout, error) {
	out, err := c.pool.Run(c.Host, probe.CmdBoardJSON, 0)
	if err != nil {
		return nil, err
	}
	return parsePortLayout(out)
}

// parsePortLayout parsea board.json → [{id, name, label, role}].
func parsePortLayout(out string) ([]PortLayout, error) {
	return probe.ParsePortLayout(out)
}

// GetEthPorts: layout + estado /sys; AP en bridge re-etiqueta wan→LAN N+1;
// fallback heurístico sin config. Devuelve []EthPort listo para el detalle.
func (c *OpenWrtClient) GetEthPorts(layout []PortLayout) []EthPort {
	states := c.GetPortStates()
	members := map[string]bool{}
	if len(layout) > 0 {
		// Miembros del bridge: si "wan" está en br-lan, este router es un AP
		brif, _ := c.pool.Run(c.Host, probe.CmdBrifMembers, 0)
		for _, l := range strings.Split(strings.TrimSpace(brif), "\n") {
			if l != "" {
				members[l] = true
			}
		}
	}
	return ethPortsToAdapter(probe.BuildEthPorts(layout, states, members))
}

// ethPortsToAdapter convierte el shape compartido al EthPort del contrato.
func ethPortsToAdapter(in []probe.EthPort) []EthPort {
	out := make([]EthPort, 0, len(in))
	for _, p := range in {
		ep := EthPort{ID: p.ID, Label: p.Label, Up: p.Up}
		if p.Up {
			ep.Speed = p.Speed
		}
		out = append(out, ep)
	}
	return out
}

// naturalLess ordena lan2 < lan10 (localeCompare numeric del JS).
func naturalLess(a, b string) bool {
	return probe.NaturalLess(a, b)
}

// GetBridgeFdb: MAC aprendida → puerto (brctl showmacs + port_no).
func (c *OpenWrtClient) GetBridgeFdb() map[string]string {
	out, err := c.pool.Run(c.Host, probe.CmdBridgeFDB, 0)
	if err != nil {
		return map[string]string{}
	}
	return parseBridgeFdb(out)
}

// parseBridgeFdb parsea la salida ==PORTS==/==MACS== del JS.
func parseBridgeFdb(out string) map[string]string {
	return probe.ParseBridgeFdb(out)
}

// GetBridgeMac: MAC del bridge br-lan (para reconocer uplinks).
func (c *OpenWrtClient) GetBridgeMac() string {
	out, err := c.pool.Run(c.Host, probe.CmdBridgeMAC, 0)
	if err != nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(out))
}

// GetLuCILabels: etiquetas de puertos/VLANs de LuCI (issue #258), si el
// router las define en /etc/config/luci. Best-effort: fichero sin secciones
// → nil.
func (c *OpenWrtClient) GetLuCILabels() *probe.LuCILabels {
	out, err := c.pool.Run(c.Host, probe.CmdLuCILabels, 0)
	if err != nil {
		return nil
	}
	return probe.ParseLuCILabels(out)
}

// ---------------------------------------------------------------------------
// Radios WiFi
// ---------------------------------------------------------------------------

// GetRadios: radios activas agregadas por banda (iwinfo info + assoclist).
func (c *OpenWrtClient) GetRadios() []Radio {
	out, err := c.pool.Run(c.Host, probe.CmdRadios, iwinfoTimeout)
	if err != nil {
		return []Radio{}
	}
	return parseRadios(out)
}

// parseRadios: líneas "freq|ch|ht|tx|n" agregadas por banda (suma clientes).
func parseRadios(out string) []Radio {
	return radiosToAdapter(probe.ParseRadios(out))
}

// radiosToAdapter convierte el shape compartido al Radio del contrato.
func radiosToAdapter(in []probe.Radio) []Radio {
	out := make([]Radio, 0, len(in))
	for _, r := range in {
		out = append(out, Radio{Name: r.Name, Channel: r.Channel,
			WidthMhz: r.WidthMhz, PowerDbm: r.PowerDbm, Clients: r.Clients})
	}
	return out
}
