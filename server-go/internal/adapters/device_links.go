package adapters

// device_links.go — cliente con varias MACs (#1151): un mismo dispositivo
// (p. ej. un reloj que usa una MAC por SSID) puede aparecer como varios
// clientes. El usuario enlaza las MACs (device_links: alias → canónico) y
// la lista las fusiona en una sola entrada.

import (
	"strings"
)

// normalizeLinkMAC: formato canónico de identidad (mayúsculas, ':'). Igual
// que normSeenMAC; nombre propio para no acoplar los dos ficheros.
func normalizeLinkMAC(mac string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(mac), "-", ":"))
}

// DeviceLinks carga TODOS los enlaces alias → canónico. Mapa vacío si no hay
// BD o no hay enlaces.
func (l *Live) deviceLinks() map[string]string {
	out := map[string]string{}
	if l.db == nil {
		return out
	}
	rows, err := l.db.DB.Query(`SELECT mac, canonical FROM device_links`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var alias, canonical string
		if err := rows.Scan(&alias, &canonical); err != nil {
			continue
		}
		out[normalizeLinkMAC(alias)] = normalizeLinkMAC(canonical)
	}
	return out
}

// mergeLinkedDevices funde los clientes enlazados en su entrada canónica:
// online si cualquiera lo está, hostname/IP/fabricante combinados (el
// canónico manda, las alias rellenan huecos) y el tráfico se suma (MACs
// alternantes: nunca simultáneas, la suma es la actividad del cliente).
// La entrada resultante lleva SIEMPRE la MAC canónica (identidad estable:
// aunque el watch cambie de SSID y su alias sea la única vista, el cliente
// conserva la MAC por la que el usuario lo enlazó) y AliasMacs con las MACs
// fundidas.
func mergeLinkedDevices(devices []Device, links map[string]string) []Device {
	if len(devices) == 0 || len(links) == 0 {
		return devices
	}
	groups := map[string][]Device{}
	order := []string{}
	loose := []Device{} // sin MAC: sin identidad enlazable, se conservan tal cual
	for _, d := range devices {
		mac := normalizeLinkMAC(d.MAC)
		if mac == "" {
			loose = append(loose, d)
			continue
		}
		canonical, linked := links[mac]
		if !linked {
			canonical = mac
		}
		if _, seen := groups[canonical]; !seen {
			order = append(order, canonical)
		}
		groups[canonical] = append(groups[canonical], d)
	}
	out := make([]Device, 0, len(devices))
	for _, canonical := range order {
		ds := groups[canonical]
		if len(ds) == 1 {
			// #1151: si la única entrada es una alias (el canónico no se ve
			// ahora), expone la identidad canónica con la alias en AliasMacs.
			d := ds[0]
			mac := normalizeLinkMAC(d.MAC)
			if c, linked := links[mac]; linked && c != mac {
				d.MAC = c
				d.ID = strings.ToLower(strings.ReplaceAll(c, ":", "-"))
				d.AliasMacs = []string{mac}
			}
			out = append(out, d)
			continue
		}
		// Primario: el dispositivo cuya MAC ES el canónico; si solo hay
		// alias (el canónico no se ve ahora), el primero online o el primero.
		primary := 0
		for i := range ds {
			if normalizeLinkMAC(ds[i].MAC) == canonical {
				primary = i
				break
			}
		}
		if normalizeLinkMAC(ds[primary].MAC) != canonical {
			for i := range ds {
				if ds[i].Online {
					primary = i
					break
				}
			}
		}
		canon := ds[primary]
		aliases := make([]string, 0, len(ds)-1)
		for i, d := range ds {
			if i == primary {
				continue
			}
			aliases = append(aliases, normalizeLinkMAC(d.MAC))
			if d.Online {
				canon.Online = true
			}
			if canon.IP == "" {
				canon.IP = d.IP
			}
			if canon.Hostname == "" {
				canon.Hostname = d.Hostname
			}
			// #1315: el canónico sin nombre hereda el de la alias solo si
			// eso es un nombre real. Si su nombre actual es una MAC (fallback
			// sin hostname) y la alias solo ofrece otra MAC, se conserva la
			// propia: mejor la MAC del canónico que la de una alias.
			if canon.Name == "" {
				canon.Name = d.Name
			} else if isMACLike(canon.Name) && !isMACLike(d.Name) {
				canon.Name = d.Name
			}
			if canon.Manufacturer == "" {
				canon.Manufacturer = d.Manufacturer
			}
			canon.TrafficMbps += d.TrafficMbps
		}
		canon.MAC = canonical
		canon.ID = strings.ToLower(strings.ReplaceAll(canonical, ":", "-"))
		canon.AliasMacs = aliases
		out = append(out, canon)
	}
	return append(out, loose...)
}

// LinkSuggestions: pares de clientes NO enlazados que comparten hostname no
// vacío e IP y no están los dos online a la vez (#1151): candidatos a ser el
// mismo dispositivo con varias MACs. Solo sugiere; nunca fusiona solo.
func LinkSuggestions(devices []Device, links map[string]string) [][2]string {
	type id struct{ host, ip string }
	byID := map[id][]Device{}
	for _, d := range devices {
		host := strings.ToLower(strings.TrimSpace(d.Hostname))
		if host == "" || d.IP == "" {
			continue
		}
		if _, linked := links[normalizeLinkMAC(d.MAC)]; linked {
			continue // ya enlazado
		}
		k := id{host: host, ip: d.IP}
		byID[k] = append(byID[k], d)
	}
	var out [][2]string
	seenPair := map[string]bool{}
	for _, ds := range byID {
		if len(ds) < 2 {
			continue
		}
		online := 0
		for _, d := range ds {
			if d.Online {
				online++
			}
		}
		if online > 1 {
			continue // dos a la vez: son dispositivos distintos
		}
		for i := 0; i < len(ds); i++ {
			for j := i + 1; j < len(ds); j++ {
				a, b := normalizeLinkMAC(ds[i].MAC), normalizeLinkMAC(ds[j].MAC)
				if a == "" || b == "" || a == b {
					continue
				}
				pair := a + "|" + b
				if a > b {
					pair = b + "|" + a
				}
				if seenPair[pair] {
					continue
				}
				seenPair[pair] = true
				out = append(out, [2]string{a, b})
			}
		}
	}
	return out
}
