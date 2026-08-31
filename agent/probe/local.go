// local.go — prober local del agente: ejecuta EN EL ROUTER los mismos
// comandos que el servidor lanza por SSH (constantes Cmd* de probe.go) y
// construye el Payload con los shapes parseados. Cada sonda es best-effort:
// si un comando no existe (ubus, iwinfo, brctl...) la sección queda ausente
// y el servidor conserva el último dato bueno (anti-parpadeo).
package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Runner ejecuta un comando de shell local y devuelve su stdout.
// timeout 0 = default corto de sonda.
type Runner interface {
	Run(ctx context.Context, cmd string, timeout time.Duration) (string, error)
}

// ShellRunner ejecuta con /bin/sh -c (BusyBox ash en OpenWrt).
type ShellRunner struct {
	DefaultTimeout time.Duration
}

// Run implementa Runner.
func (s ShellRunner) Run(ctx context.Context, cmd string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = s.DefaultTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(cctx, "/bin/sh", "-c", cmd).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Options configura las sondas opcionales del prober.
type Options struct {
	// WanPingTarget: si no está vacío, ping WAN (latencia + pérdida) — gateway.
	WanPingTarget string
	// GwPingTarget: si no está vacío, ping corto al gateway (latencia) — AP.
	GwPingTarget string
}

// Prober sondea el equipo local y construye payloads. Mantiene el estado de
// deltas (/proc/stat y /proc/net/dev) entre muestras — es el único estado
// del agente y vive solo en RAM.
type Prober struct {
	run  Runner
	opts Options

	lastStat   *CPUSample
	lastNetRx  float64
	lastNetTx  float64
	lastNetAt  time.Time
	hasLastNet bool
	// lastNetRaw conserva el último output de /proc/net/dev de probeSystem
	// para derivar la sección NetIf (contadores por iface, #305) sin un
	// segundo cat.
	lastNetRaw string
	// netIfUpdated indica si el último ciclo de probeSystem consiguió
	// refrescar /proc/net/dev. Build solo incluye NetIf cuando es true,
	// evitando enviar contadores repetidos si CmdNetDev falla o tarda.
	netIfUpdated bool

	// radiosCache (#368): el resumen de radios (canal/htmode/txpower) cambia
	// tan poco que no merece iwinfo en cada ciclo NI en el path de eventos:
	// se refresca cada radiosTTL en el sondeo completo y el resto de las
	// llamadas reutilizan la última.
	radiosMu    sync.Mutex
	radiosCache []Radio
	radiosAt    time.Time
}

const radiosTTL = 5 * time.Minute

// NewProber crea el prober con el runner dado.
func NewProber(run Runner, opts Options) *Prober {
	return &Prober{run: run, opts: opts}
}

// runBest es best-effort: error → "" (la sección queda ausente).
func (p *Prober) runBest(ctx context.Context, cmd string, timeout time.Duration) string {
	out, err := p.run.Run(ctx, cmd, timeout)
	if err != nil {
		return ""
	}
	return out
}

// Build ejecuta una ronda de sondas y devuelve el payload listo para empujar.
// router/version los rellena el caller (main) o van aquí por comodidad.
func (p *Prober) Build(ctx context.Context, router, version string) *Payload {
	pl := &Payload{
		Router:  router,
		Ts:      time.Now().Unix(),
		Version: version,
	}
	pl.Data.System = p.probeSystem(ctx)
	pl.Data.Wireless = p.probeWireless(ctx, true)
	pl.Data.DHCP = p.probeDHCP(ctx)
	pl.Data.FDB = p.probeFDB(ctx)
	pl.Data.Arp = p.probeArp(ctx)
	pl.Data.Dawn = p.probeDawn(ctx)
	pl.Data.Usteer = p.probeUsteer(ctx)
	pl.Data.LuCI = p.probeLuCI(ctx)
	// Discovery (#338): mDNS services + randomized MAC detection.
	pl.Data.Discovery = p.probeDiscovery(ctx, pl.Data.Wireless)
	// NetIf (#305): contadores por iface desde el MISMO /proc/net/dev que
	// leyó probeSystem (sin segundo cat). Solo se incluyen si el ciclo actual
	// refrescó la muestra; si no, el payload se envía sin NetIf para que el
	// servidor no inserte deltas 0 con contadores repetidos (#408).
	if p.netIfUpdated {
		pl.Data.NetIf = ParseNetDevIfaces(p.lastNetRaw)
		p.netIfUpdated = false
	}
	return pl
}

// BuildWireless es un sondeo rápido de solo wireless + DHCP (Fase 7.1:
// disparado por eventos ubus assoc/disassoc). El servidor conserva el último
// dato bueno de system y FDB (parpadeo suprimido por secciones nil).
func (p *Prober) BuildWireless(ctx context.Context, router, version string) *Payload {
	pl := &Payload{
		Router:  router,
		Ts:      time.Now().Unix(),
		Version: version,
	}
	pl.Data.Wireless = p.probeWireless(ctx, false)
	pl.Data.DHCP = p.probeDHCP(ctx)
	pl.Data.Arp = p.probeArp(ctx)
	return pl
}

// probeSystem: sysinfo + board + cpu/temp + netdev + pings + backhaul + MAC
// del bridge. Devuelve nil solo si TODAS las sondas fallaron (equipo sin
// ubus y sin /proc — no pasa en OpenWrt; cubre al fake runner de tests).
func (p *Prober) probeSystem(ctx context.Context) *SystemData {
	sd := &SystemData{}
	any := false

	if out := p.runBest(ctx, CmdUbusSystemInfo, 0); out != "" {
		var si SysInfo
		if err := json.Unmarshal([]byte(out), &si); err == nil {
			sd.SysInfo = &si
			any = true
		}
	}
	if out := p.runBest(ctx, CmdUbusSystemBoard, 0); out != "" {
		var b BoardInfo
		if err := json.Unmarshal([]byte(out), &b); err == nil {
			sd.Board = &b
			any = true
		}
	}

	if out := p.runBest(ctx, CmdProcStat, 0); out != "" {
		if cur, err := ParseProcStat(out); err == nil {
			if p.lastStat != nil {
				sd.CPU = CPUPercent(*p.lastStat, cur)
			}
			c := cur
			p.lastStat = &c
			any = true
		}
	}
	if out := p.runBest(ctx, CmdTemp, 0); out != "" {
		sd.Temp = ParseTempC(out)
		any = true
	}
	if out := p.runBest(ctx, CmdNetDev, 0); out != "" {
		rx, tx := ParseNetDev(out)
		now := time.Now()
		if p.hasLastNet {
			sd.RxBps, sd.TxBps = NetDevBps(p.lastNetRx, p.lastNetTx, rx, tx, now.Sub(p.lastNetAt).Seconds())
		}
		p.lastNetRx, p.lastNetTx, p.lastNetAt, p.hasLastNet = rx, tx, now, true
		p.lastNetRaw = out
		p.netIfUpdated = true
		any = true
	}

	if p.opts.WanPingTarget != "" {
		out := p.runBest(ctx, fmt.Sprintf(CmdPingWan, p.opts.WanPingTarget), 10*time.Second)
		sd.LatencyMs, sd.LossPct = ParsePingSummary(out)
		any = true
	} else if p.opts.GwPingTarget != "" {
		out := p.runBest(ctx, fmt.Sprintf(CmdPingGateway, p.opts.GwPingTarget), 10*time.Second)
		sd.LatencyMs, _ = ParsePingSummary(out)
		any = true
	}

	if out := p.runBest(ctx, CmdUbusWireless, 0); out != "" {
		if wifi, err := ParseWirelessUplink([]byte(out)); err == nil {
			sd.Backhaul = "cable"
			if wifi {
				sd.Backhaul = "wifi"
			}
			any = true
		}
	}
	if out := p.runBest(ctx, CmdBridgeMAC, 0); out != "" {
		sd.BridgeMAC = strings.ToUpper(strings.TrimSpace(out))
		any = true
	}

	if !any {
		return nil
	}
	return sd
}

// probeWireless: clientes asociados + radios por banda (#368: ubus primero,
// iwinfo combinado en UNA pasada como fallback, radios cacheadas radiosTTL).
// full=false (path de eventos nl80211) evita iwinfo por completo: ubus para
// clientes y radios de caché. nil si no hay fuente wireless.
func (p *Prober) probeWireless(ctx context.Context, full bool) *WirelessData {
	wd := &WirelessData{}
	any := false

	// Clientes: ubus hostapd get_clients (barato, sin ucode).
	if out := p.runBest(ctx, CmdHostapdClients, 5*time.Second); out != "" {
		wd.Clients = ParseHostapdClients(out)
		if len(wd.Clients) > 0 {
			any = true
		}
	}
	// Fallback (o APs sin ubus hostapd): iwinfo en UNA pasada por interfaz,
	// que además produce el resumen de radios.
	combined := ""
	if !any {
		combined = p.runBest(ctx, CmdWirelessCombined, 8*time.Second)
		if combined != "" {
			clients, radios := ParseWirelessCombined(combined)
			wd.Clients = clients
			if len(clients) > 0 {
				any = true
			}
			p.radiosMu.Lock()
			if len(radios) > 0 {
				p.radiosCache, p.radiosAt = radios, time.Now()
			} else if time.Since(p.radiosAt) < radiosTTL {
				radios = p.radiosCache
			}
			p.radiosMu.Unlock()
			wd.Radios = radios
		}
	}
	// Último recurso (#368): el par de comandos clásico (equipos donde el
	// combinado no produjo clientes p. ej. iwinfo sin ESSID en la interfaz).
	if !any {
		if out := p.runBest(ctx, CmdIwinfoAssoc, 8*time.Second); out != "" {
			wd.Clients = ParseWirelessClients(out)
			any = len(wd.Clients) > 0
		}
	}

	// Radios: solo en el sondeo completo (canal/htmode/txpower casi nunca
	// cambian); el path de eventos reutiliza la caché.
	if full {
		if combined != "" && wd.Radios == nil {
			wd.Radios = []Radio{}
		}
		if combined == "" {
			if out := p.runBest(ctx, CmdRadios, 8*time.Second); out != "" {
				p.radiosMu.Lock()
				p.radiosCache, p.radiosAt = ParseRadios(out), time.Now()
				wd.Radios = p.radiosCache
				p.radiosMu.Unlock()
			}
		}
	} else {
		p.radiosMu.Lock()
		cached := p.radiosCache
		p.radiosMu.Unlock()
		if len(cached) > 0 {
			wd.Radios = cached
			if len(wd.Clients) > 0 {
				any = true
			}
		}
	}

	if !any {
		return nil
	}
	if wd.Clients == nil {
		wd.Clients = map[string]WirelessClient{}
	}
	if wd.Radios == nil {
		wd.Radios = []Radio{}
	}
	return wd
}

// probeDHCP: ubus dhcp ipv4leases con fallback a /tmp/dhcp.leases, más
// gl-clients (GL.iNet) como superset para resolver IPs sin lease
// (issue #5 bug 1 — el gateway Flint2 llega por esta ruta de agente).
func (p *Prober) probeDHCP(ctx context.Context) *DHCPData {
	var dd *DHCPData
	if out := p.runBest(ctx, CmdDhcpUbus, 0); out != "" {
		if leases, err := ParseDhcpUbus([]byte(out)); err == nil {
			dd = &DHCPData{Leases: leases}
		}
	}
	if dd == nil {
		out := p.runBest(ctx, CmdDhcpFile, 0)
		if out == "" {
			return nil
		}
		dd = &DHCPData{Leases: ParseDhcpLeasesFile(out)}
	}
	if out := p.runBest(ctx, CmdGlClients, 0); out != "" {
		if gl, err := ParseGlClients([]byte(out)); err == nil {
			dd.GlClients = gl
		}
	}
	return dd
}

// probeFDB: MACs aprendidas (brctl) + puertos ethernet (layout + /sys).
func (p *Prober) probeFDB(ctx context.Context) *FDBData {
	fd := &FDBData{}
	any := false
	if out := p.runBest(ctx, CmdBridgeFDB, 0); out != "" {
		fd.MACs = ParseBridgeFdb(out)
		any = true
	}
	statesOut := p.runBest(ctx, CmdPortStates, 0)
	if statesOut != "" {
		states := ParsePortStates(statesOut)
		layout := []PortLayout{}
		if out := p.runBest(ctx, CmdBoardJSON, 0); out != "" {
			if lay, err := ParsePortLayout(out); err == nil {
				layout = lay
			}
		}
		members := map[string]bool{}
		if out := p.runBest(ctx, CmdBrifMembers, 0); out != "" {
			for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
				if l != "" {
					members[l] = true
				}
			}
		}
		// Contadores por iface (#305): absolutos del MISMO /proc/net/dev de
		// probeSystem (si ya corrió este ciclo); los rates los computa el
		// server con el delta entre payloads.
		var ifaces map[string]IfRate
		if p.lastNetRaw != "" {
			ifaces = map[string]IfRate{}
			for name, c := range ParseNetDevIfaces(p.lastNetRaw) {
				ifaces[name] = IfRate{IfCounters: c}
			}
		}
		fd.Ports = BuildEthPorts(layout, states, members, ifaces)
		any = true
	}
	if !any {
		return nil
	}
	if fd.MACs == nil {
		fd.MACs = map[string]string{}
	}
	if fd.Ports == nil {
		fd.Ports = []EthPort{}
	}
	return fd
}

