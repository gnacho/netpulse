package adapters

import (
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
func (l *Live) noteDevicesSeen(devices []Device, nowMs int64) {
	if l.db == nil {
		return
	}
	tx, err := l.db.DB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op tras Commit
	stmt, err := tx.Prepare(`INSERT INTO device_seen (mac, first_seen, last_seen, name) VALUES (?, ?, ?, ?)
		ON CONFLICT(mac) DO UPDATE SET last_seen = excluded.last_seen,
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE device_seen.name END`)
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
		name := d.Hostname
		if name == "" && d.Name != "" && !strings.EqualFold(d.Name, d.MAC) {
			name = d.Name
		}
		if _, err := stmt.Exec(mac, nowMs, nowMs, name); err != nil {
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
	name        string
}

// seenByMac carga la tabla device_seen completa (una query, sin N+1).
func (l *Live) seenByMac() (map[string]seenRow, error) {
	rows, err := l.db.DB.Query(`SELECT mac, first_seen, last_seen, COALESCE(name, '') FROM device_seen`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byMac := map[string]seenRow{}
	for rows.Next() {
		var mac string
		var s seenRow
		if err := rows.Scan(&mac, &s.first, &s.last, &s.name); err != nil {
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
	byMac, err := l.seenByMac()
	if err != nil {
		return nil
	}
	var out []Device
	for rawMac, s := range byMac {
		mac := normSeenMAC(rawMac)
		if live[mac] {
			continue
		}
		d := Device{
			ID:          strings.ToLower(strings.ReplaceAll(mac, ":", "-")),
			MAC:         mac,
			Type:        "desconocido",
			Online:      false,
			Band:        "—",
			FirstSeenMs: s.first,
			LastSeenMs:  s.last,
		}
		if s.name != "" {
			d.Hostname = s.name
			d.Name = s.name
		}
		out = append(out, d)
	}
	return out
}
