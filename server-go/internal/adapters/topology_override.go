// topology_override.go — Capa 2 manual de topología (issue #142, Fase A):
// overrides persistidos que se aplican DESPUÉS del autodiscover
// (inferTopology) y ANTES del modelo semántico (BuildTopoSemantics).
//
// Reglas:
//   - "hypervisor" (MAC): el host es un hipervisor aunque su puerto tenga
//     varios hosts físicos (caso citadel-01: CTs BC:24:11 mezclados con
//     otros equipos en el mismo puerto). Las MACs con OUI de hipervisor del
//     MISMO puerto+router se re-anidan bajo el host. No se crea distnode:
//     el host actúa de device-hub y BuildTopoSemantics dibuja los CTs bajo
//     él igual que el switch gestionado (topo_semantics.go:164-174).
//   - "switch" (MAC): el equipo es un switch gestionado aunque no anuncie
//     LLDP. Su puerto se convierte en distnode managed y el resto del puerto
//     se anida bajo él (el Device del propio switch queda como managed-switch,
//     excluido del mapa por D1).
//   - "attach" (MAC + Parent): el target cuelga de `parent` (útil para VMs
//     con MAC random, p.ej. Home Assistant dentro de citadel-01).
//
// Función pura (sin BD) para ser testeable.
package adapters

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/gnacho/netpulse/agent/macaddr"
)

// queryer: subset de *sql.DB usado para cargar overrides (evita acoplar a db.DB).
type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// loadTopologyOverrides lee los overrides enabled de la tabla. Sigue el
// patrón de lectura directa SQL del Live (device_attrib en live.go:1044).
func loadTopologyOverrides(q queryer) []TopologyOverride {
	if q == nil {
		return nil
	}
	rows, err := q.Query(
		"SELECT id, mac, kind, name, parent, enabled, created_at, updated_at FROM topology_overrides WHERE enabled = 1 ORDER BY created_at ASC")
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []TopologyOverride{}
	for rows.Next() {
		var o TopologyOverride
		var name, parent sql.NullString
		var enabled int
		if err := rows.Scan(&o.ID, &o.MAC, &o.Kind, &name, &parent, &enabled, &o.CreatedAt, &o.UpdatedAt); err != nil {
			continue
		}
		o.Name = name.String
		o.Parent = parent.String
		o.Enabled = enabled == 1
		out = append(out, o)
	}
	return out
}

// colonMAC devuelve la forma canónica (AA:BB:CC:DD:EE:FF) si s es una MAC
// escrita con ':', o "". Los campos de override admiten también ids de router,
// que son slugs (minúsculas, '-', sin ':'): uno como "aa-bb-cc-dd-ee-ff" o
// "cafebabe1234" no debe leerse como MAC, así que solo cuenta la forma con ':'.
func colonMAC(s string) string {
	s = strings.TrimSpace(s)
	if strings.Count(s, ":") != 5 {
		return ""
	}
	return macaddr.Normalize(s)
}

// NormalizeMAC normaliza el target de un override: una MAC con ':' en forma
// canónica; cualquier otra cosa es un id de router (#1485: unidad de flota sin
// MAC de bridge) y se devuelve en minúsculas sin espacios, como antes.
func NormalizeMAC(mac string) string {
	if m := colonMAC(mac); m != "" {
		return m
	}
	return strings.ToLower(strings.TrimSpace(mac))
}

// NormalizeParentRef normaliza el parent de un override antes de guardarlo:
// una MAC con ':' en forma canónica; una referencia de router ("routerId" o
// "routerId:puerto") solo sin espacios alrededor.
func NormalizeParentRef(raw string) string {
	if m := colonMAC(raw); m != "" {
		return m
	}
	return strings.TrimSpace(raw)
}

// deviceIndexByMAC indexa devices por MAC normalizada.
func deviceIndexByMAC(devices []Device) map[string]int {
	m := make(map[string]int, len(devices))
	for i, d := range devices {
		if d.MAC != "" {
			m[NormalizeMAC(d.MAC)] = i
		}
	}
	return m
}

// parentRefKind clasifica el `parent` de un override kind=attach (issue #690).
type parentRefKind int

const (
	parentRefInvalid parentRefKind = iota // no resuelve a nada (override ignorado)
	parentRefMAC                          // MAC de un device (backwards-compatible)
	parentRefRouter                       // id de router (cuelga del router)
	parentRefPort                         // id de router + puerto concreto
)

