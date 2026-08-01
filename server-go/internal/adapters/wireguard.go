// wireguard.go — Adapter live para WireGuard (port de
// src/adapters/wireguard.js, SPEC §7.5): parsea `wg show <iface> dump` vía
// SSH en el gateway. Quirk preservado: status 'active' siempre que el
// comando responda (peers.length >= 0 ⇒ true).
package adapters

import (
	"strconv"
	"strings"
	"time"
)

const handshakeActiveSec = 180 // handshake < 3 min ⇒ peer activo

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

// GetWireGuardStats obtiene WireGuardStats del gateway vía SSH.
// peerNames: mapa tunnelIp/pubkey → etiqueta opcional.
func GetWireGuardStats(pool *SSHPool, host, iface, subnet string, peerNames map[string]WGPeerName) (*WireGuardStats, error) {
	dump, err := pool.Run(host, "wg show "+iface+" dump", 0)
	if err != nil {
		return nil, err
	}
	peers := ParseWGDump(dump)
	nowSec := time.Now().Unix()

	out := &WireGuardStats{
		Interface: iface,
		Subnet:    subnet,
		Status:    "active", // quirk: siempre que el comando responda
		Peers:     []WGPeer{},
	}
	for i, p := range peers {
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
			id = "peer-" + strconv.Itoa(i+1)
		}
		name := named.Name
		if name == "" {
			name = tunnelIP
			if name == "" {
				name = "Peer " + p.Pubkey[:min(len(p.Pubkey), 8)]
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
		out.Peers = append(out.Peers, WGPeer{
			ID: id, Name: name, Type: typ, TunnelIP: tunnelIP,
			Active: active, LastHandshake: lastHandshake,
			Rx: fmtBytes(float64(p.RxBytes)), Tx: fmtBytes(float64(p.TxBytes)),
		})
	}
	return out, nil
}
