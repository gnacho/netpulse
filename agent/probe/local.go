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
}

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
	pl.Data.Wireless = p.probeWireless(ctx)
	pl.Data.DHCP = p.probeDHCP(ctx)
	pl.Data.FDB = p.probeFDB(ctx)
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
	pl.Data.Wireless = p.probeWireless(ctx)
	pl.Data.DHCP = p.probeDHCP(ctx)
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

// probeWireless: clientes asociados (iwinfo assoclist) + radios por banda.
// nil si iwinfo no existe en el equipo.
func (p *Prober) probeWireless(ctx context.Context) *WirelessData {
	wd := &WirelessData{}
	any := false
	if out := p.runBest(ctx, CmdIwinfoAssoc, 8*time.Second); out != "" {
		wd.Clients = ParseWirelessClients(out)
		any = true
	}
	if out := p.runBest(ctx, CmdRadios, 8*time.Second); out != "" {
		wd.Radios = ParseRadios(out)
		any = true
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
		fd.Ports = BuildEthPorts(layout, states, members)
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