// probeLuCI: etiquetas de puertos/VLANs de LuCI (issue #258), si el router
// las define en /etc/config/luci. Best-effort: sin fichero/sección → nil.
func (p *Prober) probeLuCI(ctx context.Context) *LuCILabels {
	out := p.runBest(ctx, CmdLuCILabels, 0)
	if out == "" {
		return nil
	}
	return ParseLuCILabels(out)
}

// probeDawn: lee el estado de DAWN (roaming/band-steering, Fase 14) vía
// `ubus call dawn get_network`. Devuelve nil si DAWN no está instalado
// (fail-soft silencioso — la sección queda ausente en el payload).
//
// El output de ubus mezcla campos del AP (channel, freq, etc.) con clientes
// anidados (MAC → {signal, ht, ...}) en el mismo objeto por BSSID. Los
// distinguimos por tipo: escalares = metadata del AP, objetos = clientes.
func (p *Prober) probeDawn(ctx context.Context) *DawnData {
	out := p.runBest(ctx, "ubus call dawn get_network", 5*time.Second)
	if out == "" {
		return nil
	}

	// SSID → BSSID → {campos mixtos}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil
	}

	ssids := make(map[string]DawnSSID, len(raw))
	for ssid, bssids := range raw {
		var aps []DawnAP
		clients := make(map[string]DawnClient)

		for bssidKey, bssidRaw := range bssids {
			// Parsear todos los campos del BSSID como mapa flexible.
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(bssidRaw, &fields); err != nil {
				continue
			}

			// ¿Tiene "channel"? → es un AP.
			chRaw, hasChannel := fields["channel"]
			if !hasChannel {
				continue
			}
			var channel int
			json.Unmarshal(chRaw, &channel)
			if channel == 0 {
				continue
			}

			ap := DawnAP{BSSID: bssidKey, Channel: channel}
			json.Unmarshal(fields["freq"], &ap.Freq)
			json.Unmarshal(fields["channel_utilization"], &ap.Utilization)
			json.Unmarshal(fields["num_sta"], &ap.Clients)
			json.Unmarshal(fields["ht_support"], &ap.HT)
			json.Unmarshal(fields["vht_support"], &ap.VHT)
			json.Unmarshal(fields["local"], &ap.Local)
			json.Unmarshal(fields["hostname"], &ap.Hostname)
			aps = append(aps, ap)

			// Buscar clientes anidados: cualquier key cuyo valor sea un
			// objeto (empieza con '{') y tenga "signal".
			for mac, valRaw := range fields {
				if mac == "channel" || mac == "freq" || mac == "channel_utilization" ||
					mac == "num_sta" || mac == "ht_support" || mac == "vht_support" ||
					mac == "local" || mac == "hostname" || mac == "iface" ||
					mac == "neighbor_report" || mac == "op_class" {
					continue
				}
				s := strings.TrimSpace(string(valRaw))
				if !strings.HasPrefix(s, "{") {
					continue
				}
				var c struct {
					Signal int  `json:"signal"`
					HT     bool `json:"ht"`
					VHT    bool `json:"vht"`
				}
				if json.Unmarshal(valRaw, &c) == nil && c.Signal < 0 {
					clients[strings.ToUpper(mac)] = DawnClient{
						BSSID: bssidKey, Signal: c.Signal, HT: c.HT, VHT: c.VHT,
					}
				}
			}
		}

		if len(aps) > 0 {
			ssids[ssid] = DawnSSID{APs: aps, Clients: clients}
		}
	}

	if len(ssids) == 0 {
		return nil
	}
	return &DawnData{SSIDs: ssids}
}

