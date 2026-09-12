// proxmox.go — integración read-only con Proxmox VE (issue #561).
//
// El problema que resuelve: la relación CT→host NO es deducible del tráfico
// L2 (un puerto del router mezcla dispositivos), así que la heurística de
// topología no puede sellar hypervisor/ct en redes reales. La fuente de
// verdad vive en el cluster PVE, que se consulta con un token de SOLO LECTURA
// (GET únicamente; Sys.Audit + VM.Audit).
//
// Diseño:
//   - Config en kv (proxmox_url/token_id/token_secret), admin por API/UI.
//   - Inventario cacheado (TTL): cluster/resources (VM→node en 1 llamada) +
//     config por VM (N+1) para sacar las MACs de cada CT/VM.
//   - Sellado en buildDevices (tras inferTopology): el device cuya MAC es la
//     de un CT/VM se marca infra=ct y cuelga del device HOST (infra=hypervisor)
//     cuyo MAC es la del nodo físico. El host se identifica porque su MAC
//     aparece como la del nodo en el inventario (ver pveHostMAC).
//
// Si la integración no está configurada o falla, todo sigue igual (no-op).
package adapters

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/pve"
)

// pveInventoryTTL: refresco del inventario (resources + MACs). Un cluster
// pequeño (~10-20 VMs) cuesta N+1 GETs; cada 5 min es razonable.
const pveInventoryTTL = 5 * time.Minute

// pveHostMAC: la MAC del host físico de un nodo. No viene en cluster/resources
// (que lista VMs/CTs, no interfaces del nodo). La resolvemos consultando la
// config de la primera VM running de ese nodo NO nos sirve (es la MAC de la
// VM). En su lugar usamos el hecho de que el HOST (citadel-01) aparece como
// device de NetPulse por su propia MAC de gestión, y el nodo PVE se llama
// igual que ese device ("citadel-01"): el sellado cruza por NOMBRE del device
// == nombre del nodo como fallback, y por MAC de VM para los CTs.
type pveInventory struct {
	// ctByMAC: MAC (upper ':') → datos del CT/VM que la usa.
	ctByMAC map[string]pveVM
	// nodes: nodos de todas las instancias (#764 multi-endpoint). La clave
	// interna de los mapas es "instancia|nodo" para que dos clusters con
	// nodos homónimos no se pisen.
	nodes map[string]pveNode
	// nodeIPs: "instancia|nodo" → IP del bridge vmbr0. Permite casar el
	// device HOST por IP cuando NetPulse no conoce su nombre.
	nodeIPs map[string]string
}

type pveVM struct {
	Name     string // hostname del CT/VM (webs, pbs…)
	Node     string // nodo que lo ejecuta (citadel-01)
	Type     string // "lxc" | "qemu"
	Instance string // instancia Proxmox a la que pertenece (#764)
}

// pveNode: un nodo PVE con su instancia de origen.
type pveNode struct {
	Instance string
	Node     string
}

// nodeKey: clave compuesta de los mapas del inventario.
func nodeKey(instance, node string) string { return instance + "|" + node }

// pveInstClient: cliente PVE asociado a una instancia configurada.
type pveInstClient struct {
	inst pve.Instance
	c    *pve.Client
}

// pveClientsCached: clientes de TODAS las instancias configuradas (#764),
// recreados si la lista cambió (patrón AdGuard). Sin instancias → nil.
func (l *Live) pveClientsCached() []pveInstClient {
	if l.db == nil {
		return nil
	}
	instances := pve.LoadInstances(l.db.DB)
	key := ""
	for _, in := range instances {
		key += in.ID + "|" + in.URL + "|" + in.TokenID + "|" + in.Secret + "\n"
	}
	if key == "" {
		l.pveClients = nil
		l.pveKey = ""
		return nil
	}
	if l.pveKey != key {
		clients := make([]pveInstClient, 0, len(instances))
		for _, in := range instances {
			clients = append(clients, pveInstClient{inst: in, c: pve.NewClient(in.Config)})
		}
		l.pveClients = clients
		l.pveKey = key
	}
	return l.pveClients
}

// pveInventoryCached: inventario PVE (VM→MAC→node) con TTL. Devuelve nil si
// no configurado o si la consulta falla (no-op, no rompe el overview).
func (l *Live) pveInventoryCached() *pveInventory {
	clients := l.pveClientsCached()
	if len(clients) == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pveInv != nil && time.Since(l.pveInvAt) < pveInventoryTTL {
		return l.pveInv
	}
	inv := l.fetchPveInventory(clients)
	if inv == nil {
		return l.pveInv // conserva el último bueno si la consulta falla
	}
	l.pveInv = inv
	l.pveInvAt = time.Now()
	return inv
}

