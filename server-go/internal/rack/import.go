// import.go - auto-cableado del rack desde la topología detectada (fase 2).
//
// La fuente es el descubrimiento (FDB/LLDP), no el dibujo: un CableHint
// propone un cable entre dos dispositivos montados. ImportCables es
// IDEMPOTENTE POR CONSTRUCCIÓN: un par de montajes ya cableado se salta (en
// cualquier puerto); no hay flag "ya importado" que viva solo en memoria y
// duplique cables tras un reload. Re-ejecutar tras montar más equipos hace
// la mitad útil del trabajo.
package rack

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CableHint candidato de cable. From/To son MACs normalizadas; ambos
// extremos deben estar montados para que el hint produzca cable.
type CableHint struct {
	FromDeviceID string
	ToDeviceID   string
	Medium       string // "ethernet" | "fiber" (vacío = se deriva del puerto)
	FromPortHint string // ifName/ifDescr opcional (LLDP/SNMP)
	ToPortHint   string
	Source       string // "lldp" | "fdb" | "manual"
}

// ImportResult resume una pasada de importación.
type ImportResult struct {
	Created    []Cable
	Skipped    int // par ya cableado o algún extremo no montado
	NoFreePort []CableHint
	Removed    int // #1186: cables detected retirados por el sync
}

// pairKey dedup por par NO ordenado: sort(a,b).join("|").
func pairKey(a, b string) string {
	p := []string{a, b}
	sort.Strings(p)
	return p[0] + "|" + p[1]
}

func normMac(m string) string { return strings.ToLower(strings.TrimSpace(m)) }