// probeUsteer: lee el estado de usteer (roaming/steering) vía
// `ubus call usteer local_info`, `remote_info` y `connected_clients`. Devuelve
// nil si usteer no está instalado (fail-soft silencioso — la sección queda
// ausente en el payload).
func (p *Prober) probeUsteer(ctx context.Context) *UsteerData {
	localOut := p.runBest(ctx, "ubus call usteer local_info", 5*time.Second)
	remoteOut := p.runBest(ctx, "ubus call usteer remote_info", 5*time.Second)
	if localOut == "" && remoteOut == "" {
		return nil
	}
	clientsOut := p.runBest(ctx, "ubus call usteer connected_clients", 5*time.Second)
	return ParseUsteer(localOut, remoteOut, clientsOut)
}

// usteerAPRaw es la forma cruda de una entrada de `ubus call usteer
// local_info` / `remote_info` (por BSSID/iface).
type usteerAPRaw struct {
	BSSID  string `json:"bssid"`
	SSID   string `json:"ssid"`
	Freq   int    `json:"freq"`
	NAssoc int    `json:"n_assoc"`
	Load   int    `json:"load"`
}

// usteerClientRaw es la forma cruda de una entrada de `ubus call usteer
// connected_clients` (por MAC).
type usteerClientRaw struct {
	Signal int `json:"signal"`
}

