// topo_semantics.go — Builder del modelo SEMÁNTICO de topología (SPEC-65
// D65-3), compartido demo/live. Porta a Go la lógica de asignación de
// anillos y enlaces de app/src/components/topology/model.ts
// (buildTopologyModel), SIN geometría: nada de coordenadas, radios, paths
// SVG ni flujo de paquetes — eso queda en la app.
//
// Paridad con model.ts (canon demo fijada en topo_semantics_test.go):
//   - El switch gestionado existe como Device Y como distnode managed: se
//     excluye de chips/enlaces cualquier Device cuya MAC coincida con la
//     chassis-MAC de un distnode managed (D1).
//   - Anclaje al gateway: cableado SIN evidencia (ni attachTo ni puerto FDB)
//     cuelga del gateway, no de su AP.
//   - Solo los distnodes inferred|managed son hubs de enlaces "dist"; el
//     hipervisor enlaza vía su Device host (sus CTs/VMs cuelgan del host).
//   - Peers WG: los activos que exceden las 4 coordenadas canónicas no
//     generan enlace (la app los agrupa en su chip "+N" de peers).
package adapters

import "strings"

// Capacidad de los anillos canónicos de model.ts (GATEWAY_RINGS 8+12+16+24,
// AP_RINGS 8+14+18): el límite de chips visibles por anillo que aplica la app.
// HiddenPeers = clientes del anillo - visibles con este mismo límite.
// (9-Ago-2026: subido desde 13/20 — el gateway con ~60 clientes ocultaba
// casi todo; el resolver de colisiones mantiene 0 solapes con anillos densos.)
const (
	topoGatewayRingCap = 60
	topoAPRingCap      = 40
)

// maxTopoPeerChips: coordenadas canónicas de peers WG en model.ts (4); los
// activos que exceden no se trazan como túnel propio.
const maxTopoPeerChips = 4

