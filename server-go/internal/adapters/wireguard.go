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

// wgGlMarker separa, tras el UCI de network, el volcado filtrado del config
// propietario wireguard_server de GL.iNet (firmware GL: el servidor WG no usa
// proto 'wireguard', así que los clientes NO aparecen en uci show network;
// viven en secciones config peers con option name, issue #1433).
const wgGlMarker = "===NETPULSE_GL==="

// wgGlNamesCommand vuelca SOLO name/public_key/client_ip de
// wireguard_server. El grep corre EN EL ROUTER: las private_key de los peers
// (que GL.iNet guarda en ese mismo config) jamás salen del equipo.
const wgGlNamesCommand = "uci show wireguard_server 2>/dev/null | grep -E '\\.(name|public_key|client_ip)='"

// wgStatsCommand devuelve el comando que emite el dump de `wg show <iface>`,
// a continuación la config WireGuard UCI y, tras el segundo marker, los
// nombres de peers del config wireguard_server (GL.iNet). El exit code es el
// de `wg show` (si el túnel está caído, la llamada falla y GetWireGuardStats
// salta esa interfaz sin tumbar el resto, issue #714).
func wgStatsCommand(iface string) string {
	return fmt.Sprintf("wg show %s dump; rc=$?; echo '%s'; uci show network 2>/dev/null; echo '%s'; %s; exit $rc", iface, wgUciMarker, wgGlMarker, wgGlNamesCommand)
}

// awgStatsCommand es la variante AmneziaWG de wgStatsCommand: mismo contrato
// (dump + markers + UCI, exit del dump) pero con la herramienta `awg`, la
// única que puede leer interfaces de la familia amneziawg (issue #776).
func awgStatsCommand(iface string) string {
	return fmt.Sprintf("awg show %s dump; rc=$?; echo '%s'; uci show network 2>/dev/null; echo '%s'; %s; exit $rc", iface, wgUciMarker, wgGlMarker, wgGlNamesCommand)
}

// wgListCommand lista las interfaces WireGuard presentes en el router
// (`wg show interfaces`): nombres separados por espacios en una sola línea.
const wgListCommand = "wg show interfaces"

// awgListCommand lista las interfaces AmneziaWG (`awg show interfaces`).
// AmneziaWG (luci-proto-amneziawg) usa su propia familia netlink y su
// herramienta `awg`: `wg` NO ve sus interfaces (issue #776).
const awgListCommand = "awg show interfaces"

// parseWGInterfaces parsea la salida de `wg show interfaces` en una lista de
// nombres. Salida vacía → lista vacía (sin interfaces).
func parseWGInterfaces(out string) []string {
	return strings.Fields(strings.TrimSpace(out))
}

// wgInterfaces resuelve qué interfaces sondear. iface vacío o "auto" →
// descubre todas con `wg show interfaces` y `awg show interfaces` (AmneziaWG,
// #776); un valor explícito (una interfaz o una lista separada por comas)
// actúa como filtro/override sin tocar el router para listarlas (#713). Si
// AMBOS listados fallan en modo auto (p. ej. un `wg` sin el subcomando
// `interfaces`), cae al default histórico `wg0` para no dejar el panel sin
// datos (#714). Un listado que falle individualmente (p. ej. `awg` no
// instalado) se trata como "sin interfaces de ese tipo".
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
	awgRaw, awgErr := pool.Run(host, awgListCommand, 0)
	if err != nil && awgErr != nil {
		return []string{"wg0"}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, name := range append(parseWGInterfaces(raw), parseWGInterfaces(awgRaw)...) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// splitWGParts separa la salida combinada (dump + marker UCI + marker GL) en
// sus tres partes. Cualquier parte cuyo marker falte queda vacía (los routers
// sin config wireguard_server, es decir, los no-GL.iNet, no emiten nada tras
// el segundo marker).
func splitWGParts(out string) (dump, uci, gl string) {
	dump = out
	if idx := strings.Index(dump, wgUciMarker); idx >= 0 {
		uci = strings.TrimLeft(dump[idx+len(wgUciMarker):], "\n")
		dump = dump[:idx]
	}
	if idx := strings.Index(uci, wgGlMarker); idx >= 0 {
		gl = strings.TrimLeft(uci[idx+len(wgGlMarker):], "\n")
		uci = strings.TrimRight(uci[:idx], "\n")
	}
	return dump, uci, gl
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

// parseWGServerNames extrae los nombres de peers del config propietario
// wireguard_server de GL.iNet (issue #1433): secciones `config peers` con
// option name/public_key/client_ip. Indexa por public_key y por client_ip
// normalizada (sin máscara, p.ej. 10.0.0.2/24 → 10.0.0.2), como hace
// parseWGUciDescs con las allowed_ips. Solo secciones con name no vacío.
func parseWGServerNames(gl string) map[string]string {
	type glPeer struct {
		pub  string
		name string
		ip   string
	}
	bySec := map[string]*glPeer{}
	for _, line := range strings.Split(gl, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "wireguard_server.") {
			continue
		}
		rest := strings.TrimPrefix(line, "wireguard_server.")
		eq := strings.Index(rest, "=")
		if eq < 0 {
			continue
		}
		val := strings.Trim(strings.TrimPrefix(rest[eq:], "="), "'")
		path := rest[:eq]
		dot := strings.LastIndex(path, ".")
		if dot < 0 {
			continue
		}
		sec, opt := path[:dot], path[dot+1:]
		p := bySec[sec]
		if p == nil {
			p = &glPeer{}
			bySec[sec] = p
		}
		switch opt {
		case "public_key":
			p.pub = val
		case "name":
			p.name = val
		case "client_ip":
			p.ip = val
		}
	}
	out := map[string]string{}
	for _, p := range bySec {
		if p.name == "" {
			continue
		}
		if p.pub != "" {
			out[p.pub] = p.name
		}
		if ip := p.ip; ip != "" {
			if i := strings.Index(ip, "/"); i >= 0 {
				ip = ip[:i]
			}
			if ip != "" {
				out[ip] = p.name
			}
		}
	}
	return out
}

// buildWGPeer convierte un peer del dump en un WGPeer, resolviendo el nombre
// con la prioridad de siempre (nombre de device NetPulse → description UCI →
// nombre del config wireguard_server (GL.iNet) → tunnelIP → "Peer <pubkey8>",
// issues #659/#1433) y asignando el fallback de ID
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
// de NetPulse (peerNames) → description del peer en UCI (OpenWrt) → name del
// config wireguard_server (GL.iNet, issue #1433) → IP del túnel →
// "Peer <pubkey8>" (issue #659).
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
			// La interfaz puede ser AmneziaWG (`wg` no la ve; #776): se
			// reintenta con `awg` antes de saltarla.
			out, err = pool.Run(host, awgStatsCommand(ifc), 0)
			if err != nil {
				// Una interfaz caída no tumba el resto: se salta (#714).
				continue
			}
		}
		ok++
		dump, uci, gl := splitWGParts(out)
		// Nombres del config wireguard_server (GL.iNet) como base; la
		// description UCI estándar (network.*) pisa a la de GL cuando ambas
		// existen para la misma clave.
		for k, v := range parseWGServerNames(gl) {
			descs[k] = v
		}
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
