package adapters

import (
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Inferencia de topología v5 (FDB): puertos físicos, switches/bridges e
// hipervisores. Función pura (sin SSH ni BD) para ser testeable.
//
// Reglas (plan 2-Ago-2026; regla de anclaje refinada 2-Ago-2026):
//   - Un puerto físico (lanN) con UNA MAC aprendida = cableado directo →
//     Device.Port.
//   - Un puerto con VARIAS MACs = algo multiplexándolo (switch, hipervisor,
//     PLC…). El FDB no dice qué: OUI heterogéneo → "inferred" (círculo
//     dashed, sin afirmar); alguna MAC con OUI de hipervisor y exactamente
//     un host → "hypervisor" (sus VMs se anidan bajo el host).
//   - Se excluyen las MACs de los propios routers (su bridge br-lan) y las
//     de clientes atribuidos a OTROS routers (los clientes wifi de un AP
//     aparecen en el FDB del gateway por el puerto del uplink).
//   - Anclaje al GATEWAY (decisión del usuario 2-Ago-2026): los nodos
//     "inferred" SOLO se crean en el gateway. En un AP, un puerto multi-MAC
//     es casi seguro su UPLINK (aprende las MACs de toda la LAN) y el FDB
//     no puede distinguirlo de un switch local → no se afirma nada: esos
//     clientes quedan "sin evidencia" y el frontend los cuelgan del gateway.
//     Única excepción tras un AP: hipervisor con evidencia clara (MACs OUI
//     de hipervisor + exactamente un host) = servidor con VMs tras ese AP.
//   - LLDP (Fase 2, contrato C2): si en un puerto multi-MAC se anuncia un
//     vecino LLDP cuya chassis-MAC está entre las aprendidas, el equipo que
//     multiplexa queda IDENTIFICADO → nodo "managed" (nombre/mgmt-ip/caps
//     anunciados). Es evidencia positiva, como la de hipervisor: aplica
//     también tras un AP (el uplink al gateway no puede promover porque su
//     MAC está excluida de las aprendidas). Además, cualquier vecino cuya
//     chassis-MAC sea un Device conocido puebla Device.Lldp. Sin datos LLDP
//     todo sigue exactamente como antes (inferred/hypervisor + reglas OUI).
// ---------------------------------------------------------------------------

// hypervisorOUI: prefijos de MAC de hipervisores conocidos.
var hypervisorOUI = []string{
	"BC:24:11", // Proxmox VE
	"00:50:56", // VMware (ESXi/Workstation)
	"00:0C:29", // VMware
	"00:15:5D", // Hyper-V
	"52:54:00", // KVM/QEMU
	"08:00:27", // VirtualBox
}

func isHypervisorMAC(mac string) bool {
	upper := strings.ToUpper(mac)
	for _, p := range hypervisorOUI {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
}

// inferTopology enriquece devices con Port/AttachTo e infiere los
// DistributionNode del FDB sondeado. `polled` lleva fdb (MAC→puerto, ya
// filtrado a lanN/wan por parseBridgeFdb) y brMac por router.
func inferTopology(polled map[string]*routerPolled, devices []Device) ([]Device, []DistributionNode) {
	byMAC := make(map[string]int, len(devices))
	for i, d := range devices {
		byMAC[d.MAC] = i
	}
	routerMACs := map[string]bool{}
	for _, p := range polled {
		if p.brMac != "" {
			routerMACs[p.brMac] = true
		}
	}

	var dists []DistributionNode
	// routers en orden determinista (el mapa polled no lo es)
	routerIDs := make([]string, 0, len(polled))
	for id := range polled {
		routerIDs = append(routerIDs, id)
	}
	sort.Strings(routerIDs)

	// Gateway con la misma prioridad que pickGateway (is_gateway → glinet →
	// primero): solo él puede anclar nodos "inferred".
	gatewayID := ""
	if gw := pickGatewayCfg(polled); gw != nil {
		gatewayID = gw.ID
	}
	// Identidades de routers para el matching LLDP tolerante (issue #252):
	// un vecino cuya mgmt-IP/nombre coincide con un router conocido identifica
	// el switch/AP aunque su chassis-ID no esté entre las MACs aprendidas.
	routers := routerIdentities(polled)

	for _, routerID := range routerIDs {
		p := polled[routerID]
		// invertir fdb: puerto → MACs (el WAN no es distribución LAN)
		byPort := map[string][]string{}
		for mac, port := range p.fdb {
			if port == "wan" {
				continue
			}
			byPort[port] = append(byPort[port], mac)
		}
		ports := make([]string, 0, len(byPort))
		for port := range byPort {
			ports = append(ports, port)
		}
		sort.Strings(ports)

		for _, port := range ports {
			var kept []string
			for _, mac := range byPort[port] {
				if routerMACs[mac] {
					continue
				}
				if idx, ok := byMAC[mac]; ok {
					d := devices[idx]
					if d.RouterID != routerID {
						continue
					}
					if d.Band != "cable" && d.Band != "—" {
						continue
					}
				}
				kept = append(kept, mac)
			}
			if len(kept) == 0 {
				continue
			}
			setPort := func() {
				for _, mac := range kept {
					if idx, ok := byMAC[mac]; ok && devices[idx].RouterID == routerID {
						devices[idx].Port = port
						devices[idx].PortLabel = portLabel(p, port)
					}
				}
			}
			if len(kept) == 1 {
				// cableado directo: puerto conocido, sin nodo de distribución
				setPort()
				continue
			}

			// puerto multi-MAC
			id := fmt.Sprintf("dist-%s-%s", routerID, port)
			var vmMACs, hostMACs []string
			for _, mac := range kept {
				if isHypervisorMAC(mac) {
					vmMACs = append(vmMACs, mac)
				} else {
					hostMACs = append(hostMACs, mac)
				}
			}
			if len(vmMACs) > 0 && len(hostMACs) == 1 {
				if hidx, ok := byMAC[hostMACs[0]]; ok && devices[hidx].RouterID == routerID {
					// Hipervisor con host identificado: evidencia clara, válida
					// también tras un AP (servidor con VMs colgando de ese AP).
					setPort()
					dists = append(dists, DistributionNode{
						ID: id, Kind: "hypervisor", RouterID: routerID, Port: port,
						PortLabel: portLabel(p, port),
						MacCount:  len(kept), HostDeviceID: devices[hidx].ID, Name: devices[hidx].Name,
					})
					// SPEC-65 D65-2: sellado de infra — host = hypervisor,
					// las MACs con OUI de hipervisor anidadas bajo él = ct.
					devices[hidx].Infra = "hypervisor"
					for _, mac := range vmMACs {
						if idx, ok := byMAC[mac]; ok && devices[idx].RouterID == routerID {
							devices[idx].AttachTo = devices[hidx].ID
							devices[idx].Infra = "ct"
						}
					}
					continue
				}
			}
			// LLDP: un vecino anunciado en este puerto cuya chassis-MAC está
			// entre las aprendidas identifica el switch/AP gestionado que
			// multiplexa → nodo "managed" (evidencia positiva, válida también
			// tras un AP; ver cabecera).
			if nb := findManagedNeighbor(p.lldp, port, kept, routers, routerID); nb != nil {
				setPort()
				dists = append(dists, DistributionNode{
					ID: id, Kind: "managed", RouterID: routerID, Port: port,
					PortLabel: portLabel(p, port),
					MacCount:  len(kept), Name: nb.displayName(), Ip: nb.Mgmt,
					Mac: nb.ChassisMac, Lldp: nb.info(),
				})
				// SPEC-65 D65-2: el vecino LLDP managed que existe como Device
				// queda sellado como managed-switch.
				if idx, ok := byMAC[nb.ChassisMac]; ok {
					devices[idx].Infra = "managed-switch"
				}
				for _, mac := range kept {
					// SPEC-CANON D2: la chassis-MAC del propio switch NO cuelga
					// de su propio nodo — su Device queda con attachTo al
					// router + port de uplink (el switch es Device Y nodo a la
					// vez, sin self-attach ni doble conteo).
					if mac == nb.ChassisMac {
						continue
					}
					if idx, ok := byMAC[mac]; ok && devices[idx].RouterID == routerID {
						devices[idx].AttachTo = id
					}
				}
				continue
			}
			if routerID != gatewayID && !polled[routerID].cfg.AgentOnly {
				// AP con puerto multi-MAC sin evidencia de hipervisor: es casi
				// seguro su uplink (aprende las MACs de toda la LAN). No se
				// afirma switch local ni se anota puerto: los clientes quedan
				// "sin evidencia" y el frontend los ancla al gateway.
				continue
			}
			// Gateway, OUI heterogéneo (o hipervisor ambiguo sin host claro):
			// algo multiplexa ese puerto → nodo inferido colgando del gateway.
			setPort()
			dn := DistributionNode{
				ID: id, Kind: "inferred", RouterID: routerID, Port: port, PortLabel: portLabel(p, port), MacCount: len(kept),
			}
			if routerID == gatewayID {
				for _, rp := range polled {
					if !rp.cfg.AgentOnly {
						continue
					}
					swMacs := map[string]bool{}
					for mac := range rp.fdb {
						swMacs[mac] = true
					}
					allKnown := true
					for _, mac := range kept {
						if !swMacs[mac] {
							allKnown = false
							break
						}
					}
					if allKnown && len(kept) > 0 {
						dn.Name = rp.cfg.Name
						dn.Ip = rp.cfg.Host
						break
					}
				}
			}
			dists = append(dists, dn)
			for _, mac := range kept {
				if idx, ok := byMAC[mac]; ok && devices[idx].RouterID == routerID {
					devices[idx].AttachTo = id
				}
			}
		}
	}

	// Post-paso LLDP: un vecino cuya chassis-MAC es un Device conocido lo
	// identifica (Device.Lldp), en el puerto que sea y aunque no multiplexe.
	for _, routerID := range routerIDs {
		for i := range polled[routerID].lldp {
			nb := &polled[routerID].lldp[i]
			if nb.ChassisMac == "" {
				continue
			}
			if idx, ok := byMAC[nb.ChassisMac]; ok {
				devices[idx].Lldp = nb.info()
			}
		}
	}
	return devices, dists
}

// portLabel devuelve la etiqueta LuCI del puerto (issue #258) si el router
// la define ("" si no existe): la app la usa como nombre preferente.
func portLabel(p *routerPolled, port string) string {
	if p == nil || p.luci == nil {
		return ""
	}
	return p.luci.PortLabels[port]
}

// routerIdentity: datos de un router de la config para casar un vecino LLDP
// aunque su chassis-ID no coincida con la bridge MAC. En OpenWrt/GLuON lldpd
// suele anunciar como chassis la MAC de la primera interfaz (p.ej. eth0/WAN),
// que NO es la br-lan que aprende el FDB del vecino: el matching exclusivo por
// chassis-MAC dejaba links sin descubrir con lldpd instalado en todo (issue #252).
type routerIdentity struct {
	ID, Name, Host string
	BrMac          string
}

// routerIdentities: identidades de todos los routers sondeados, en orden
// determinista (el mapa polled no lo es).
func routerIdentities(polled map[string]*routerPolled) []routerIdentity {
	ids := make([]string, 0, len(polled))
	for id := range polled {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]routerIdentity, 0, len(ids))
	for _, id := range ids {
		p := polled[id]
		out = append(out, routerIdentity{ID: id, Name: p.cfg.Name, Host: p.cfg.Host, BrMac: p.brMac})
	}
	return out
}

// neighborIsRouter: el vecino LLDP identifica a un router conocido de la
// config. Coincide por cualquiera de (prioridad): chassis-MAC = bridge MAC
// (matching original, SPEC C2), mgmt-IP = Host, o nombre del chasis ≈ nombre
// del router (case-insensitive). Los dos últimos son el fix tolerante al
// chassis-ID distinto de la br-lan (issue #252). selfID se excluye (un router
// no es su propio vecino). nil si no hay coincidencia.
func neighborIsRouter(nb *LldpNeighbor, routers []routerIdentity, selfID string) *routerIdentity {
	if nb == nil {
		return nil
	}
	for i := range routers {
		r := &routers[i]
		if r.ID == selfID {
			continue
		}
		if nb.ChassisMac != "" && r.BrMac != "" && strings.EqualFold(nb.ChassisMac, r.BrMac) {
			return r
		}
	}
	for i := range routers {
		r := &routers[i]
		if r.ID == selfID {
			continue
		}
		if nb.Mgmt != "" && r.Host != "" && nb.Mgmt == r.Host {
			return r
		}
	}
	for i := range routers {
		r := &routers[i]
		if r.ID == selfID {
			continue
		}
		if nb.Chassis != "" && r.Name != "" && strings.EqualFold(nb.Chassis, r.Name) {
			return r
		}
	}
	return nil
}

// findManagedNeighbor: vecino LLDP anunciado en `port` que identifica el
// equipo que multiplexa ese puerto multi-MAC (nil si no hay evidencia).
//  1. matching estricto: chassis-MAC entre las MACs aprendidas del puerto.
//  2. fallback #252: la identidad del vecino (mgmt-IP o nombre del chasis)
//     coincide con un router conocido de la config → ese router ES el switch/AP
//     que multiplexa, aunque su chassis-ID no esté en el FDB.
func findManagedNeighbor(neighbors []LldpNeighbor, port string, kept []string, routers []routerIdentity, selfID string) *LldpNeighbor {
	for i := range neighbors {
		nb := &neighbors[i]
		if nb.Port != port || nb.ChassisMac == "" {
			continue
		}
		for _, mac := range kept {
			if mac == nb.ChassisMac {
				return nb
			}
		}
	}
	for i := range neighbors {
		nb := &neighbors[i]
		if nb.Port != port {
			continue
		}
		if r := neighborIsRouter(nb, routers, selfID); r != nil {
			return nb
		}
	}
	return nil
}
