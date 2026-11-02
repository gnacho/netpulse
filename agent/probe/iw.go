// iw.go — fallback nl80211 portable (#1481): `iw dev` + `iw station dump`
// por interfaz para targets sin ubus hostapd (Asuswrt con driver compatible
// nl80211, Linux genérico). Mismo shape que el camino hostapd/iwinfo:
// clientes keyed por MAC mayúscula y radios agrupadas por phy.
package probe

import (
	"strconv"
	"strings"
)

// CmdIwStationDump: un SOLO spawn: el bucle de shell itera las interfaces de
// `iw dev` y vuelca sus estaciones. La sección ==DEV== lleva la topología
// (phy → interfaces, banda, BSSID) y cada ==IF== su station dump.
const CmdIwStationDump = `echo "==DEV=="; iw dev 2>/dev/null; for i in $(iw dev 2>/dev/null | awk '/^[ \t]*Interface /{print $2}'); do echo "==IF==$i"; iw dev "$i" station dump 2>/dev/null; done`

// bandFromMHz convierte una frecuencia MHz al nombre de banda del payload
// ("2.4 GHz"|"5 GHz"|"6 GHz"); "" si no cae en ninguna conocida.
func bandFromMHz(mhz int) string {
	switch {
	case mhz >= 2300 && mhz <= 2500:
		return "2.4 GHz"
	case mhz >= 4900 && mhz < 5950:
		return "5 GHz"
	case mhz >= 5950 && mhz <= 7125:
		return "6 GHz"
	}
	return ""
}

// iwIface: una interfaz de `iw dev` con lo que hace falta para bandas y
// radios (solo interesan las tipo AP para las radios del equipo).
type iwIface struct {
	phy     string
	name    string
	addr    string
	kind    string
	band    string
	channel int
	width   int
	// ssid: `iw dev` la imprime en las interfaces AP/STA activas. Vacia en
	// interfaces down o sin ESSID, y en kernels que no la publican.
	ssid string
}

// ParseIwWireless parsea la salida combinada de CmdIwStationDump: clientes
// asociados (signal, contadores, banda de su interfaz) y radios por phy
// (una por phy con interfaces AP: banda/canal/ancho/BSSID y conteo de
// estaciones). Defensivo: líneas desconocidas se ignoran; sin ==DEV== los
// clientes salen sin banda y sin radios.
func ParseIwWireless(out string) (map[string]WirelessClient, []Radio) {
	clients := map[string]WirelessClient{}
	stationIface := map[string]string{}
	ifaces := map[string]iwIface{}
	phyOrder := []string{}
	phyIfaces := map[string][]iwIface{}

	section := "dev" // nombre de interfaz del ==IF== actual
	curPhy := ""
	var cur *iwIface
	flush := func() {
		if cur != nil && cur.name != "" {
			ifaces[cur.name] = *cur
			phyIfaces[cur.phy] = append(phyIfaces[cur.phy], *cur)
		}
		cur = nil
	}
	station := ""
	for _, line := range strings.Split(out, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case trim == "==DEV==":
			flush()
			section = "dev"
			station = ""
			continue
		case strings.HasPrefix(trim, "==IF=="):
			flush()
			section = strings.TrimPrefix(trim, "==IF==")
			station = ""
			continue
		}
		if section == "dev" {
			switch {
			case strings.HasPrefix(trim, "phy#"):
				curPhy = trim
				phyOrder = append(phyOrder, curPhy)
			case strings.HasPrefix(trim, "Interface "):
				flush()
				cur = &iwIface{phy: curPhy, name: strings.TrimPrefix(trim, "Interface ")}
			case cur != nil && strings.HasPrefix(trim, "addr "):
				cur.addr = strings.ToUpper(strings.TrimPrefix(trim, "addr "))
			case cur != nil && strings.HasPrefix(trim, "type "):
				cur.kind = strings.TrimPrefix(trim, "type ")
			case cur != nil && strings.HasPrefix(trim, "ssid "):
				cur.ssid = strings.TrimSpace(strings.TrimPrefix(trim, "ssid "))
			case cur != nil && strings.HasPrefix(trim, "channel "):
				f := strings.Fields(trim)
				if len(f) >= 2 {
					cur.channel, _ = strconv.Atoi(f[1])
				}
				if open := strings.Index(trim, "("); open >= 0 {
					rest := trim[open+1:]
					if close := strings.Index(rest, ")"); close > 0 {
						if mhz, err := strconv.Atoi(strings.Fields(rest[:close])[0]); err == nil {
							cur.band = bandFromMHz(mhz)
						}
					}
				}
				if w := strings.Index(trim, "width:"); w >= 0 {
					if wf := strings.Fields(trim[w:]); len(wf) >= 2 {
						cur.width, _ = strconv.Atoi(wf[1])
					}
				}
			}
			continue
		}
		// Sección de station dump.
		if strings.HasPrefix(trim, "Station ") {
			// Station 24:9e:7d:0a:c3:1a (on wl3.1)
			f := strings.Fields(trim)
			if len(f) >= 4 {
				station = strings.ToUpper(f[1])
				iface := strings.TrimSuffix(strings.TrimPrefix(f[3], "("), ")")
				stationIface[station] = iface
				if _, ok := clients[station]; !ok {
					c := WirelessClient{Band: ifaces[iface].band, Iface: iface}
					// #1757: sin SSID no se puede ubicar la estacion en una
					// VLAN; se deja vacio antes que inventarlo.
					c.SSID = ifaces[iface].ssid
					clients[station] = c
				}
			}
			continue
		}
		if station == "" {
			continue
		}
		c := clients[station]
		switch {
		case strings.HasPrefix(trim, "signal:"):
			// signal:   -56 [-62] dBm (el primero es el agregado)
			if f := strings.Fields(trim); len(f) >= 2 {
				if dbm, err := strconv.Atoi(f[1]); err == nil {
					c.SignalDbm = dbm
				}
			}
		case strings.HasPrefix(trim, "rx bytes:"):
			if f := strings.Fields(trim); len(f) >= 3 {
				c.RxBytes, _ = strconv.ParseUint(f[2], 10, 64)
			}
		case strings.HasPrefix(trim, "tx bytes:"):
			if f := strings.Fields(trim); len(f) >= 3 {
				c.TxBytes, _ = strconv.ParseUint(f[2], 10, 64)
			}
		}
		clients[station] = c
	}
	flush()

	radios := []Radio{}
	for _, phy := range phyOrder {
		aps := []iwIface{}
		for _, ifc := range phyIfaces[phy] {
			if ifc.kind == "AP" {
				aps = append(aps, ifc)
			}
		}
		if len(aps) == 0 {
			continue
		}
		inPhy := map[string]bool{}
		for _, ifc := range aps {
			inPhy[ifc.name] = true
		}
		n := 0
		for mac, iface := range stationIface {
			if _, ok := clients[mac]; ok && inPhy[iface] {
				n++
			}
		}
		radios = append(radios, Radio{
			Name: aps[0].band, Channel: aps[0].channel,
			WidthMhz: aps[0].width, BSSID: aps[0].addr, Clients: n,
		})
	}
	return clients, radios
}