// fetchPveInventory: consulta cluster/resources + config de cada VM/CT
// running de TODAS las instancias (#764) y fusiona el inventario. Best-effort
// por instancia: una instancia caída no invalida las demás.
func (l *Live) fetchPveInventory(clients []pveInstClient) *pveInventory {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{},
		nodes:   map[string]pveNode{},
		nodeIPs: map[string]string{},
	}
	for _, ic := range clients {
		resources, err := ic.c.ClusterResources(ctx)
		if err != nil {
			log.Printf("[netpulse:pve] %s cluster/resources: %v", ic.inst.ID, err)
			continue
		}
		for _, r := range resources {
			if r.Type == "node" {
				// El nombre del nodo viene en `node`, no en `name` (que los
				// nodos dejan vacío en cluster/resources).
				if r.Node != "" {
					key := nodeKey(ic.inst.ID, r.Node)
					inv.nodes[key] = pveNode{Instance: ic.inst.ID, Node: r.Node}
					// IP del host (vmbr0) para casar el device HOST por IP.
					if ip, err := ic.c.NodeIP(ctx, r.Node); err == nil && ip != "" {
						inv.nodeIPs[key] = ip
					} else if err != nil {
						log.Printf("[netpulse:pve] %s nodeip %s: %v", ic.inst.ID, r.Node, err)
					}
				}
				continue
			}
			if r.Type != "lxc" && r.Type != "qemu" {
				continue
			}
			if r.Node == "" || r.VMID == 0 {
				continue
			}
			if r.Status == "stopped" {
				continue // sin tráfico → no está en la red; no aporta MAC
			}
			cfg, err := ic.c.VMConfig(ctx, r.Node, r.Type, r.VMID)
			if err != nil {
				log.Printf("[netpulse:pve] %s config %s/%d: %v", ic.inst.ID, r.Type, r.VMID, err)
				continue
			}
			for _, mac := range pve.MACsOfConfig(cfg) {
				inv.ctByMAC[mac] = pveVM{Name: r.Name, Node: r.Node, Type: r.Type, Instance: ic.inst.ID}
			}
		}
	}
	if len(inv.ctByMAC) == 0 && len(inv.nodes) == 0 {
		return nil // sin VMs corriendo ni nodos legibles en ninguna instancia
	}
	return inv
}

// sealProxmoxInfra aplica el inventario PVE sobre los devices ya construidos
// (tras inferTopology): el device cuyo MAC es la de un CT/VM se marca
// infra=ct y cuelga del device HOST del nodo (infra=hypervisor).
//
// El host se casa por nombre del device == nombre del nodo (p. ej. el device
// "citadel-01"). Esto resuelve el caso real donde el host PVE está en la LAN
// con su nombre y sus CTs (MACs BC:24:11 o locally-administered) ya visibles
// como devices sin sellar.
//
// Además devuelve los distnodes kind=hypervisor de cada host PVE (con
// Source="proxmox"): sin ellos el frontend no usa la maquinaria de
// hipervisor (grid de CTs bajo el host, badge +N, enlaces host→CT) y los CTs
// quedan como un abanico de chips normales. Los hosts que ya tengan un
// distnode hypervisor inferido por L2 no se duplican.
func (l *Live) sealProxmoxInfra(devices []Device, dists []DistributionNode) []DistributionNode {
	inv := l.pveInventoryCached()
	if inv == nil {
		return dists
	}
	return applyPVEInfra(devices, dists, inv)
}