// ParseUsteer parsea las salidas de `ubus call usteer local_info`,
// `remote_info` y `connected_clients` a un UsteerData (SSID → APs + clientes).
// Función pura para testear sin ejecutar ubus.
//
//   - local_info:  {"<iface>": {bssid, ssid, freq, n_assoc, load, ...}}  (este router)
//   - remote_info: {"<ip>#<iface>": {...}}                                (otros routers)
//   - connected_clients: {"<iface>": {"<mac>": {signal, ...}}}
//
// Nota: usteer usa el nombre ubus de hostapd (`hostapd.phy0-ap0`) en
// local_info/remote_info y el ifname (`hostapd.wlan0`) en connected_clients;
// cuando ambas convenciones no casan, el cliente se omite (no se le asigna un
// SSID erróneo).
func ParseUsteer(localOut, remoteOut, clientsOut string) *UsteerData {
	ssids := make(map[string]UsteerSSID)
	ifaceSSID := map[string]string{}

	addAP := func(ssid string, ap UsteerAP) {
		s := ssids[ssid]
		s.APs = append(s.APs, ap)
		if s.Clients == nil {
			s.Clients = map[string]UsteerClient{}
		}
		ssids[ssid] = s
	}

	if localOut != "" {
		var local map[string]usteerAPRaw
		if json.Unmarshal([]byte(localOut), &local) == nil {
			for iface, ap := range local {
				if ap.SSID == "" || ap.BSSID == "" {
					continue
				}
				ifaceSSID[iface] = ap.SSID
				addAP(ap.SSID, UsteerAP{
					BSSID: strings.ToUpper(ap.BSSID), Freq: ap.Freq,
					Load: ap.Load, Clients: ap.NAssoc, Local: true,
				})
			}
		}
	}

	if remoteOut != "" {
		var remote map[string]usteerAPRaw
		if json.Unmarshal([]byte(remoteOut), &remote) == nil {
			for key, ap := range remote {
				if ap.SSID == "" || ap.BSSID == "" {
					continue
				}
				host := key
				if i := strings.IndexByte(key, '#'); i >= 0 {
					host = key[:i]
				}
				addAP(ap.SSID, UsteerAP{
					BSSID: strings.ToUpper(ap.BSSID), Hostname: host, Freq: ap.Freq,
					Load: ap.Load, Clients: ap.NAssoc, Local: false,
				})
			}
		}
	}

	if clientsOut != "" {
		var clients map[string]map[string]usteerClientRaw
		if json.Unmarshal([]byte(clientsOut), &clients) == nil {
			for iface, macs := range clients {
				ssid := ifaceSSID[iface]
				if ssid == "" {
					continue
				}
				s := ssids[ssid]
				if s.Clients == nil {
					s.Clients = map[string]UsteerClient{}
				}
				for mac, c := range macs {
					if c.Signal >= 0 {
						continue // señal 0/positiva = sin datos
					}
					s.Clients[strings.ToUpper(mac)] = UsteerClient{Signal: c.Signal}
				}
				ssids[ssid] = s
			}
		}
	}

	if len(ssids) == 0 {
		return nil
	}
	return &UsteerData{SSIDs: ssids}
}