// routerPort: clave compuesta para localizar un distnode por (router, puerto).
type routerPort struct{ router, port string }

// parseParentRef clasifica el `parent` de un override attach. Formato:
//   - MAC ("AA:BB:CC:DD:EE:FF"): cuelga del device con esa MAC (formato
//     original, se mantiene).
//   - "<routerId>": cuelga directamente del router.
//   - "<routerId>:<puerto>": cuelga del puerto concreto del router.
//
// Una MAC solo cuenta en la forma con ':' (ver colonMAC). La referencia de
// router se compara en minúsculas, id y puerto, como siempre.
func parseParentRef(parent string) (kind parentRefKind, routerID, port, mac string) {
	p := strings.TrimSpace(parent)
	if p == "" {
		return parentRefInvalid, "", "", ""
	}
	if m := colonMAC(p); m != "" {
		return parentRefMAC, "", "", m
	}
	p = strings.ToLower(p)
	if i := strings.IndexByte(p, ':'); i >= 0 {
		routerID, port = strings.TrimSpace(p[:i]), strings.TrimSpace(p[i+1:])
		if routerID == "" || port == "" {
			return parentRefInvalid, "", "", ""
		}
		return parentRefPort, routerID, port, ""
	}
	return parentRefRouter, p, "", ""
}

// applyTopologyOverrides aplica la capa 2 manual sobre el resultado del
// autodiscover. Orden determinista por kind (hypervisor → switch → attach)
// para que los attaches resuelvan sobre los hosts ya sellados.
func applyTopologyOverrides(routers []Router, devices []Device, dists []DistributionNode, overrides []TopologyOverride) ([]Device, []DistributionNode) {
	if len(overrides) == 0 {
		return devices, dists
	}
	byMAC := deviceIndexByMAC(devices)

	// routerByID: routers conocidos (para validar el parent router/port).
	// distByRouterPort: distnodes inferred|managed por (router, puerto) — el
	// anclaje natural de "colgar de un puerto concreto" (issue #690).
	routerByID := make(map[string]bool, len(routers))
	for _, r := range routers {
		routerByID[r.ID] = true
	}
	distByRouterPort := make(map[routerPort]string)
	for _, dn := range dists {
		if dn.Kind != "inferred" && dn.Kind != "managed" {
			continue
		}
		distByRouterPort[routerPort{dn.RouterID, dn.Port}] = dn.ID
	}

	// 1) hypervisor: sellar host + re-anidar CTs OUI del mismo puerto.
	for _, ov := range overrides {
		if !ov.Enabled || ov.Kind != "hypervisor" {
			continue
		}
		idx, ok := byMAC[NormalizeMAC(ov.MAC)]
		if !ok {
			continue
		}
		host := &devices[idx]
		host.Infra = "hypervisor"
		if host.Port == "" {
			continue
		}
		for i := range devices {
			d := &devices[i]
			if d.RouterID != host.RouterID || d.Port != host.Port {
				continue
			}
			if d.ID == host.ID || !isHypervisorMAC(NormalizeMAC(d.MAC)) {
				continue
			}
			d.AttachTo = host.ID
			d.Infra = "ct"
		}
	}

	// 2) switch: el puerto del target → distnode managed; el resto del puerto
	//    se anida bajo él.
	for _, ov := range overrides {
		if !ov.Enabled || ov.Kind != "switch" {
			continue
		}
		idx, ok := byMAC[NormalizeMAC(ov.MAC)]
		if !ok {
			continue
		}
		sw := &devices[idx]
		sw.Infra = "managed-switch"
		if sw.Port == "" {
			continue
		}
		id := fmt.Sprintf("dist-%s-%s", sw.RouterID, sw.Port)
		name := ov.Name
		if name == "" {
			name = sw.Name
		}
		var node *DistributionNode
		for i := range dists {
			if dists[i].ID == id {
				node = &dists[i]
				break
			}
		}
		if node == nil {
			dists = append(dists, DistributionNode{
				ID: id, Kind: "managed", RouterID: sw.RouterID, Port: sw.Port,
				Mac: sw.MAC, Name: name,
			})
			node = &dists[len(dists)-1]
		} else {
			node.Kind = "managed"
			node.Mac = sw.MAC
			if name != "" {
				node.Name = name
			}
		}
		for i := range devices {
			d := &devices[i]
			if d.RouterID != sw.RouterID || d.Port != sw.Port || d.ID == sw.ID {
				continue
			}
			d.AttachTo = id
		}
	}

	// 3) attach: el target cuelga de `parent` (device por MAC, router o
	//    router:puerto — issue #690).
	for _, ov := range overrides {
		if !ov.Enabled || ov.Kind != "attach" || ov.Parent == "" {
			continue
		}
		tIdx, ok := byMAC[NormalizeMAC(ov.MAC)]
		if !ok {
			continue
		}
		target := &devices[tIdx]
		kind, routerID, port, mac := parseParentRef(ov.Parent)
		switch kind {
		case parentRefMAC:
			pIdx, ok := byMAC[mac]
			if !ok {
				continue
			}
			parent := &devices[pIdx]
			target.AttachTo = parent.ID
			if parent.Infra == "hypervisor" {
				target.Infra = "ct"
			}
		case parentRefRouter:
			if !routerByID[routerID] {
				continue
			}
			target.AttachTo = routerID
		case parentRefPort:
			if !routerByID[routerID] {
				continue
			}
			if distID, ok := distByRouterPort[routerPort{routerID, port}]; ok {
				target.AttachTo = distID
			} else {
				target.AttachTo = routerID
				target.Port = port
			}
		default:
			// parent inválido: no-op.
			continue
		}
	}
	return devices, dists
}

