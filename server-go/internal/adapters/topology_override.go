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

// NormalizeMAC normaliza una MAC a minúsculas sin espacios (formato de
// comparación de la capa de overrides y del resto del pipeline).
func NormalizeMAC(mac string) string {
	return strings.ToLower(strings.TrimSpace(mac))
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

// applyTopologyOverrides aplica la capa 2 manual sobre el resultado del
// autodiscover. Orden determinista por kind (hypervisor → switch → attach)
// para que los attaches resuelvan sobre los hosts ya sellados.
func applyTopologyOverrides(devices []Device, dists []DistributionNode, overrides []TopologyOverride) ([]Device, []DistributionNode) {
	if len(overrides) == 0 {
		return devices, dists
	}
	byMAC := deviceIndexByMAC(devices)

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

	// 3) attach: el target cuelga de `parent`.
	for _, ov := range overrides {
		if !ov.Enabled || ov.Kind != "attach" || ov.Parent == "" {
			continue
		}
		tIdx, ok := byMAC[NormalizeMAC(ov.MAC)]
		if !ok {
			continue
		}
		pIdx, ok := byMAC[NormalizeMAC(ov.Parent)]
		if !ok {
			continue
		}
		target := &devices[tIdx]
		parent := &devices[pIdx]
		target.AttachTo = parent.ID
		if parent.Infra == "hypervisor" {
			target.Infra = "ct"
		}
	}
	return devices, dists
}
