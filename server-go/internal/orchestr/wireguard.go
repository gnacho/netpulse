// wireguard.go - módulo de orquestación "WireGuard peers" (Fase 10.3).
//
// Reconoce los peers de un túnel WireGuard contra el estado deseado: crea la
// interfaz network.wg0 (si no existe), añade/actualiza/borra las secciones
// network.wgpeer<N> (public_key + allowed_ips) y termina con un reload de red
// más healthcheck `wg show`. Si el túnel que estaba arriba no levanta tras el
// apply, el executor revierte el plan (rollback real).
//
// Anti-lockout: un peer cuyo public_key coincide con
// WireGuardDesired.AdminPeerPublicKey NUNCA se borra (el admin gestiona el
// túnel desde ese peer; borrarlo aislaría al admin del túnel).
package orchestr

import (
	"fmt"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
)

// WireGuardPeer es un peer del túnel WireGuard.
type WireGuardPeer struct {
	Name string `json:"name"`
	// PublicKey: clave pública base64 del peer (wireguard pubkey).
	PublicKey string `json:"publicKey"`
	// AllowedIPs: CIDRs permitidos del peer, separados por coma
	// (p. ej. "10.0.0.2/32" o "10.0.0.2/32,10.0.1.0/24").
	AllowedIPs string `json:"allowedIps"`
}

// WireGuardDesired es el estado deseado del módulo WireGuard.
type WireGuardDesired struct {
	// Interface: nombre de la interfaz del túnel (default "wg0").
	Interface string `json:"interface"`
	// Peers: conjunto declarativo de peers. Los peers presentes en el router
	// que NO estén en esta lista se borran (salvo el del admin).
	Peers []WireGuardPeer `json:"peers"`
	// AdminPeerPublicKey: pubkey del peer del admin. Protección anti-lockout:
	// ese peer nunca se borra, aunque no esté en Peers.
	AdminPeerPublicKey string `json:"adminPeerPubkey,omitempty"`
}

// WireGuardScenario describe el estado de WireGuard en el router.
type WireGuardScenario struct {
	// WGInstalled: /usr/bin/wg existe (wireguard-tools instalado).
	WGInstalled bool
	// WGActive: `wg show` reporta al menos una interfaz arriba.
	WGActive bool
	// WGIfaces: interfaces WireGuard presentes en UCI
	// (network.<name>.proto='wireguard').
	WGIfaces []string
	// ActiveIfaces: interfaces activas en el kernel según `wg show`.
	ActiveIfaces []string
	// Peers: secciones wgpeer existentes (public_key + allowed_ips).
	Peers []WireGuardPeer
}

// probeWireGuardCmd detecta instalación, estado del túnel y secciones UCI.
// `|| true` tras wg show para no abortar la sesión si wg no está instalado.
const probeWireGuardCmd = `echo '===WG_BIN==='
[ -x /usr/bin/wg ] && echo yes || echo no
echo '===WG==='
wg show 2>/dev/null
echo '===NETWORK==='
uci show network 2>/dev/null
echo '===END==='`

// DetectWireGuard ejecuta el probe y devuelve el escenario.
func DetectWireGuard(run CommandRunner, host string) (WireGuardScenario, error) {
	out, err := run.Run(host, probeWireGuardCmd, 8*time.Second)
	if err != nil {
		return WireGuardScenario{}, err
	}
	return parseWireGuard(out), nil
}

// parseWireGuard extrae el escenario del output del probe. Función pura.
func parseWireGuard(out string) WireGuardScenario {
	sc := WireGuardScenario{}
	sections := splitSections(out)
	sc.WGInstalled = strings.TrimSpace(firstNonEmpty(sections["WG_BIN"])) == "yes"

	// `wg show`: líneas "interface: <nombre>".
	for _, line := range sections["WG"] {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			if name := strings.TrimSpace(strings.TrimPrefix(line, "interface:")); name != "" {
				sc.ActiveIfaces = append(sc.ActiveIfaces, name)
			}
		}
	}
	sc.WGActive = len(sc.ActiveIfaces) > 0

	// `uci show network`: secciones interface (proto wireguard) y peers.
	uci := strings.Join(sections["NETWORK"], "\n")
	for _, line := range strings.Split(uci, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "network.") {
			continue
		}
		rest := strings.TrimPrefix(line, "network.")
		switch {
		case strings.HasSuffix(rest, "=interface"):
			sec := strings.TrimSuffix(rest, "=interface")
			if wireguardSectionProto(uci, sec) {
				sc.WGIfaces = append(sc.WGIfaces, sec)
			}
		case strings.Contains(rest, "=wireguard_"):
			sec := rest[:strings.Index(rest, "=")]
			peer := WireGuardPeer{
				Name:      sec,
				PublicKey: wireguardOption(uci, sec, "public_key"),
			}
			peer.AllowedIPs = normalizeAllowedIPs(wireguardOptionList(uci, sec, "allowed_ips"))
			sc.Peers = append(sc.Peers, peer)
		}
	}
	return sc
}

