// wireguard.go — Adapter live para WireGuard (port de
// src/adapters/wireguard.js, SPEC §7.5): parsea `wg show <iface> dump` vía
// SSH en el gateway. Quirk preservado: status 'active' siempre que el
// comando responda (peers.length >= 0 ⇒ true).
package adapters

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const handshakeActiveSec = 180 // handshake < 3 min ⇒ peer activo

// wgUciMarker separa el dump `wg show` de la config `uci show network` en una
// única sesión SSH. Permite resolver el nombre del peer (option description de
// los wireguard_peer) sin un segundo roundtrip (issue #659).
const wgUciMarker = "===NETPULSE_UCI==="

// wgStatsCommand devuelve el comando que emite el dump de `wg show <iface>`
// y, a continuación, la config WireGuard UCI. El exit code es el de `wg show`
// (si el túnel está caído, la llamada falla y GetWireGuardStats salta esa
// interfaz sin tumbar el resto, issue #714).
func wgStatsCommand(iface string) string {
	return fmt.Sprintf("wg show %s dump; rc=$?; echo '%s'; uci show network 2>/dev/null; exit $rc", iface, wgUciMarker)
}

// wgListCommand lista las interfaces WireGuard presentes en el router
// (`wg show interfaces`): nombres separados por espacios en una sola línea.
const wgListCommand = "wg show interfaces"

// parseWGInterfaces parsea la salida de `wg show interfaces` en una lista de
// nombres. Salida vacía → lista vacía (sin interfaces).
func parseWGInterfaces(out string) []string {
	return strings.Fields(strings.TrimSpace(out))
}

// wgInterfaces resuelve qué interfaces sondear. iface vacío o "auto" →
// descubre todas con `wg show interfaces`; un valor explícito (una interfaz o
// una lista separada por comas) actúa como filtro/override sin tocar el
// router para listarlas (#713). Si el listado falla en modo auto (p. ej. un
// `wg` sin el subcomando `interfaces`), cae al default histórico `wg0` para
// no dejar el panel sin datos (#714).
func wgInterfaces(pool sshRunner, host, iface string) []string {
	v := strings.TrimSpace(iface)
	if v != "" && v != "auto" {
		out := []string{}
		for _, part := range strings.Split(v, ",") {
			if s := strings.TrimSpace(part); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	raw, err := pool.Run(host, wgListCommand, 0)
	if err != nil {
		return []string{"wg0"}
	}
	return parseWGInterfaces(raw)
}

// splitWGUci separa la salida combinada en la parte del dump y la parte UCI.
func splitWGUci(out string) (string, string) {
	if idx := strings.Index(out, wgUciMarker); idx >= 0 {
		uci := strings.TrimLeft(out[idx+len(wgUciMarker):], "\n")
		return out[:idx], uci
	}
	return out, ""
}

// WGDumpPeer es un peer parseado del dump (campos numéricos crudos).
type WGDumpPeer struct {
	Pubkey       string
	Endpoint     *string // nil si "(none)"
	AllowedIPs   string
	HandshakeSec int64
	RxBytes      int64
	TxBytes      int64
}

// ParseWGDump parsea la salida TSV de `wg show <iface> dump` (literal del
// JS: se ignora la línea 1 — interfaz — y las líneas con < 8 campos).
func ParseWGDump(dump string) []WGDumpPeer {
	lines := strings.Split(strings.TrimSpace(dump), "\n")
	peers := []WGDumpPeer{}
	for _, line := range lines[1:] {
		f := strings.Split(line, "\t")
		if len(f) < 8 {
			continue
		}
		var endpoint *string
		if f[2] != "(none)" {
			ep := f[2]
			endpoint = &ep
		}
		peers = append(peers, WGDumpPeer{
			Pubkey:       f[0],
			Endpoint:     endpoint,
			AllowedIPs:   f[3],
			HandshakeSec: atoiOr(f[4], 0),
			RxBytes:      atoiOr(f[5], 0),
			TxBytes:      atoiOr(f[6], 0),
		})
	}
	return peers
}

func atoiOr(s string, def int64) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return def
	}
	return n
}

// WGPeerName etiqueta un peer por tunnelIp o pubkey (peerNames del config).
type WGPeerName struct {
	ID   string
	Name string
	Type string
}