// BuildTopoSemantics deriva enlaces, anillos y peers ocultos del mismo bundle
// que hoy consume la app (routers, devices, wireguard, distributionNodes).
func BuildTopoSemantics(routers []Router, devices []Device, wg WireGuardStats, dists []DistributionNode) *TopoSemantics {
	sem := &TopoSemantics{Links: []TopoLink{}, Rings: map[string][]string{}}
	if len(routers) == 0 {
		return sem
	}

	// Gateway y APs/switches como model.ts: roleBadge "Principal" → gateway
	// (si no, el primero); el resto son routers (APs y switches gestionados).
	// Sin límite de 3 APs — el frontend decide cuántos dibujar con coordenadas
	// canónicas y cuáles como "extra" (switches, APs adicionales).
	gateway := routers[0]
	for _, r := range routers {
		if r.RoleBadge == "Principal" {
			gateway = r
			break
		}
	}
	nonGateway := make([]Router, 0, len(routers)-1)
	for _, r := range routers {
		if r.ID != gateway.ID {
			nonGateway = append(nonGateway, r)
		}
	}
	routerNodes := append([]Router{gateway}, nonGateway...)
	inRouter := map[string]bool{}
	for _, r := range routerNodes {
		inRouter[r.ID] = true
	}

	// D1: los Devices cuya MAC es la chassis-MAC de un distnode managed se
	// representan SOLO como nodo managed (sin chip ni enlaces propios).
	managedMacs := map[string]bool{}
	for _, n := range dists {
		if n.Kind == "managed" && n.Mac != "" {
			managedMacs[strings.ToUpper(n.Mac)] = true
		}
	}
	online := make([]Device, 0, len(devices))
	for _, d := range devices {
		if !d.Online || managedMacs[strings.ToUpper(d.MAC)] {
			continue
		}
		online = append(online, d)
	}
	deviceByID := map[string]Device{}
	for _, d := range online {
		deviceByID[d.ID] = d
	}
	distByID := map[string]DistributionNode{}
	for _, n := range dists {
		distByID[n.ID] = n
	}

	// hubOf: attachTo (si resuelve a router/distnode/device conocido) o su
	// router; un cableado SIN evidencia (ni attachTo ni puerto FDB) se ancla
	// al GATEWAY (regla 2-Ago-2026 de model.ts).
	hubOf := func(d Device) string {
		if d.AttachTo != "" {
			if inRouter[d.AttachTo] {
				return d.AttachTo
			}
			if _, ok := distByID[d.AttachTo]; ok {
				return d.AttachTo
			}
			if _, ok := deviceByID[d.AttachTo]; ok {
				return d.AttachTo
			}
		}
		if d.Band == "cable" && d.Port == "" {
			return gateway.ID
		}
		return d.RouterID
	}

	// deviceHubs: dispositivos con hijos propios (switch gestionado,
	// hipervisor), en orden de descubrimiento (orden del dataset, como el
	// Set de JS). hypervisorHosts: hosts con distnode kind=hypervisor.
	var deviceHubOrder []string
	deviceHubs := map[string]bool{}
	for _, d := range online {
		if d.AttachTo == "" || deviceHubs[d.AttachTo] {
			continue
		}
		if _, ok := deviceByID[d.AttachTo]; ok {
			deviceHubs[d.AttachTo] = true
			deviceHubOrder = append(deviceHubOrder, d.AttachTo)
		}
	}
	hypervisorHosts := map[string]bool{}
	for _, n := range dists {
		if n.Kind == "hypervisor" && n.HostDeviceID != "" {
			hypervisorHosts[n.HostDeviceID] = true
		}
	}

	wiredLink := func(from string, d Device) {
		sem.Links = append(sem.Links, TopoLink{From: from, To: d.ID, Kind: "wired", Port: d.Port})
	}

	// -- enlaces (mismo orden que model.ts) --------------------------------
	sem.Links = append(sem.Links, TopoLink{From: "internet", To: gateway.ID, Kind: "wan"})
	for _, ap := range nonGateway {
		l := TopoLink{From: gateway.ID, To: ap.ID, Kind: "uplink"}
		if ap.Lldp != nil {
			l.Port = ap.Lldp.PortDesc // puerto del uplink en el gateway (C2)
		}
		sem.Links = append(sem.Links, l)
	}
	// router → distnode (solo inferred|managed son hubs propios en el mapa).
	// En una cadena LLDP switch→switch (issue #300) el distnode cuelga de su
	// Parent (otro distnode) en vez del router.
	for _, rn := range routerNodes {
		for _, dn := range dists {
			if dn.RouterID != rn.ID || (dn.Kind != "inferred" && dn.Kind != "managed") {
				continue
			}
			from := rn.ID
			if dn.Parent != "" {
				from = dn.Parent
			}
			sem.Links = append(sem.Links, TopoLink{From: from, To: dn.ID, Kind: "dist", Port: dn.Port})
		}
	}
	// cableados directos del router (sin los device-hubs, que enlazan después)
	for _, rn := range routerNodes {
		for _, d := range online {
			if d.Band != "cable" || deviceHubs[d.ID] || hubOf(d) != rn.ID {
				continue
			}
			wiredLink(rn.ID, d)
		}
	}
	// hijos cableados de device-hubs NO hipervisor (los CTs van en su fase)
	for _, hubID := range deviceHubOrder {
		if hypervisorHosts[hubID] {
			continue
		}
		for _, d := range online {
			if d.Band != "cable" || hubOf(d) != hubID {
				continue
			}
			wiredLink(hubID, d)
		}
	}
	// el cable del propio hub (chip con hijos; el hipervisor entre ellos)
	for _, hubID := range deviceHubOrder {
		hub := deviceByID[hubID]
		h := hubOf(hub)
		// Si el hub cuelga de un distnode inferred|managed, su cable ya lo
		// genera el bucle de hijos de distnodes (más abajo): no duplicar.
		// (Caso issue #142: host con CTs anidados por override que cuelga de
		// un distnode inferred — el host es device-hub Y cliente del distnode.)
		if dn, ok := distByID[h]; ok && (dn.Kind == "inferred" || dn.Kind == "managed") {
			continue
		}
		wiredLink(h, hub)
	}
	// hijos cableados de distnodes (abanico alrededor del círculo)
	for _, rn := range routerNodes {
		for _, dn := range dists {
			if dn.RouterID != rn.ID || (dn.Kind != "inferred" && dn.Kind != "managed") {
				continue
			}
			for _, d := range online {
				if d.Band != "cable" || hubOf(d) != dn.ID {
					continue
				}
				wiredLink(dn.ID, d)
			}
		}
	}
	// CTs/VMs anidados bajo el host hipervisor (línea desde el host)
	for _, dn := range dists {
		if dn.Kind != "hypervisor" || dn.HostDeviceID == "" {
			continue
		}
		if _, ok := deviceByID[dn.HostDeviceID]; !ok {
			continue
		}
		for _, d := range online {
			if hubOf(d) != dn.HostDeviceID {
				continue
			}
			wiredLink(dn.HostDeviceID, d)
		}
	}
	// túneles WG: peers activos con coordenada canónica (máx 4)
	n := 0
	for _, p := range wg.Peers {
		if !p.Active {
			continue
		}
		if n >= maxTopoPeerChips {
			break
		}
		sem.Links = append(sem.Links, TopoLink{From: "peer-" + p.ID, To: "internet", Kind: "wg"})
		n++
	}

	// -- anillos por router --------------------------------------------------
	// Anillo = chips que cuelgan DIRECTAMENTE del router (cableados directos,
	// incluidos los device-hubs, y wifi). Orden SPEC-65: cableados primero,
	// luego por banda 5GHz/2.4GHz, estable (orden del dataset).
	for _, rn := range routerNodes {
		var wired, g5, g24, other []string
		for _, d := range online {
			if hubOf(d) != rn.ID {
				continue
			}
			switch d.Band {
			case "cable":
				wired = append(wired, d.ID)
			case "5 GHz":
				g5 = append(g5, d.ID)
			case "2.4 GHz":
				g24 = append(g24, d.ID)
			default:
				other = append(other, d.ID)
			}
		}
		ring := make([]string, 0, len(wired)+len(g5)+len(g24)+len(other))
		ring = append(ring, wired...)
		ring = append(ring, g5...)
		ring = append(ring, g24...)
		ring = append(ring, other...)
		if len(ring) == 0 {
			continue
		}
		sem.Rings[rn.ID] = ring
		cap := topoAPRingCap
		if rn.ID == gateway.ID {
			cap = topoGatewayRingCap
		}
		if hidden := len(ring) - cap; hidden > 0 {
			if sem.HiddenPeers == nil {
				sem.HiddenPeers = map[string]int{}
			}
			sem.HiddenPeers[rn.ID] = hidden
		}
	}
	return sem
}