// wireguardSectionProto devuelve true si la sección interface tiene
// proto='wireguard' ("network.<sec>.proto='wireguard'").
func wireguardSectionProto(uci, sec string) bool {
	prefix := "network." + sec + ".proto="
	for _, line := range strings.Split(uci, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.Contains(line, "'wireguard'")
		}
	}
	return false
}

// wireguardOption lee una option de una sección ("network.<sec>.<opt>='v'").
func wireguardOption(uci, sec, option string) string {
	prefix := "network." + sec + "." + option + "="
	for _, line := range strings.Split(uci, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimPrefix(line, prefix), "'")
		}
	}
	return ""
}

// wireguardOptionList une los valores de una option tipo lista
// ("network.<sec>.allowed_ips='v'" por cada entrada).
func wireguardOptionList(uci, sec, option string) string {
	prefix := "network." + sec + "." + option + "="
	var vals []string
	for _, line := range strings.Split(uci, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			vals = append(vals, strings.Trim(strings.TrimPrefix(line, prefix), "'"))
		}
	}
	return strings.Join(vals, ",")
}

// WireGuardOps genera las Ops para reconciliar los peers del túnel.
func WireGuardOps(desired WireGuardDesired, sc WireGuardScenario) []executor.Op {
	iface := desired.Interface
	if iface == "" {
		iface = "wg0"
	}
	var ops []executor.Op

	// 1. Interfaz: crear la sección si no existe.
	if !wgInterfaceExists(sc, iface) {
		ops = append(ops,
			executor.Op{Kind: "uci_set_named", Args: map[string]string{"config": "network", "section": iface, "type": "interface"}, Desc: "Create wireguard interface " + iface},
			executor.Op{Kind: "uci_set", Args: map[string]string{"config": "network", "section": iface, "option": "proto", "value": "wireguard"}, Desc: "Set wireguard proto on " + iface},
		)
	}

	// 2. Peers del desired: crear los nuevos, actualizar los existentes.
	for _, p := range desired.Peers {
		pub := strings.TrimSpace(p.PublicKey)
		if pub == "" {
			continue // fila vacía de la UI
		}
		existing := wgPeerByPubkey(sc, pub)
		if existing != nil {
			// public_key igual por construcción (match por pubkey): solo
			// reconciliar allowed_ips si cambia.
			if normalizeAllowedIPs(existing.AllowedIPs) != normalizeAllowedIPs(p.AllowedIPs) {
				ops = append(ops, executor.Op{Kind: "uci_delete", Args: map[string]string{"config": "network", "section": existing.Name, "option": "allowed_ips"}, Desc: "Reset allowed_ips on " + existing.Name})
				for _, cidr := range splitAllowedIPs(p.AllowedIPs) {
					ops = append(ops, executor.Op{Kind: "uci_add_list", Args: map[string]string{"config": "network", "section": existing.Name, "option": "allowed_ips", "value": cidr}, Desc: "Add allowed_ip " + cidr + " to " + existing.Name})
				}
			}
			continue
		}
		name := nextWgPeerName(sc, iface)
		ops = append(ops,
			executor.Op{Kind: "uci_set_named", Args: map[string]string{"config": "network", "section": name, "type": "wireguard_" + iface}, Desc: "Create peer section " + name},
			executor.Op{Kind: "uci_set", Args: map[string]string{"config": "network", "section": name, "option": "public_key", "value": pub}, Desc: "Set public key on " + name},
		)
		for _, cidr := range splitAllowedIPs(p.AllowedIPs) {
			ops = append(ops, executor.Op{Kind: "uci_add_list", Args: map[string]string{"config": "network", "section": name, "option": "allowed_ips", "value": cidr}, Desc: "Add allowed_ip " + cidr + " to " + name})
		}
		sc.Peers = append(sc.Peers, WireGuardPeer{Name: name, PublicKey: pub})
	}

	// 3. Borrar peers que ya no están en el desired. Anti-lockout: el peer
	// cuyo public_key es el del admin NUNCA se borra.
	for _, p := range sc.Peers {
		if wgPeerDesired(desired.Peers, p.PublicKey) {
			continue
		}
		if p.PublicKey != "" && p.PublicKey == desired.AdminPeerPublicKey {
			continue // protección anti-lockout
		}
		ops = append(ops, executor.Op{Kind: "uci_delete_section", Args: map[string]string{"config": "network", "section": p.Name}, Desc: "Remove peer " + p.Name})
	}

	if len(ops) == 0 {
		return nil
	}
	ops = append(ops, executor.Op{Kind: "uci_commit", Args: map[string]string{"config": "network"}, Desc: "Commit network config"})
	ops = append(ops, executor.Op{Kind: "service", Args: map[string]string{"name": "network", "action": "reload"}, Desc: "Reload network (apply wireguard)"})
	if wgCheckNeeded(sc, iface) {
		ops = append(ops, executor.Op{Kind: "wg_check", Args: map[string]string{"interface": iface}, Desc: "Check tunnel " + iface + " is up (wg show)"})
	}
	return ops
}