// ImportCables crea cables para los hints cuyos extremos están montados.
// Elección de puerto en dos niveles: primero uno libre cuyo tipo case con el
// medio (fiber → sfp/sfp+, cobre → rj45); si no, cualquier puerto libre.
// Sin puerto libre en algún extremo se reporta en NoFreePort (nunca se cae
// en silencio). Todo en una transacción: fallo medio → ningún cable.
func ImportCables(db *sql.DB, hints []CableHint) (ImportResult, error) {
	mounts, err := ListMountRows(db)
	if err != nil {
		return ImportResult{}, err
	}
	mountByMac := map[string]MountRow{}
	for _, m := range mounts {
		if m.DeviceMAC != "" {
			mountByMac[normMac(m.DeviceMAC)] = m
		}
	}
	profiles := map[string]DeviceProfile{}
	if profs, err := ListProfiles(db); err == nil {
		for _, p := range profs {
			profiles[normMac(p.MAC)] = p
		}
	}
	existing, err := ListCables(db)
	if err != nil {
		return ImportResult{}, err
	}

	// Estado vivo: pares cableados y uso por puerto.
	pairs := map[string]bool{}
	usage := map[string]map[string]int{} // mountID -> portID -> cables
	addUsage := func(mountID, portID string) {
		if usage[mountID] == nil {
			usage[mountID] = map[string]int{}
		}
		usage[mountID][portID]++
	}
	for _, c := range existing {
		pairs[pairKey(c.FromMount, c.ToMount)] = true
		addUsage(c.FromMount, c.FromPort)
		addUsage(c.ToMount, c.ToPort)
	}

	capacity := func(m MountRow) int {
		if m.DeviceMAC == "" && IsPassThrough(m.FaceplateID) {
			return 2
		}
		return 1
	}
	free := func(m MountRow, portID string) bool {
		return usage[m.ID][portID] < capacity(m)
	}
	mediumWantsFiber := func(medium string) bool { return medium == CableFiber }

	// pickPort: 0) hint exacto libre (evidencia directa: LLDP/SNMP lo vio
	// en ese puerto), 1) libre que case con el medio, 2) cualquier libre.
	// hinted=true cuando ganó el hint exacto: el tipo de cable se deriva
	// ENTONCES del puerto (la evidencia directa manda sobre el medio
	// inferido).
	pickPort := func(m MountRow, prof DeviceProfile, medium, hint string) (*Port, bool) {
		ports := prof.Ports
		if len(ports) == 0 {
			return nil, false
		}
		if hint != "" {
			for i := range ports {
				if ports[i].ID == hint && free(m, ports[i].ID) {
					return &ports[i], true
				}
			}
		}
		wantFiber := mediumWantsFiber(medium)
		for i := range ports {
			isFiber := ports[i].Kind != PortRJ45
			if free(m, ports[i].ID) && isFiber == wantFiber {
				return &ports[i], false
			}
		}
		for i := range ports {
			if free(m, ports[i].ID) {
				return &ports[i], false
			}
		}
		return nil, false
	}

	res := ImportResult{}
	tx, err := db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	for _, h := range hints {
		ma, oka := mountByMac[normMac(h.FromDeviceID)]
		mb, okb := mountByMac[normMac(h.ToDeviceID)]
		if !oka || !okb || ma.ID == mb.ID {
			res.Skipped++
			continue
		}
		if pairs[pairKey(ma.ID, mb.ID)] {
			res.Skipped++
			continue
		}
		pa, hintedA := pickPort(ma, profiles[normMac(ma.DeviceMAC)], h.Medium, h.FromPortHint)
		pb, _ := pickPort(mb, profiles[normMac(mb.DeviceMAC)], h.Medium, h.ToPortHint)
		if pa == nil || pb == nil {
			res.NoFreePort = append(res.NoFreePort, h)
			continue
		}
		// Cobre vs fibra: si el puerto de origen salió de evidencia directa
		// (hint), manda su tipo; si no, el medio declarado y por último el
		// tipo del puerto de origen del patch.
		medium := h.Medium
		if hintedA || medium == "" {
			medium = CableEthernet
			if pa.Kind != PortRJ45 {
				medium = CableFiber
			}
		}
		origin := OriginDetected
		if h.Source == OriginManual {
			origin = OriginImported
		}
		c := Cable{
			ID:        newID(),
			FromMount: ma.ID,
			FromPort:  pa.ID,
			ToMount:   mb.ID,
			ToPort:    pb.ID,
			Type:      medium,
			Origin:    origin,
			CreatedAt: time.Now().UnixMilli(),
		}
		if _, err := tx.Exec(`INSERT INTO rack_cables (id, from_mount, from_port, to_mount, to_port, type, label, properties_json, origin, created_at)
			VALUES (?, ?, ?, ?, ?, ?, NULL, '{}', ?, ?)`,
			c.ID, c.FromMount, c.FromPort, c.ToMount, c.ToPort, c.Type, c.Origin, c.CreatedAt); err != nil {
			return res, err
		}
		pairs[pairKey(ma.ID, mb.ID)] = true
		addUsage(ma.ID, pa.ID)
		addUsage(mb.ID, pb.ID)
		res.Created = append(res.Created, c)
	}
	return res, tx.Commit()
}

// AuditStatus de un cable contra la detección actual.
const (
	AuditConfirmed  = "confirmed"   // detección actual confirma el par
	AuditManualOnly = "manual-only" // dibujado a mano, sin evidencia
	AuditDetected   = "detected"    // origin detectado/importado
)

// AuditCables marca cada cable: los origin!=manual son "detected"; los
// "manual" cuyo par aparece en los hints actuales pasan a "confirmed"; el
// resto queda "manual-only".
func AuditCables(cables []Cable, hints []CableHint, mounts []MountRow) map[string]string {
	mountByMac := map[string]MountRow{}
	for _, m := range mounts {
		if m.DeviceMAC != "" {
			mountByMac[normMac(m.DeviceMAC)] = m
		}
	}
	hintPairs := map[string]bool{}
	for _, h := range hints {
		ma, oka := mountByMac[normMac(h.FromDeviceID)]
		mb, okb := mountByMac[normMac(h.ToDeviceID)]
		if oka && okb {
			hintPairs[pairKey(ma.ID, mb.ID)] = true
		}
	}
	out := map[string]string{}
	for _, c := range cables {
		switch {
		case c.Origin != OriginManual:
			out[c.ID] = AuditDetected
		case hintPairs[pairKey(c.FromMount, c.ToMount)]:
			out[c.ID] = AuditConfirmed
		default:
			out[c.ID] = AuditManualOnly
		}
	}
	return out
}

var _ = fmt.Sprintf // (placeholder si se añaden errores formateados)
