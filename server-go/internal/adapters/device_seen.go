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
	return strings.ToUpper(strings.TrimSpace(mac))
}

// noteDevicesSeen upsert de las MACs online en device_seen (#954): alta con
// first_seen=last_seen=now; las ya conocidas solo actualizan last_seen.
func (l *Live) noteDevicesSeen(devices []Device, nowMs int64) {
	if l.db == nil {
		return
	}
	tx, err := l.db.DB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op tras Commit
	stmt, err := tx.Prepare(`INSERT INTO device_seen (mac, first_seen, last_seen) VALUES (?, ?, ?)
		ON CONFLICT(mac) DO UPDATE SET last_seen = excluded.last_seen`)
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
		if _, err := stmt.Exec(mac, nowMs, nowMs); err != nil {
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
	rows, err := l.db.DB.Query(`SELECT mac, first_seen, last_seen FROM device_seen`)
	if err != nil {
		return
	}
	defer rows.Close()
	type seen struct{ first, last int64 }
	byMac := map[string]seen{}
	for rows.Next() {
		var mac string
		var s seen
		if err := rows.Scan(&mac, &s.first, &s.last); err != nil {
			return
		}
		byMac[mac] = s
	}
	for i := range devices {
		if s, ok := byMac[normSeenMAC(devices[i].MAC)]; ok {
			devices[i].FirstSeenMs = s.first
			devices[i].LastSeenMs = s.last
		}
	}
}