// applyPVEInfra sella devices con un inventario dado (función pura, testeable
// sin red ni kv). Ver sealProxmoxInfra para el diseño.
func applyPVEInfra(devices []Device, dists []DistributionNode, inv *pveInventory) []DistributionNode {
	if inv == nil || len(inv.ctByMAC) == 0 {
		return dists
	}
	// Índices. El host de un nodo se casa por IP del nodo (vmbr0, la que da
	// el cluster) con prioridad, y por nombre del device como fallback. Un
	// host físico con dos NICs aparece como dos devices (p. ej. citadel-01
	// con .100 vmbr0 y .243 de gestión): el device con la IP del nodo es el
	// host correcto (online); el otro (nombre coincidente pero IP distinta)
	// NO debe ganar. Con multi-instancia (#764) las claves son compuestas
	// "instancia|nodo": dos clusters pueden tener nodos homónimos.
	hostIDByNode := map[string]string{} // "inst|node" → id del device host
	hostIdxByID := map[string]int{}
	nodeByKey := map[string]pveNode{} // "inst|node" → nodo (para renombrar)
	for i := range devices {
		hostIdxByID[devices[i].ID] = i
		if devices[i].IP != "" {
			for key, ip := range inv.nodeIPs {
				if devices[i].IP == ip {
					hostIDByNode[key] = devices[i].ID
					nodeByKey[key] = inv.nodes[key]
				}
			}
		}
	}
	// Pasada 2: por nombre, solo si ese nodo aún no tiene host por IP.
	for i := range devices {
		matched := ""
		for key, n := range inv.nodes {
			if n.Node == devices[i].Name {
				matched = key
				if _, ok := hostIDByNode[key]; !ok {
					break // este nodo en concreto aún no tiene host
				}
			}
		}
		if matched == "" {
			continue
		}
		if _, ok := hostIDByNode[matched]; ok {
			continue // ya tiene host por IP
		}
		hostIDByNode[matched] = devices[i].ID
		nodeByKey[matched] = inv.nodes[matched]
	}
	// Casar cada CT por MAC.
	for mac, vm := range inv.ctByMAC {
		idx, ok := hostIdxByID[macToDeviceID(mac)]
		if !ok {
			continue // el CT no es un device conocido (apagado o sin tráfico)
		}
		hostID := hostIDByNode[nodeKey(vm.Instance, vm.Node)]
		devices[idx].Infra = "ct"
		// El sello PVE es ground truth: si el CT tiene host conocido, cuelga
		// de él (sobreescribe el attachTo inferido por L2, que en puertos
		// mezclados apunta a un nodo "inferred" genérico).
		if hostID != "" {
			devices[idx].AttachTo = hostID
		}
	}
	// Sellar los hosts y renombrarlos con el nombre del nodo cuando el device
	// solo se conoce por MAC (p. ej. "FE:C9:95:97:15:30" → "citadel-01").
	for key, id := range hostIDByNode {
		idx, ok := hostIdxByID[id]
		if !ok {
			continue
		}
		devices[idx].Infra = "hypervisor"
		if n, ok := nodeByKey[key]; ok && looksLikeMACName(devices[idx].Name) {
			devices[idx].Name = n.Node
		}
	}
	// CTs por host (para el macCount informativo del distnode).
	ctCountByHost := map[string]int{}
	for mac := range inv.ctByMAC {
		if idx, ok := hostIdxByID[macToDeviceID(mac)]; ok {
			if h := devices[idx].AttachTo; h != "" {
				ctCountByHost[h]++
			}
		}
	}
	// Distnodes kind=hypervisor por host PVE: activan en el frontend el grid
	// de CTs, el badge +N y los enlaces host→CT. Se saltan los hosts que ya
	// tengan uno inferido por L2 (no duplicar).
	existingHost := map[string]bool{}
	for _, dn := range dists {
		if dn.Kind == "hypervisor" && dn.HostDeviceID != "" {
			existingHost[dn.HostDeviceID] = true
		}
	}
	for key, id := range hostIDByNode {
		if existingHost[id] {
			continue
		}
		idx, ok := hostIdxByID[id]
		if !ok {
			continue
		}
		n := inv.nodes[key]
		dists = append(dists, DistributionNode{
			ID: "dist-pve-" + n.Instance + "-" + n.Node, Kind: "hypervisor",
			RouterID: devices[idx].RouterID, Port: devices[idx].Port, PortLabel: devices[idx].PortLabel,
			HostDeviceID: id, Name: n.Node, MacCount: ctCountByHost[id],
			Source: "proxmox", Instance: n.Instance,
		})
	}
	return dists
}

// looksLikeMACName: true si el nombre del device es una MAC (no tiene nombre
// real asignado por lease/alias). Formato "AA:BB:CC:DD:EE:FF" o con guiones.
func looksLikeMACName(name string) bool {
	hex := 0
	for _, r := range name {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			hex++
		} else if r != ':' && r != '-' {
			return false
		}
	}
	return hex == 12
}

// macToDeviceID: el ID de device es la MAC en minúsculas con guiones.
func macToDeviceID(mac string) string {
	return strings.ToLower(strings.ReplaceAll(mac, ":", "-"))
}