// probeDiscovery (#338): mDNS service discovery + randomized MAC detection.
// Runs umdns browse; enriches with randomized MACs from wireless clients.
func (p *Prober) probeDiscovery(ctx context.Context, wireless *WirelessData) *DiscoveryData {
	out, err := p.run.Run(ctx, CmdMdnsBrowse, 5*time.Second)
	var dd *DiscoveryData
	if err == nil && strings.TrimSpace(out) != "" && strings.TrimSpace(out) != "{}" {
		dd = ParseMdnsBrowse([]byte(out))
	}
	if dd == nil {
		dd = &DiscoveryData{}
	}

	// Detect randomized MACs from wireless clients.
	if wireless != nil {
		for mac := range wireless.Clients {
			if IsRandomizedMAC(mac) {
				dd.RandomMACs = append(dd.RandomMACs, mac)
			}
		}
	}
	if len(dd.Services) == 0 && len(dd.HostByIP) == 0 && len(dd.RandomMACs) == 0 {
		return nil
	}
	return dd
}

// probeArp: tabla ARP del kernel (#377). nil si no hay fichero (equipo sin
// ARP, p. ej. contenedor); mapa vacío es resultado real.
func (p *Prober) probeArp(ctx context.Context) map[string]string {
	out := p.runBest(ctx, CmdProcArp, 3*time.Second)
	if out == "" {
		return nil
	}
	m := ParseArp(out)
	if m == nil {
		m = map[string]string{}
	}
	return m
}