// WireGuardHealthcheck verifica que el túnel sigue arriba tras el apply:
// `wg show <iface>` falla si la interfaz no existe o está caída. Es el
// healthcheck del módulo (el executor lo ejecuta como op wg_check en el plan).
func WireGuardHealthcheck(run CommandRunner, host, iface string) error {
	if iface == "" {
		iface = "wg0"
	}
	out, err := run.Run(host, "wg show "+iface, 8*time.Second)
	if err != nil {
		return fmt.Errorf("tunnel %s not up: %w", iface, err)
	}
	if !strings.Contains(out, "interface: "+iface) {
		return fmt.Errorf("tunnel %s not up (wg show sin interfaz)", iface)
	}
	return nil
}

// wgInterfaceExists: la interfaz deseada está en UCI con proto wireguard.
func wgInterfaceExists(sc WireGuardScenario, iface string) bool {
	return wgContains(sc.WGIfaces, iface)
}

// wgPeerByPubkey busca un peer existente por su public_key (nil si no existe).
func wgPeerByPubkey(sc WireGuardScenario, pub string) *WireGuardPeer {
	for i := range sc.Peers {
		if sc.Peers[i].PublicKey == pub {
			return &sc.Peers[i]
		}
	}
	return nil
}

// wgPeerDesired: hay un peer en desired con ese public_key.
func wgPeerDesired(peers []WireGuardPeer, pub string) bool {
	for _, p := range peers {
		if strings.TrimSpace(p.PublicKey) == pub {
			return true
		}
	}
	return false
}

// nextWgPeerName devuelve el nombre libre wgpeer<N> más bajo no usado.
func nextWgPeerName(sc WireGuardScenario, iface string) string {
	used := map[string]bool{iface: true}
	for _, p := range sc.Peers {
		used[p.Name] = true
	}
	for n := 1; ; n++ {
		name := fmt.Sprintf("wgpeer%d", n)
		if !used[name] {
			return name
		}
	}
}

// splitAllowedIPs parte un valor "cidr1,cidr2" en CIDRs individuales.
func splitAllowedIPs(val string) []string {
	var out []string
	for _, part := range strings.Split(val, ",") {
		if cidr := strings.TrimSpace(part); cidr != "" {
			out = append(out, cidr)
		}
	}
	return out
}

// normalizeAllowedIPs limpia espacios/vacíos de un valor de CIDRs para poder
// comparar listas entre el router y el desired sin depender del formato.
func normalizeAllowedIPs(val string) string {
	parts := splitAllowedIPs(val)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ",")
}

// wgCheckNeeded: solo verificar el túnel si WireGuard está instalado y había
// un túnel activo antes del apply (el healthcheck protege el túnel EXISTENTE).
func wgCheckNeeded(sc WireGuardScenario, iface string) bool {
	if !sc.WGInstalled || !sc.WGActive {
		return false
	}
	return wgContains(sc.ActiveIfaces, iface)
}

func wgContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// validateWireGuardOps valida cada op contra el executor (usado en tests).
func validateWireGuardOps(ops []executor.Op) error {
	for _, op := range ops {
		if err := executor.Validate(op); err != nil {
			return fmt.Errorf("%s: %w", op.Desc, err)
		}
	}
	return nil
}
