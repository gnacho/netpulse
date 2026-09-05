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
	// nodeNames: nodos del cluster (p. ej. "citadel-01").
	nodeNames map[string]bool
	// nodeIPs: nodo → IP del bridge vmbr0 (p. ej. citadel-02 → 192.168.1.101).
	// Permite casar el device HOST por IP cuando NetPulse no conoce su nombre
	// (el host aparece solo por MAC/IP).
	nodeIPs map[string]string
}

type pveVM struct {
	Name string // hostname del CT/VM (webs, pbs…)
	Node string // nodo que lo ejecuta (citadel-01)
	Type string // "lxc" | "qemu"
}

// pveConfigFromKV: lee la config de la integración desde el kv.
func (l *Live) pveConfigFromKV() pve.Config {
	var cfg pve.Config
	if l.db == nil {
		return cfg
	}
	_ = l.db.QueryRow("SELECT value FROM kv WHERE key='proxmox_url'").Scan(&cfg.URL)
	_ = l.db.QueryRow("SELECT value FROM kv WHERE key='proxmox_token_id'").Scan(&cfg.TokenID)
	_ = l.db.QueryRow("SELECT value FROM kv WHERE key='proxmox_token_secret'").Scan(&cfg.Secret)
	return cfg
}

// pveClientCached: devuelve el cliente PVE para la config actual, recreándolo
// si cambió (patrón AdGuard). Sin config → nil.
func (l *Live) pveClientCached() *pve.Client {
	cfg := l.pveConfigFromKV()
	if !cfg.Enabled() {
		l.pveClient = nil
		l.pveKey = ""
		return nil
	}
	key := cfg.URL + "|" + cfg.TokenID + "|" + cfg.Secret
	if l.pveClient == nil || l.pveKey != key {
		l.pveClient = pve.NewClient(cfg)
		l.pveKey = key
	}
	return l.pveClient
}

// pveInventoryCached: inventario PVE (VM→MAC→node) con TTL. Devuelve nil si
// no configurado o si la consulta falla (no-op, no rompe el overview).
func (l *Live) pveInventoryCached() *pveInventory {
	client := l.pveClientCached()
	if client == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pveInv != nil && time.Since(l.pveInvAt) < pveInventoryTTL {
		return l.pveInv
	}
	inv := l.fetchPveInventory(client)
	if inv == nil {
		return l.pveInv // conserva el último bueno si la consulta falla
	}
	l.pveInv = inv
	l.pveInvAt = time.Now()
	return inv
}

// fetchPveInventory: consulta cluster/resources + config de cada VM/CT
// running para construir mac→VM y la lista de nodos. Best-effort: una VM que
// no se pueda leer no invalida el resto.
func (l *Live) fetchPveInventory(client *pve.Client) *pveInventory {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resources, err := client.ClusterResources(ctx)
	if err != nil {
		log.Printf("[netpulse:pve] cluster/resources: %v", err)
		return nil
	}
	inv := &pveInventory{
		ctByMAC:   map[string]pveVM{},
		nodeNames: map[string]bool{},
		nodeIPs:   map[string]string{},
	}
	for _, r := range resources {
		if r.Type == "node" {
			// El nombre del nodo viene en `node`, no en `name` (que los
			// nodos dejan vacío en cluster/resources).
			if r.Node != "" {
				inv.nodeNames[r.Node] = true
				// IP del host (vmbr0) para casar el device HOST por IP.
				if ip, err := client.NodeIP(ctx, r.Node); err == nil && ip != "" {
					inv.nodeIPs[r.Node] = ip
				} else if err != nil {
					log.Printf("[netpulse:pve] nodeip %s: %v", r.Node, err)
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
		cfg, err := client.VMConfig(ctx, r.Node, r.Type, r.VMID)
		if err != nil {
			log.Printf("[netpulse:pve] config %s/%d: %v", r.Type, r.VMID, err)
			continue
		}
		for _, mac := range pve.MACsOfConfig(cfg) {
			inv.ctByMAC[mac] = pveVM{Name: r.Name, Node: r.Node, Type: r.Type}
		}
	}
	if len(inv.ctByMAC) == 0 && len(inv.nodeNames) == 0 {
		return nil // cluster sin VMs corriendo ni nodos legibles
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
func (l *Live) sealProxmoxInfra(devices []Device) {
	inv := l.pveInventoryCached()
	if inv == nil {
		return
	}
	applyPVEInfra(devices, inv)
}

// applyPVEInfra sella devices con un inventario dado (función pura, testeable
// sin red ni kv). Ver sealProxmoxInfra para el diseño.
func applyPVEInfra(devices []Device, inv *pveInventory) {
	if inv == nil || len(inv.ctByMAC) == 0 {
		return
	}
	// Índices. El host de un nodo se casa por IP del nodo (vmbr0, la que da
	// el cluster) con prioridad, y por nombre del device como fallback. Un
	// host físico con dos NICs aparece como dos devices (p. ej. citadel-01
	// con .100 vmbr0 y .243 de gestión): el device con la IP del nodo es el
	// host correcto (online); el otro (nombre coincidente pero IP distinta)
	// NO debe ganar.
	hostIDByNode := map[string]string{} // node → id del device host
	hostIdxByID := map[string]int{}
	nodeNameByID := map[string]string{} // id del device host → nombre del nodo
	for i := range devices {
		hostIdxByID[devices[i].ID] = i
		if devices[i].IP != "" {
			for node, ip := range inv.nodeIPs {
				if devices[i].IP == ip {
					hostIDByNode[node] = devices[i].ID
					nodeNameByID[devices[i].ID] = node
				}
			}
		}
	}
	// Pasada 2: por nombre, solo si el nodo aún no tiene host por IP.
	for i := range devices {
		if _, ok := hostIDByNode[devices[i].Name]; ok {
			continue // el nodo ya tiene host (por IP)
		}
		if inv.nodeNames[devices[i].Name] {
			hostIDByNode[devices[i].Name] = devices[i].ID
			nodeNameByID[devices[i].ID] = devices[i].Name
		}
	}
	// Casar cada CT por MAC.
	for mac, vm := range inv.ctByMAC {
		idx, ok := hostIdxByID[macToDeviceID(mac)]
		if !ok {
			continue // el CT no es un device conocido (apagado o sin tráfico)
		}
		hostID := hostIDByNode[vm.Node]
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
	for node, id := range hostIDByNode {
		if idx, ok := hostIdxByID[id]; ok {
			devices[idx].Infra = "hypervisor"
			if name, ok := nodeNameByID[id]; ok && looksLikeMACName(devices[idx].Name) {
				devices[idx].Name = name
				_ = node
			}
		}
	}
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
