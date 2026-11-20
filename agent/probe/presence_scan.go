package probe

// presence_scan.go — #2110: barrido activo de las subredes que la unidad
// enruta, para los hosts que no dejan ni una entrada de vecino.
//
// El recheck de #2073/#2109 confirma vecinos que YA están en la tabla. Un host
// cableado con IP estática que nunca habla no tiene entrada que confirmar, así
// que la única forma de verlo es tocar su dirección y leer después la tabla.
//
// No usa raw sockets ni paquetes ARP a mano: manda un datagrama UDP a un
// puerto cerrado, y eso obliga al kernel a resolver el vecino por ARP. Si el
// host está, la respuesta ARP deja la entrada REACHABLE y el `ip neigh` que se
// lee después ya lo ve. Sin CAP_NET_RAW, sin arping, sin instalar nada.

import (
	"context"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

// Parámetros del barrido (#2110). El intervalo es largo a propósito: un /24
// cada 30 s es tráfico que nadie pidió. Cada ronda cubre como mucho una /24
// por subred y 32 direcciones a la vez.
const (
	PresenceScanInterval = 5 * time.Minute
	PresenceScanMaxHosts = 254
	presenceScanWorkers  = 32
	presenceScanTimeout  = 200 * time.Millisecond
)

// scanPresenceOff apaga el barrido por entorno (NETPULSE_PRESENCE_SCAN=off).
// El interruptor global de la UI llegará cuando exista el canal de ajustes
// hacia el agente; mientras tanto, esto es la válvula de escape del operador.
const presenceScanEnv = "NETPULSE_PRESENCE_SCAN"

// scanProbe toca una dirección. Variable para los tests: en producción es un
// datagrama UDP al puerto 9, que no necesita privilegios y fuerza la
// resolución ARP del vecino.
var scanProbe = func(ctx context.Context, ip string) {
	d := net.Dialer{Timeout: presenceScanTimeout}
	conn, err := d.DialContext(ctx, "udp", net.JoinHostPort(ip, "9"))
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte{0})
}

// presenceScanDue decide si toca ronda y marca el sello. Un barrido cada
// PresenceScanInterval por unidad, no por subred: es la unidad la que paga el
// tráfico.
func (p *Prober) presenceScanDue() bool {
	p.presenceMu.Lock()
	defer p.presenceMu.Unlock()
	if !p.presenceAt.IsZero() && time.Since(p.presenceAt) < PresenceScanInterval {
		return false
	}
	p.presenceAt = time.Now()
	return true
}

// PresenceScanEnabled: el entorno puede apagarlo siempre. `on` fuerza el
// barrido aunque la unidad no enrute (unidad designada como sonda).
func PresenceScanEnabled() (on, off bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(presenceScanEnv))) {
	case "off", "0", "false", "no":
		return false, true
	case "on", "1", "true", "yes":
		return true, false
	}
	return false, false
}

// ParseGlobalIPv4Prefixes lee la salida de `ip -4 -o addr show scope global`
// y devuelve una subred por interfaz. Lo comparten el agente y el sondeo SSH.
func ParseGlobalIPv4Prefixes(out string) []netip.Prefix {
	if out == "" {
		return nil
	}
	var prefixes []netip.Prefix
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		for i := 0; i+1 < len(f); i++ {
			if f[i] != "inet" {
				continue
			}
			pf, err := netip.ParsePrefix(f[i+1])
			if err != nil || !pf.Addr().Is4() {
				break
			}
			prefixes = append(prefixes, pf.Masked())
			break
		}
	}
	return prefixes
}

// globalIPv4Subnets devuelve las subredes globales que la unidad tiene
// configuradas, una por interfaz.
func (p *Prober) globalIPv4Subnets(ctx context.Context) []netip.Prefix {
	return ParseGlobalIPv4Prefixes(p.runBest(ctx, "ip -4 -o addr show scope global 2>/dev/null", 3*time.Second))
}

// ipv4Forwarding: el kernel reenvía IPv4. En OpenWrt lo pone netifd al
// configurar una interfaz con subred propia y rol de router.
func (p *Prober) ipv4Forwarding(ctx context.Context) bool {
	out := p.runBest(ctx, "cat /proc/sys/net/ipv4/ip_forward 2>/dev/null", 2*time.Second)
	return strings.TrimSpace(out) == "1"
}

// scanPresence ejecuta una ronda si toca. Se llama antes de leer las tablas
// de vecinos, así el mismo `ip neigh` que se lee después ya ve lo que el
// barrido despertó.
func (p *Prober) scanPresence(ctx context.Context) {
	if p.run == nil {
		return
	}
	forced, off := PresenceScanEnabled()
	if off {
		return
	}
	if !p.presenceScanDue() {
		return
	}
	subnets := p.globalIPv4Subnets(ctx)
	// Gate por unidad que enruta (#2110): forwarding y más de una subred
	// propia. Con una sola subred la unidad es un AP o un bridge y no le toca
	// sondear; `NETPULSE_PRESENCE_SCAN=on` lo fuerza para una unidad designada.
	if !forced && (len(subnets) < 2 || !p.ipv4Forwarding(ctx)) {
		return
	}
	if len(subnets) > 8 {
		subnets = subnets[:8] // techo de cortesía: una unidad no enruta 20 LAN
	}
	sem := make(chan struct{}, presenceScanWorkers)
	var wg sync.WaitGroup
	for _, pf := range subnets {
		for _, ip := range HostAddrs(pf, PresenceScanMaxHosts) {
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(ip string) {
				defer wg.Done()
				defer func() { <-sem }()
				scanProbe(ctx, ip)
			}(ip)
		}
	}
	wg.Wait()
}

// hostAddrs enumera las direcciones de host de una subred: sin la de red ni
// la de broadcast, y con tope para no convertir una /16 en una tormenta.
func HostAddrs(pf netip.Prefix, max int) []string {
	if !pf.IsValid() || !pf.Addr().Is4() {
		return nil
	}
	b := pf.Addr().As4()
	base := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	size := uint32(1) << uint(32-pf.Bits())
	if size <= 2 {
		// /31 y /32 no tienen hosts que sondear (RFC 3021 aparte).
		return nil
	}
	last := base + size - 1 // broadcast
	capacity := int(size) - 2
	if capacity > max {
		capacity = max
	}
	out := make([]string, 0, capacity)
	for v := base + 1; v < last && len(out) < max; v++ {
		out = append(out, netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}).String())
	}
	return out
}
