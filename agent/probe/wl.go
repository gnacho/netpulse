// wl.go — último recurso para drivers Broadcom dhd (Asuswrt/Merlin con
// wl): el nl80211 de esos drivers no expone NI la tabla de estaciones NI el
// canal (`iw station dump` vacío, `iw info` sin Channel), así que una radio
// con clientes reales parece vacía: el panel de radios no trae banda y los
// clientes solo existen como hint wireless-side con banda "—". `wl` sí lo
// sabe todo: band (a/b), channel, assoclist y rssi por estación.
package probe

import (
	"strconv"
	"strings"
)

// CmdWlWireless: un solo spawn. Por interfaz AP de `iw dev` emite
// "==IF== <iface>", la dirección (BSSID), la letra de banda, la línea de
// canal y una línea "STA <mac> <rssi>" por estación asociada. Sin `wl`
// (no-Broadcom) o sin permisos la salida queda vacía: best-effort como el
// resto de sondas. OJO: sin `command -v` (el ash de Merlin no lo tiene).
const CmdWlWireless = `if [ -x /usr/sbin/wl ]; then for i in $(iw dev 2>/dev/null | awk '/^[ \t]*Interface /{n=$2} /^[ \t]*type AP/{print n}'); do echo "==IF== $i"; cat /sys/class/net/$i/address 2>/dev/null; /usr/sbin/wl -i "$i" band 2>/dev/null | head -1; /usr/sbin/wl -i "$i" channel 2>/dev/null | tail -1; for m in $(/usr/sbin/wl -i "$i" assoclist 2>/dev/null | awk '/^assoclist/{print $2}'); do echo "STA $m $(/usr/sbin/wl -i "$i" rssi "$m" 2>/dev/null)"; done; done; fi`

// wlBand: la letra de `wl band` a banda canónica. "b" = 2.4G (802.11b/g/n),
// "a" = 5G; el resto (n/ax/be y variantes 6G) se decide por canal.
func wlBand(letter string, channel int) string {
	switch strings.TrimSpace(strings.ToLower(letter)) {
	case "b":
		return "2.4 GHz"
	case "a":
		return "5 GHz"
	}
	if channel > 0 {
		if channel <= 14 {
			return "2.4 GHz"
		}
		return "5 GHz"
	}
	return ""
}

// ParseWlWireless parsea la salida de CmdWlWireless. Contratos:
//   - clientes: MAC upper → WirelessClient{SignalDbm, Band de su interfaz};
//     rssi no numérico (estación recién ido) → cliente con señal 0 omitida
//     vía SignalDbm=0 igualmente, la presencia es lo que importa.
//   - radios: una entrada por interfaz con BSSID (dirección del puerto) y
//     banda/canal conocidos; sin banda no se emite radio (mejor nada que
//     una banda inventada).
func ParseWlWireless(out string) (clients map[string]WirelessClient, radios []Radio) {
	clients = map[string]WirelessClient{}
	curBand := ""
	curAddr := ""
	curCh := 0
	pending := 0
	flush := func() {
		if b := wlBand(curBand, curCh); b != "" && curAddr != "" {
			radios = append(radios, Radio{Name: b, Channel: curCh, BSSID: strings.ToUpper(curAddr), Clients: pending})
		}
	}
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
		case strings.HasPrefix(t, "==IF== "):
			flush()
			curBand, curAddr, curCh, pending = "", "", 0, 0
		case len(t) == 17 && !strings.ContainsAny(t, " "):
			curAddr = t
		case t == "a" || t == "b" || t == "n" || t == "ac" || t == "ax" || t == "be":
			curBand = t
		case strings.Contains(t, "channel"):
			fs := strings.Fields(t)
			if len(fs) > 0 {
				if ch, err := strconv.Atoi(fs[len(fs)-1]); err == nil {
					curCh = ch
				}
			}
		case strings.HasPrefix(t, "STA "):
			fs := strings.Fields(t)
			if len(fs) < 3 {
				continue
			}
			sig, err := strconv.Atoi(fs[2])
			if err != nil || sig >= 0 {
				continue // estación recién ida o rssi no numérico: sin señal fiable, no se inventa
			}
			band := wlBand(curBand, curCh)
			if band == "" {
				band = "—"
			}
			clients[strings.ToUpper(fs[1])] = WirelessClient{SignalDbm: sig, Band: band}
			pending++
		}
	}
	flush()
	if len(clients) == 0 {
		clients = nil
	}
	return clients, radios
}
