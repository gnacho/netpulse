package adapters

import (
	"net"
	"strings"
)

// device_seen.go — seguimiento first/last seen de clientes (#954).
//
// noteDevicesSeen se llama una vez por ciclo del poller con TODOS los
// clientes y persiste, en una sola transacción, la primera vez (alta) y la
// última vez (cada ciclo online) que se vio cada MAC. applyDeviceSeen
// rellena FirstSeenMs/LastSeenMs del payload final leyendo la tabla de una
// vez (sin N+1). Con db nil (demo/tests sin BD) ambos son no-op y los campos
// quedan ausentes, como hasta ahora.

func normSeenMAC(mac string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(mac), "-", ":"))
}

// noteDevicesSeen upsert de las MACs online en device_seen (#954): alta con
// first_seen=last_seen=now; las ya conocidas solo actualizan last_seen.
// #1145: guarda también el último nombre conocido (hostname de lease o nombre
// visible) para que un cliente que deje de verse conserve su nombre en la
// lista (el nombre nuevo pisa al viejo solo si no es vacío).
// saneIP: nil si el valor no es una IP (#1278). Algunas fuentes reportan
// el hostname con formato MAC-con-guiones en el campo ip y acaba pintándose
// en la columna IP de la tabla.
func saneIP(ip string) string {
	if ip == "" {
		return ""
	}
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

// filterTombstoned (#1292): quita de la LISTA FINAL las MACs enterradas.
// Una fuente viva (una estación stale del AP) puede seguir emitiéndolas en
// cada ciclo sin que pasen por device_seen (ni first_seen ni last_seen):
// el filtro del registro no basta, la lista misma se filtra.
// #1307: el mismo razonamiento aplica a las MACs propias de flota (base,
// bridge y BSSID de radios): aunque el purge las borre del registro, la
// fuente viva las re-emitiría como cliente nuevo en el mismo ciclo.
func (l *Live) filterTombstoned(devices []Device) []Device {
	if l.db == nil || len(devices) == 0 {
		return devices
	}
	tomb, err := l.db.DeletedClients()
	if err != nil {
		tomb = map[string]bool{}
	}
	fleet := l.fleetSeenMACs()
	if len(tomb) == 0 && len(fleet) == 0 {
		return devices
	}
	out := devices[:0]
	for _, d := range devices {
		m := normSeenMAC(d.MAC)
		if tomb[m] || fleet[m] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// fleetSeenMACs (#1278/#1307): MACs propias de la flota que nunca deben
// tratarse como clientes - MAC base de cada unidad (tabla routers), MAC de
// bridge del último push de cada agente (la "mgmt" que los switches aprenden
// por FDB) y BSSID de cada radio (#1307, p.ej. ap1-phy1-mac-addr: los AP se
// ven a sí mismos como estaciones stale). La usan el purge del registro y
// el filtrado de las listas finales.
func (l *Live) fleetSeenMACs() map[string]bool {
	macs := map[string]bool{}
	// MAC base de cada unidad (tabla routers).
	if l.db != nil {
		if rows, err := l.db.Query("SELECT mac FROM routers WHERE mac IS NOT NULL AND mac != ''"); err == nil {
			for rows.Next() {
				var m string
				if err := rows.Scan(&m); err == nil {
					if n := normSeenMAC(m); n != "" {
						macs[n] = true
					}
				}
			}
			rows.Close()
		}
	}
	l.mu.Lock()
	for _, p := range l.lastPolled {
		if m := normSeenMAC(p.brMac); m != "" {
			macs[m] = true
		}
		for _, r := range p.radios {
			if m := normSeenMAC(r.BSSID); m != "" {
				macs[m] = true
			}
		}
	}
	l.mu.Unlock()
	return macs
}

// purgeFleetSeen (#1278): borra del registro la MAC base de las unidades de
// flota. Las bocas/gestiones de los propios equipos se aprendían como
// "clientes" vía FDB de los switches y quedaban como fantasmas offline para
// siempre. Purgamos todo el conjunto de fleetSeenMACs (base + bridge +
// BSSID de radios, #1307); las lápidas del borrado manual van aparte.
func (l *Live) purgeFleetSeen() {
	macs := l.fleetSeenMACs()
	for m := range macs {
		_, _ = l.db.Exec("DELETE FROM device_seen WHERE mac = ?", m)
	}
}

func (l *Live) noteDevicesSeen(devices []Device, nowMs int64) {
	if l.db == nil {
		return
	}
	l.purgeFleetSeen()
	tomb, err := l.db.DeletedClients()
	if err != nil {
		tomb = map[string]bool{}
	}
	tx, err := l.db.DB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op tras Commit
	stmt, err := tx.Prepare(`INSERT INTO device_seen (mac, first_seen, last_seen, name, ip) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(mac) DO UPDATE SET last_seen = excluded.last_seen,
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE device_seen.name END,
			ip = CASE WHEN excluded.ip != '' THEN excluded.ip ELSE device_seen.ip END`)
	if err != nil {
		return
	}
	defer stmt.Close()
	for _, d := range devices {
		if !d.Online {
			continue
		}
		mac := normSeenMAC(d.MAC)
		if mac == "" {
			continue
		}
		if tomb[mac] {
			continue // #1278: borrado a mano, no re-alta
		}
		name := d.Hostname
		if name == "" && d.Name != "" && !strings.EqualFold(d.Name, d.MAC) {
			name = d.Name
		}
		if _, err := stmt.Exec(mac, nowMs, nowMs, name, saneIP(d.IP)); err != nil {
			return
		}
	}
	_ = tx.Commit()
}

// applyDeviceSeen rellena FirstSeenMs/LastSeenMs de cada dispositivo desde
// device_seen (#954). Los sin fila (nunca vistos online por ESTE server, o
// demo sin BD) quedan a 0 y la app muestra "—".
func (l *Live) applyDeviceSeen(devices []Device) {
	if l.db == nil || len(devices) == 0 {
		return
	}
	byMac, err := l.seenByMac()
	if err != nil {
		return
	}
	for i := range devices {
		if s, ok := byMac[normSeenMAC(devices[i].MAC)]; ok {
			devices[i].FirstSeenMs = s.first
			devices[i].LastSeenMs = s.last
		}
	}
}

type seenRow struct {
	first, last int64
	name, ip    string
}

// seenByMac carga la tabla device_seen completa (una query, sin N+1).
func (l *Live) seenByMac() (map[string]seenRow, error) {
	rows, err := l.db.DB.Query(`SELECT mac, first_seen, last_seen, COALESCE(name, ''), COALESCE(ip, '') FROM device_seen`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byMac := map[string]seenRow{}
	for rows.Next() {
		var mac string
		var s seenRow
		if err := rows.Scan(&mac, &s.first, &s.last, &s.name, &s.ip); err != nil {
			return nil, err
		}
		byMac[mac] = s
	}
	return byMac, nil
}

// ghostDevices: clientes del registro (device_seen) que ya no aparecen en
// ninguna fuente viva (lease caducado, ARP limpiada, FDB envejecido) y por
// tanto buildDevices no emite. Se listan como offline con su último nombre
// y último seen: retención indefinida (#1145). El front los distingue por
// Online=false y su LastSeenMs antiguo.
func (l *Live) ghostDevices(devices []Device) []Device {
	if l.db == nil {
		return nil
	}
	live := make(map[string]bool, len(devices))
	for i := range devices {
		live[normSeenMAC(devices[i].MAC)] = true
	}
	// #1151: una alias enlazada no sale como fantasma propio - su canónico la
	// representa en la lista (si el canónico no está en el registro, la alias
	// se muestra tal cual para no perder al cliente).
	links := l.deviceLinks()
	byMac, err := l.seenByMac()
	if err != nil {
		return nil
	}
	// #1151: aliases registradas de cada canónico (para que la hoja las
	// muestre y permita desenlazarlas).
	revAlias := map[string][]string{}
	for alias, canonical := range links {
		if _, registered := byMac[canonical]; registered {
			revAlias[canonical] = append(revAlias[canonical], alias)
		}
	}
	tomb, err := l.db.DeletedClients()
	if err != nil {
		tomb = map[string]bool{}
	}
	var out []Device
	for rawMac, s := range byMac {
		mac := normSeenMAC(rawMac)
		if live[mac] {
			continue
		}
		if tomb[mac] {
			continue // #1278: borrado a mano, fuera de la lista de fantasmas
		}
		if canonical, linked := links[mac]; linked {
			if _, canonRegistered := byMac[canonical]; canonRegistered {
				continue
			}
		}
		d := Device{
			ID:          strings.ToLower(strings.ReplaceAll(mac, ":", "-")),
			MAC:         mac,
			Type:        "desconocido",
			IP:          saneIP(s.ip),
			Online:      false,
			Band:        "—",
			FirstSeenMs: s.first,
			LastSeenMs:  s.last,
		}
		if s.name != "" {
			d.Hostname = s.name
			d.Name = s.name
		}
		if aliases := revAlias[mac]; len(aliases) > 0 {
			d.AliasMacs = aliases
		}
		out = append(out, d)
	}
	return out
}