// fleetOverrideEvidence (#1321): uplinks de unidades de flota fijados a mano
// por la capa manual (#142). Un override kind=attach cuya MAC es la MAC
// bridge de OTRO router de la flota fija el padre de esa unidad por encima
// de toda inferencia (LLDP > FDB > device > gateway quedan por debajo).
// El target se resuelve en los tres formatos del parent (#690): MAC de
// router, id de router, o "routerId:puerto" (este también fija el puerto
// del enlace). Los overrides de clientes (MAC que no es bridge de flota)
// los sigue aplicando la capa de devices, no esta. Con varios overrides
// sobre la misma unidad gana el ÚLTIMO (la carga viene created_at ASC, así
// que es el más reciente). Pure function, sin BD.
//
// El parseo del parent es local y NO pasa NormalizeMAC al conjunto: el
// formato "routerId:puerto" llevaba el puerto a minúsculas y dejaba de
// casar con el nombre real de la boca ("two-gigabitEthernet 1/0/21").
func fleetOverrideEvidence(overrides []TopologyOverride, routers []Router) map[string]topoParent {
	if len(overrides) == 0 {
		return nil
	}
	routerByMac := map[string]string{}
	routerByID := map[string]bool{}
	routerIDByLower := map[string]string{}
	for _, r := range routers {
		if r.MAC != "" {
			routerByMac[NormalizeMAC(r.MAC)] = r.ID
		}
		routerByID[r.ID] = true
		routerIDByLower[strings.ToLower(r.ID)] = r.ID
	}
	out := map[string]topoParent{}
	for _, o := range overrides {
		if !o.Enabled || o.Kind != "attach" {
			continue
		}
		rid, ok := routerByMac[NormalizeMAC(o.MAC)]
		if !ok {
			// #1485: una unidad de flota sin MAC de bridge (ONT/módem dado de
			// alta como external sin MAC) no se puede fijar por MAC. El target
			// puede ser su id de router: así la intención del admin siempre se
			// puede expresar, sea cual sea la forma en que se registró.
			rid, ok = routerIDByLower[strings.ToLower(strings.TrimSpace(o.MAC))]
			if !ok {
				continue
			}
		}
		p := strings.TrimSpace(o.Parent)
		if p == "" {
			continue
		}
		var parent, port string
		if m := colonMAC(p); m != "" {
			parent = routerByMac[m]
			if parent == "" {
				continue // el target no es un router de flota
			}
		} else {
			routerID := ""
			if i := strings.IndexByte(p, ':'); i >= 0 {
				routerID, port = strings.ToLower(strings.TrimSpace(p[:i])), strings.TrimSpace(p[i+1:])
			} else {
				routerID = strings.ToLower(p)
			}
			if !routerByID[routerID] {
				continue
			}
			parent = routerID
		}
		if parent == "" || parent == rid {
			continue
		}
		out[rid] = topoParent{parent: parent, port: port}
	}
	return out
}