// parseWGUciDescs extrae de la salida de `uci show network` las descripciones
// de los peers WireGuard (option description), indexadas por public_key y por
// cada allowed_ip normalizada (sin /32). Devuelve un mapa key → descripción.
// Solo incluye secciones wireguard_peer con descripción no vacía.
func parseWGUciDescs(uci string) map[string]string {
	type peerDesc struct {
		pub        string
		allowedIPs string
		desc       string
	}
	bySec := map[string]*peerDesc{}
	for _, line := range strings.Split(uci, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "network.") {
			continue
		}
		rest := strings.TrimPrefix(line, "network.")
		eq := strings.Index(rest, "=")
		if eq < 0 {
			continue
		}
		val := strings.Trim(strings.TrimPrefix(rest[eq:], "="), "'")
		path := rest[:eq]
		dot := strings.Index(path, ".")
		if dot < 0 {
			continue
		}
		sec, opt := path[:dot], path[dot+1:]
		p := bySec[sec]
		if p == nil {
			p = &peerDesc{}
			bySec[sec] = p
		}
		switch opt {
		case "public_key":
			p.pub = val
		case "allowed_ips":
			if p.allowedIPs != "" {
				p.allowedIPs += ","
			}
			p.allowedIPs += val
		case "description":
			p.desc = val
		}
	}
	out := map[string]string{}
	for _, p := range bySec {
		if p.desc == "" {
			continue
		}
		if p.pub != "" {
			out[p.pub] = p.desc
		}
		for _, cidr := range strings.Split(p.allowedIPs, ",") {
			ip := strings.Replace(strings.TrimSpace(cidr), "/32", "", 1)
			if ip != "" {
				out[ip] = p.desc
			}
		}
	}
	return out
}

// buildWGPeer convierte un peer del dump en un WGPeer, resolviendo el nombre
// con la prioridad de siempre (nombre de device NetPulse → description UCI →
// tunnelIP → "Peer <pubkey8>", issue #659) y asignando el fallback de ID
// "peer-<idx>" con un contador global (idx) para que los peers de varias
// interfaces no colisionen (#713).
func buildWGPeer(p WGDumpPeer, peerNames map[string]WGPeerName, descs map[string]string, nowSec int64, idx int) WGPeer {
	tunnelIP := ""
	if parts := strings.Split(p.AllowedIPs, ","); len(parts) > 0 {
		tunnelIP = strings.Replace(parts[0], "/32", "", 1)
	}
	named, ok := peerNames[tunnelIP]
	if !ok {
		named = peerNames[p.Pubkey]
	}
	id := named.ID
	if id == "" {
		id = "peer-" + strconv.Itoa(idx)
	}
	name := named.Name
	if name == "" {
		name = descs[p.Pubkey]
		if name == "" {
			name = descs[tunnelIP]
		}
		if name == "" {
			name = tunnelIP
			if name == "" {
				name = "Peer " + p.Pubkey[:min(len(p.Pubkey), 8)]
			}
		}
	}
	typ := named.Type
	if typ == "" {
		typ = "desconocido"
	}
	active := p.HandshakeSec > 0 && nowSec-p.HandshakeSec < handshakeActiveSec
	lastHandshake := "nunca"
	if p.HandshakeSec > 0 {
		lastHandshake = relTime(p.HandshakeSec, nowSec)
	}
	return WGPeer{
		ID: id, Name: name, Type: typ, TunnelIP: tunnelIP,
		Active: active, LastHandshake: lastHandshake,
		Rx: fmtBytes(float64(p.RxBytes)), Tx: fmtBytes(float64(p.TxBytes)),
	}
}

// GetWireGuardStats obtiene WireGuardStats del gateway vía SSH, agregando los
// peers de TODAS las interfaces WireGuard del router (issue #713).
// iface: filtro/override de interfaces a sondear ("auto"/vacío → descubrir
// todas con `wg show interfaces`; valor → interfaz o lista separada por
// comas). peerNames: mapa tunnelIp/pubkey → etiqueta opcional (nombre de
// device). El nombre de cada peer se resuelve en este orden: nombre de device
// de NetPulse (peerNames) → description del peer en UCI (OpenWrt) → IP del
// túnel → "Peer <pubkey8>" (issue #659).
//
// Tolerancia a fallos (#714): el fallo del dump de UNA interfaz se salta (se
// acumulan los peers de las que responden); si TODAS fallan se devuelve
// Status "inactive" SIN error, para que pollWireGuard no loguee "no
// disponible" en cada tick.
func GetWireGuardStats(pool sshRunner, host, iface, subnet string, peerNames map[string]WGPeerName) (*WireGuardStats, error) {
	ifaces := wgInterfaces(pool, host, iface)
	stats := &WireGuardStats{
		Interface: strings.Join(ifaces, ", "),
		Subnet:    subnet,
		Status:    "active", // quirk: siempre que el comando responda
		Peers:     []WGPeer{},
	}
	if len(ifaces) == 0 {
		// Sin interfaces WireGuard en el router: no hay túneles que sondear.
		stats.Status = "inactive"
		return stats, nil
	}
	nowSec := time.Now().Unix()
	descs := map[string]string{}
	idx := 1
	ok := 0
	for _, ifc := range ifaces {
		out, err := pool.Run(host, wgStatsCommand(ifc), 0)
		if err != nil {
			// Una interfaz caída no tumba el resto: se salta (#714).
			continue
		}
		ok++
		dump, uci := splitWGUci(out)
		for k, v := range parseWGUciDescs(uci) {
			descs[k] = v
		}
		for _, p := range ParseWGDump(dump) {
			stats.Peers = append(stats.Peers, buildWGPeer(p, peerNames, descs, nowSec, idx))
			idx++
		}
	}
	if ok == 0 {
		// Ninguna interfaz respondió: túnel inactivo, sin error (#714).
		stats.Status = "inactive"
	}
	return stats, nil
}
