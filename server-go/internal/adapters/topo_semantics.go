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

import (
	"sort"
	"strings"
)

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
// topoParent (#1047/#1051): padre de uplink de una unidad de flota derivado
// de evidencia FDB (en qué equipo se aprendió su MAC y en qué puerto).
type topoParent struct {
	parent    string
	port      string // puerto del PADRE donde aprende la MAC del hijo
	childPort string // puerto del HIJO hacia el padre (del FDB del hijo)
}

// fleetFdbEvidence (#1051/#1186): para cada router de la flota, ¿en qué OTRO
// miembro se aprendió su MAC bridge y en qué puerto? Fuente directa del FDB
// de cada poller, sin pasar por la atribución a devices (que no existe en
// redes donde las unidades no aparecen como clientes).
//
// Resolución v2 (#1186): todos los miembros que aprenden la MAC de una unidad
// son candidatos a padre. El gateway aprende TODO detrás de los switches (su
// candidata es el artefacto de estrella plana), así que el padre es el
// candidato NO-gateway más profundo, con la profundidad propagándose del root
// hacia las hojas en pasadas (gw=0; switch colgado del gw=1; routers colgados
// del switch=2). Devuelve también el puerto del lado del hijo (en qué puerto
// de la propia unidad se aprende la MAC del padre), para cablear ambos
// extremos. Orden determinista: ids ordenados.
func fleetFdbEvidence(polled map[string]*routerPolled, gatewayID string, registered map[string]string, seedDepth map[string]int) map[string]topoParent {
	macs := map[string]string{}    // MAC bridge (upper) → router propietario
	idToMac := map[string]string{} // router id → MAC bridge (upper)
	// learned[sid] = qué MACs (upper) aprende sid y en qué puerto propio.
	learned := map[string]map[string]string{}
	ids := make([]string, 0, len(polled))
	for id, p := range polled {
		if p == nil {
			continue
		}
		ids = append(ids, id)
		lm := map[string]string{}
		for mac, port := range p.fdb {
			lm[strings.ToUpper(mac)] = port
		}
		learned[id] = lm
		// MAC registrada (routers.mac): unidades sin bridge MAC utilizable
		// (DSM: bridgeDetect cae en docker0 y no corre lldpd) eran invisibles
		// para la evidencia FDB y su único anclaje era un override manual.
		// La MAC de bridge manda si ambas existen y difieren en propietario.
		if m := strings.ToUpper(strings.TrimSpace(registered[id])); m != "" {
			if _, ok := macs[m]; !ok {
				macs[m] = id
			}
		}
		if p.brMac != "" {
			macs[strings.ToUpper(p.brMac)] = id
			idToMac[id] = strings.ToUpper(p.brMac)
		}
	}
	sort.Strings(ids)
	// candidatos[rid] = unidades que aprenden la MAC bridge de rid y el puerto
	// (del lado de la unidad que aprende) donde la vieron.
	candidates := map[string][]topoParent{}
	for _, sid := range ids {
		for ownerMac, port := range learned[sid] {
			rid, ok := macs[ownerMac]
			if !ok || rid == sid {
				continue
			}
			candidates[rid] = append(candidates[rid], topoParent{parent: sid, port: port})
		}
	}
	// Resolución en pasadas: el gateway (root, profundidad 0) no tiene padre;
	// el resto elige su padre entre sus candidatos strong: el NO-gateway con
	// mayor profundidad conocida (el gateway aprende TODO detrás de los
	// switches: su candidata es el artefacto de estrella plana). Guarda de
	// ciclos: un candidato que cuelga de rid (rid en su cadena de padres) no
	// puede ser el padre de rid. Estabiliza en ≤ N+1 pasadas.
	depth := map[string]int{gatewayID: 0}
	// Semilla de profundidades derivada de LLDP (el caller la pasa): un switch
	// de core cuya MAC de chasis nadie aprende no tiene candidato padre por
	// FDB y su profundidad quedaría desconocida para siempre, arrastrando a
	// las unidades cableadas en él al padre provisional del gateway.
	for id, d := range seedDepth {
		if id != gatewayID && d > 0 {
			depth[id] = d
		}
	}
	parent := map[string]topoParent{}
	isDescendant := func(rid, c string) bool {
		cur := c
		for i := 0; i <= len(ids); i++ {
			if cur == "" || cur == rid {
				return cur == rid
			}
			p, ok := parent[cur]
			if !ok {
				return false
			}
			cur = p.parent
		}
		return false
	}
	for iter := 0; iter <= len(ids); iter++ {
		changed := false
		for _, rid := range ids {
			if rid == gatewayID {
				continue
			}
			for _, c := range candidates[rid] {
				if c.parent == gatewayID {
					// provisional: solo si aún no tiene padre ninguno
					if _, has := parent[rid]; !has {
						parent[rid] = c
						depth[rid] = 1
						changed = true
					}
					continue
				}
				// #1373: el padre de rid es quien aprende a rid FUERA de su
				// propio root port (rid es downstream suyo). Un candidato que
				// aprende a rid EN su root port tiene a rid aguas ARRIBA (o es
				// lateral en L2 plana) y no puede ser su padre: sin esta guarda
				// la relajación por profundidad cuelga un switch gestionado de
				// sus propios APs (p. ej. switch16 <- rt4 cuando la física es
				// switch16 -> gateway por lan7).
				cRoot := ""
				if gwMac := idToMac[gatewayID]; gwMac != "" {
					cRoot = learned[c.parent][gwMac]
				}
				if cRoot != "" && c.port == cRoot {
					continue
				}
				if isDescendant(rid, c.parent) {
					continue
				}
				if d, known := depth[c.parent]; known && d+1 > depth[rid] {
					parent[rid] = c
					depth[rid] = d + 1
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	// Puerto del lado del hijo: en qué puerto de la propia unidad se aprende
	// la MAC bridge del padre.
	for rid, p := range parent {
		if lm, ok := learned[rid]; ok {
			if port, ok := lm[idToMac[p.parent]]; ok {
				p.childPort = port
				parent[rid] = p
			}
		}
	}
	return parent
}

// fleetLldpEvidence (#1279): uplinks resueltos POR LLDP con la regla del
// puerto raíz. El uplink de una unidad es el vecino router que vive en el
// puerto local donde la propia unidad aprende la MAC bridge del gateway
// (tránsito hacia el núcleo): un switch ve al gateway directo o al switch
// padre en ese puerto, un AP solo ve a su switch. Sin MAC de gateway
// disponible: un único candidato manda; con varios, se prefiere el switch
// gestionado (hacia el core) y si el empate persiste no hay evidencia
// (decide la FDB). El puerto es el del lado del PADRE (PortDesc que el
// padre anuncia); si el padre es una unidad sondeada, se rellena con el
// puerto local del padre desde SU vista LLDP (dos lados #1279). childPort es
// la boca del HIJO hacia el padre (su puerto local hacia él): la necesita el
// cálculo de velocidad cuando el padre no reporta la suya (#1471).
func fleetLldpEvidence(polled map[string]*routerPolled, gatewayID string) map[string]topoParent {
	routers := routerIdentities(polled)
	gwMac := ""
	if gw := polled[gatewayID]; gw != nil && gw.brMac != "" {
		gwMac = strings.ToUpper(gw.brMac)
	}
	ids := make([]string, 0, len(polled))
	for id := range polled {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	type lldpCand struct {
		parentID  string
		port      string // lado padre (anunciado por él)
		localPort string // puerto local del poller hacia él
		isSwitch  bool
		brMac     string // #1372: bridge MAC del candidato (corroboración FDB)
	}
	out := map[string]topoParent{}
	for _, rid := range ids {
		if rid == gatewayID {
			continue
		}
		p := polled[rid]
		if p == nil || len(p.lldp) == 0 {
			continue
		}
		var cands []lldpCand
		for i := range p.lldp {
			nb := &p.lldp[i]
			if nb.Port == "" {
				continue
			}
			// #1279 (reapertura): el Chassis sintetizado desde lldpLocPortDesc
			// es la anotación del admin en el puerto LOCAL, no identidad
			// remota: casarla con los nombres de la flota produjo padres
			// falsos en bucle (sw1<->sw2). Sigue valiendo para etiquetas;
			// aquí no cuenta como candidato.
			if nb.ChassisFromLocDesc {
				continue
			}
			ri := neighborIsRouter(nb, routers, rid)
			if ri == nil || ri.ID == rid {
				continue
			}
			cands = append(cands, lldpCand{
				parentID: ri.ID, port: nb.PortDesc, localPort: nb.Port,
				isSwitch: ri.Type == "managed-switch", brMac: strings.ToUpper(ri.BrMac),
			})
		}
		if len(cands) == 0 {
			continue
		}
		chosen := -1
		if gwMac != "" {
			rootPort, ok := p.fdb[gwMac]
			if ok {
				// #1372 (phantom guard): un switch que inunda la multicast
				// reservada 01:80:c2:00:00:0e (no 802.1AB-compliant) mete
				// vecinos de SEGUNDO salto por el MISMO puerto raíz, así que
				// "el primer candidato del root port" puede ser un phantom.
				// Corroboración FDB por MAC: el peer verdadero intercambia
				// tráfico real con nosotros y su bridge MAC está aprendida
				// en la FDB de ese puerto; un phantom puro (sin tráfico
				// directo) no. Solo se exige al desempatar varios candidatos
				// en el mismo puerto: con uno solo, la LLDP basta (la FDB
				// puede ir vacía o aún sin aprendizaje).
				var onRoot []int
				for i := range cands {
					if cands[i].localPort == rootPort {
						onRoot = append(onRoot, i)
					}
				}
				if len(onRoot) == 1 {
					chosen = onRoot[0]
				} else if len(onRoot) > 1 {
					var corr []int
					for _, i := range onRoot {
						if cands[i].brMac == "" {
							continue
						}
						if port, ok2 := p.fdb[cands[i].brMac]; ok2 && port == cands[i].localPort {
							corr = append(corr, i)
						}
					}
					if len(corr) == 1 {
						chosen = corr[0]
					} else {
						// Varios corroborados (LAN plana: todos hablan con
						// todos) o ninguno (FDB sin datos): switch gestionado
						// preferido entre los del root port (hacia el core).
						pool := corr
						if len(pool) == 0 {
							pool = onRoot
						}
						for _, i := range pool {
							if cands[i].isSwitch {
								chosen = i
								break
							}
						}
					}
				}
			}
		}
		if chosen == -1 && len(cands) == 1 {
			chosen = 0
		}
		if chosen == -1 {
			for i := range cands {
				if cands[i].isSwitch {
					chosen = i
					break
				}
			}
		}
		if chosen == -1 {
			continue
		}
		ev := topoParent{parent: cands[chosen].parentID, port: cands[chosen].port, childPort: cands[chosen].localPort}
		if ev.port == "" {
			// El padre no anuncia su puerto (Omada sin PortDesc remoto):
			// si el padre es una unidad sondeada, su propia vista LLDP da
			// el puerto local por el que nos ve.
			if pp := polled[ev.parent]; pp != nil {
				for i := range pp.lldp {
					if pp.lldp[i].ChassisFromLocDesc {
						continue
					}
					if ri := neighborIsRouter(&pp.lldp[i], routers, ev.parent); ri != nil && ri.ID == rid {
						ev.port = pp.lldp[i].Port
						break
					}
				}
			}
		}
		out[rid] = ev
	}
	breakEvidenceCycles(out)
	return out
}

// breakEvidenceCycles: defensa en profundidad (#1279, reapertura). La
// resolución de uplinks nunca debe producir un ciclo entre unidades de
// flota: si la evidencia LLDP lo genera (p.ej. dos unidades con un único
// candidato cruzado, "un candidato manda"), se elimina a los miembros del
// ciclo del mapa y BuildTopoSemantics degrada esos uplinks al siguiente
// nivel de evidencia (device/FDB), que resuelve con guarda de descendencia.
func breakEvidenceCycles(ev map[string]topoParent) {
	const (
		unvisited = iota
		visiting
		done
	)
	state := map[string]int{}
	inCycle := map[string]bool{}
	ids := make([]string, 0, len(ev))
	for id := range ev {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, start := range ids {
		if state[start] != unvisited {
			continue
		}
		var chain []string
		for cur := start; ; {
			if state[cur] == visiting {
				// Ciclo: desde la primera aparición de cur en la cadena.
				for i, n := range chain {
					if n == cur {
						for _, m := range chain[i:] {
							inCycle[m] = true
						}
						break
					}
				}
				break
			}
			if state[cur] == done {
				break
			}
			e, ok := ev[cur]
			if !ok {
				break // sin entrada: la cadena de padres se acaba aquí
			}
			state[cur] = visiting
			chain = append(chain, cur)
			cur = e.parent
		}
		for _, n := range chain {
			state[n] = done
		}
	}
	for rid := range inCycle {
		delete(ev, rid)
	}
}

func BuildTopoSemantics(routers []Router, devices []Device, wg WireGuardStats, dists []DistributionNode, wanGateway string, fdbEvidence map[string]topoParent, lldpEvidence map[string]topoParent, overrideUplinks map[string]topoParent) *TopoSemantics {
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
	// switchRouter: managed switch units (RoleBadge "SW"), e.g. a fleet switch
	// or an agent-only NAS. A wired client with no FDB port attributed to an AP
	// is not trustworthy (the AP bridge learns MACs that merely transit its
	// uplink), so rule 2-Ago-2026 anchors it to the gateway. But a switch/NAS
	// reports its own downstream clients first-party (e.g. a Docker container
	// behind the NAS, portless because its veth is in the denylist), so its
	// RouterID is honored to stay coherent with the unit's client count.
	switchRouter := map[string]bool{}
	for _, r := range routerNodes {
		if r.RoleBadge == "SW" {
			switchRouter[r.ID] = true
		}
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
	// #1047: las unidades de la flota (routers/switches gestionados) también
	// aparecen como Device porque su brMac tiene lease, pero el nodo del
	// router ya las representa: se excluyen de los chips y su atribución
	// FDB (dónde se vio la MAC y en qué puerto) queda como evidencia para
	// anclar el uplink al padre REAL (p. ej. ap1 cuelga de sw1, no del
	// gateway) aunque no haya LLDP.
	routerMacs := map[string]string{} // MAC(upper) → router id
	for _, r := range routers {
		if r.MAC != "" {
			routerMacs[strings.ToUpper(r.MAC)] = r.ID
		}
	}
	uplinkEvidence := map[string]topoParent{}
	// uplinkSource (#1325): qué capa fijó el padre de cada unidad de flota,
	// para exponer la evidencia real por enlace (el front ya no decide el tag
	// "LLDP" por node.router.lldp). Valores canónicos: "device"/"fdb"/"lldp"/
	// "override". Vacío = fallback al gateway (sin evidencia, sin tag).
	uplinkSource := map[string]string{}
	filtered := online[:0]
	for _, d := range online {
		if rid, ok := routerMacs[strings.ToUpper(d.MAC)]; ok {
			if rid != d.RouterID {
				if _, seen := uplinkEvidence[rid]; !seen {
					uplinkEvidence[rid] = topoParent{parent: d.RouterID, port: d.Port}
					uplinkSource[rid] = "device"
				}
			}
			continue
		}
		filtered = append(filtered, d)
	}
	online = filtered
	// #1051: la evidencia FDB directa del poller cubre las redes donde las
	// unidades de flota NO aparecen como devices; la atribución de device
	// (attachTo/override) manda cuando existe.
	for rid, ev := range fdbEvidence {
		if _, ok := uplinkEvidence[rid]; !ok {
			uplinkEvidence[rid] = ev
			uplinkSource[rid] = "fdb"
		}
	}
	// #1279: el LLDP es ground truth (dos equipos que se anuncian por el
	// cable, con regla de puerto raíz contra la FDB propia) y manda SOBRE la
	// inferencia FDB/device. Se aplica al final: pisa lo inferido cuando hay
	// vecino LLDP resuelto; donde no lo hay, quedan device/FDB como antes.
	for rid, ev := range lldpEvidence {
		if ev.parent != rid {
			uplinkEvidence[rid] = ev
			uplinkSource[rid] = "lldp"
		}
	}
	// #1321: el attach manual (#142) sobre la MAC bridge de una unidad de
	// flota es la voluntad del admin y manda sobre TODO. Si el override no
	// fija puerto, se conserva el que la inferencia hubiera dado.
	for rid, ev := range overrideUplinks {
		if ev.parent == rid {
			continue
		}
		if ev.port == "" {
			if old, ok := uplinkEvidence[rid]; ok {
				ev.port = old.port
			}
		}
		uplinkEvidence[rid] = ev
		uplinkSource[rid] = "override"
	}
	// #1374: un distnode inferred que es la sombra no gestionada de un switch
	// gestionado de la flota se absorbe: si el uplink del switch apunta al
	// mismo (router, puerto) que ocupa el distnode, ambos nodos son la misma
	// caja física (un puerto = un cable). El switch enlaza directo con el
	// padre (el salto #1060 deja de encontrar distnode) y los clientes del
	// distnode pasan a colgar del switch, su posición física real. Solo
	// inferred: los managed tienen identidad propia (chassis MAC) y su dedup
	// es el de #1325/D1.
	switchUnit := map[string]bool{}
	for _, r := range routers {
		if r.Role == "Switch" {
			switchUnit[r.ID] = true
		}
	}
	if len(switchUnit) > 0 {
		absorbed := map[string]string{} // distnode -> switch que lo absorbe
		// kept es un slice NUEVO: filtrar in situ (dists[:0]) reescribe el
		// backing array del caller, que conserva su len original y acaba con
		// el último elemento duplicado (#1378).
		kept := make([]DistributionNode, 0, len(dists))
		for _, dn := range dists {
			hub := ""
			if dn.Kind == "inferred" {
				for rid := range switchUnit {
					if ev, ok := uplinkEvidence[rid]; ok && ev.parent == dn.RouterID && ev.port != "" && ev.port == dn.Port {
						hub = rid
						break
					}
				}
			}
			if hub != "" {
				absorbed[dn.ID] = hub
				continue
			}
			kept = append(kept, dn)
		}
		if len(absorbed) > 0 {
			dists = kept
			sem.AbsorbedDists = make([]string, 0, len(absorbed))
			for id := range absorbed {
				sem.AbsorbedDists = append(sem.AbsorbedDists, id)
			}
			sort.Strings(sem.AbsorbedDists)
			for i := range online {
				// Re-anclar al switch SOLO los clientes atribuidos a él: los
				// atribuidos a otro router (p. ej. equipos de un switch NO
				// gestionado que cuelga del gateway y cuyo tráfico el switch
				// aprende en su root port) no son suyos; al quitar el blob
				// quedan con su router, su posición física.
				if sw, ok := absorbed[online[i].AttachTo]; ok && online[i].RouterID == sw {
					online[i].AttachTo = sw
				}
			}
		}
	}
	// #1042: el equipo aguas arriba del gateway (módem/ONT del ISP) se
	// descubre por ARP como un cliente más, pero vive en el lado WAN: si su
	// IP coincide con la puerta de enlace WAN se representa bajo el nodo
	// Internet, no como cliente LAN. Se excluye de anillos y enlaces.
	wanPeerID := ""
	if wanGateway != "" {
		for _, d := range online {
			if d.IP != "" && d.IP == wanGateway {
				wanPeerID = d.ID
				break
			}
		}
	}
	if wanPeerID == "" {
		// #1485: con PPPoE la puerta de enlace WAN es el BRAS, así que el
		// ONT/módem del primer salto nunca casa por IP. Su evidencia es el ARP
		// aprendido en el puerto WAN del gateway (Device.WanSide); si hay
		// varios, gana la MAC más baja para que el resultado sea estable.
		bestMac := ""
		for _, d := range online {
			if !d.WanSide {
				continue
			}
			m := strings.ToLower(d.MAC)
			if wanPeerID == "" || m < bestMac {
				wanPeerID, bestMac = d.ID, m
			}
		}
	}
	if wanPeerID != "" {
		filtered := online[:0]
		for _, d := range online {
			if d.ID != wanPeerID {
				filtered = append(filtered, d)
			}
		}
		online = filtered
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
		// A wired client with no FDB port is force-anchored to the gateway unless
		// its RouterID resolves to a managed switch. Attribution to a wireless AP
		// is not trustworthy without port evidence (the AP bridge learns MACs
		// that merely transit its uplink — rule 2-Ago-2026), but a switch/NAS
		// reports its own downstream clients first-party (e.g. a Docker container
		// behind the NAS, portless because its veth is in the port denylist);
		// honoring that RouterID keeps the chip coherent with the unit's client
		// count. Mirrors the model.ts hubOf guard.
		if d.Band == "cable" && d.Port == "" && !switchRouter[d.RouterID] {
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
	if wanPeerID != "" {
		sem.WanPeer = wanPeerID
		sem.Links = append(sem.Links, TopoLink{From: "internet", To: wanPeerID, Kind: "wan-peer"})
	}
	for _, ap := range nonGateway {
		parent := gateway.ID
		port := ""
		if ap.Lldp != nil {
			port = ap.Lldp.PortDesc
		}
		// #1047: la evidencia FDB (MAC del equipo aprendida en el puerto de
		// otro miembro de la flota) manda sobre el fallback al gateway; el
		// puerto reportado es el del padre (switch), no del gateway.
		if ev, ok := uplinkEvidence[ap.ID]; ok && ev.parent != ap.ID {
			parent = ev.parent
			if ev.port != "" {
				port = ev.port
			}
		}
		// #1060: si el puerto de la evidencia tiene un distnode (inferred o
		// managed) en ese router, el uplink sale del CÍRCULO, no del router:
		// gateway -> (switch inferido, lan1) -> AP. Sin esto el enlace salta
		// por encima del círculo y el cableado real no se reconoce.
		from := parent
		if port != "" {
			for _, dn := range dists {
				if dn.RouterID == parent && (dn.Kind == "inferred" || dn.Kind == "managed") && dn.Port == port {
					from = dn.ID
					break
				}
			}
		}
		sem.Links = append(sem.Links, TopoLink{From: from, To: ap.ID, Kind: "uplink", Port: port, Evidence: uplinkSource[ap.ID]})
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
	// túneles WG: peers activos con coordenada canónica (máx 4). #1441: los
	// urgentes primero, igual que el orden de slots del mapa (urgentes
	// activos → activos → urgentes caídos → caídos): los 4 enlaces del
	// servidor tienen que ser exactamente los 4 nodos que el front dibuja.
	n := 0
	for pass := 0; pass < 2 && n < maxTopoPeerChips; pass++ {
		wantUrgent := pass == 0
		for _, p := range wg.Peers {
			if !p.Active || p.Urgent != wantUrgent {
				continue
			}
			if n >= maxTopoPeerChips {
				break
			}
			sem.Links = append(sem.Links, TopoLink{From: "peer-" + p.ID, To: "internet", Kind: "wg"})
			n++
		}
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
