// reanchor.go — recomendaciones de re-anclaje WiFi (issue #403).
//
// El motor prueba primero usteer (daemon preferido) y, si no está disponible,
// recurre a DAWN. Para usteer usa `local_info` + `remote_info` para el
// inventario de APs, `connected_clients` para saber dónde está cada cliente y
// `get_clients` como hearing map. Para DAWN usa `get_network` + `get_hearing_map`.
package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Umbrales por defecto del re-anclaje: -65 dBm mínimo para el destino y 10 dBm
// de mejora respecto al AP actual.
const (
	reanchorMinRecommendedSignal = -65
	reanchorMinDeltaDbm          = 10
)

// GetReanchorRecommendations devuelve recomendaciones de re-anclaje WiFi.
// Prefiere usteer; si ningún router responde con usteer, prueba DAWN.
func (l *Live) GetReanchorRecommendations(ctx context.Context, cfg ReanchorConfig) ([]ReanchorRecommendation, RoamingDaemon, error) {
	l.mu.Lock()
	routers := append([]RouterConfig(nil), l.routers...)
	l.mu.Unlock()

	if cfg.MinRecommendedSignal == 0 {
		cfg.MinRecommendedSignal = reanchorMinRecommendedSignal
	}
	if cfg.MinDeltaDbm == 0 {
		cfg.MinDeltaDbm = reanchorMinDeltaDbm
	}

	if recs, ok := usteerReanchor(ctx, routers, cfg, l.pool); ok {
		return recs, RoamingDaemonUsteer, nil
	}
	if recs, ok := dawnReanchor(ctx, routers, cfg, l.pool); ok {
		return recs, RoamingDaemonDawn, nil
	}
	return []ReanchorRecommendation{}, RoamingDaemonNone, nil
}

// ---------------------------------------------------------------------------
// usteer
// ---------------------------------------------------------------------------

// usteerHearingRaw es una entrada de `ubus call usteer get_clients`:
// { "connected": bool, "signal": int }.
type usteerHearingRaw struct {
	Connected bool `json:"connected"`
	Signal    int  `json:"signal"`
}

// usteerAPInfo almacena la metainformación de un AP (local o remoto) para
// convertir el nombre de nodo devuelto por usteer en datos legibles.
type usteerAPInfo struct {
	BSSID    string
	SSID     string
	Freq     int
	Hostname string
	Iface    string
	Local    bool
	Host     string // solo para APs locales; SSH host donde ejecutar del_client
}

// usteerReanchor devuelve (recomendaciones, true) si al menos un router responde
// con usteer local_info. Si no hay usteer devuelve (nil, false).
func usteerReanchor(ctx context.Context, routers []RouterConfig, cfg ReanchorConfig, runner sshRunner) ([]ReanchorRecommendation, bool) {
	apByNode := make(map[string]usteerAPInfo) // nombre de nodo usteer -> AP
	currentByMAC := make(map[string]struct {
		node   string
		signal int
		host   string
	})
	hearing := make(map[string]map[string]int) // MAC -> nodo -> mejor señal
	found := false

	for _, rt := range routers {
		if rt.AgentOnly || rt.Host == "" {
			continue
		}
		name := rt.Name
		if name == "" {
			name = rt.ID
		}

		// APs locales: nombre de nodo = iface.
		localOut, _ := runner.Run(rt.Host, "ubus call usteer local_info", 0)
		var local map[string]usteerAPRaw
		if err := json.Unmarshal([]byte(localOut), &local); err != nil {
			continue
		}
		if len(local) > 0 {
			found = true
		}
		for iface, raw := range local {
			if raw.SSID == "" || raw.BSSID == "" {
				continue
			}
			apByNode[iface] = usteerAPInfo{
				BSSID:    strings.ToUpper(raw.BSSID),
				SSID:     raw.SSID,
				Freq:     raw.Freq,
				Hostname: name,
				Iface:    iface,
				Local:    true,
				Host:     rt.Host,
			}
		}

		// APs remotos: nombre de nodo = "IP#iface".
		remoteOut, _ := runner.Run(rt.Host, "ubus call usteer remote_info", 0)
		var remote map[string]usteerAPRaw
		if err := json.Unmarshal([]byte(remoteOut), &remote); err == nil {
			for node, raw := range remote {
				if raw.SSID == "" || raw.BSSID == "" {
					continue
				}
				if _, exists := apByNode[node]; exists {
					continue
				}
				apByNode[node] = usteerAPInfo{
					BSSID:    strings.ToUpper(raw.BSSID),
					SSID:     raw.SSID,
					Freq:     raw.Freq,
					Hostname: usteerRemoteHostname(node, routers, raw.BSSID),
					Iface:    usteerRemoteIface(node),
					Local:    false,
				}
			}
		}

		// Clientes conectados en este router (solo nodos locales).
		clientsOut, _ := runner.Run(rt.Host, "ubus call usteer connected_clients", 0)
		var clients map[string]map[string]usteerClientRaw
		if err := json.Unmarshal([]byte(clientsOut), &clients); err == nil {
			for iface, macs := range clients {
				for mac, c := range macs {
					if c.Signal >= 0 {
						continue
					}
					mac = strings.ToUpper(mac)
					currentByMAC[mac] = struct {
						node   string
						signal int
						host   string
					}{node: iface, signal: c.Signal, host: rt.Host}
				}
			}
		}

		// Hearing map global de usteer.
		hearOut, _ := runner.Run(rt.Host, "ubus call usteer get_clients", 0)
		var hear map[string]map[string]usteerHearingRaw
		if err := json.Unmarshal([]byte(hearOut), &hear); err == nil {
			for mac, nodes := range hear {
				mac = strings.ToUpper(mac)
				if hearing[mac] == nil {
					hearing[mac] = make(map[string]int)
				}
				for node, h := range nodes {
					if h.Signal >= 0 {
						continue
					}
					if prev, ok := hearing[mac][node]; !ok || h.Signal > prev {
						hearing[mac][node] = h.Signal
					}
				}
			}
		}
	}

	if !found {
		return nil, false
	}

	var recs []ReanchorRecommendation
	for mac, current := range currentByMAC {
		hearMap, ok := hearing[mac]
		if !ok {
			continue
		}
		currentAP, ok := apByNode[current.node]
		if !ok {
			continue
		}
		var bestNode string
		var bestSignal int
		for node, signal := range hearMap {
			if node == current.node {
				continue
			}
			if bestNode == "" || signal > bestSignal {
				bestNode = node
				bestSignal = signal
			}
		}
		if bestNode == "" {
			continue
		}
		if bestSignal < cfg.MinRecommendedSignal {
			continue
		}
		if bestSignal-current.signal < cfg.MinDeltaDbm {
			continue
		}
		recommendedAP := apByNode[bestNode]
		recs = append(recs, ReanchorRecommendation{
			MAC:                 mac,
			CurrentBSSID:        currentAP.BSSID,
			CurrentHostname:     currentAP.Hostname,
			CurrentIface:        currentAP.Iface,
			CurrentHost:         current.host,
			CurrentSignal:       current.signal,
			RecommendedBSSID:    recommendedAP.BSSID,
			RecommendedHostname: recommendedAP.Hostname,
			RecommendedIface:    recommendedAP.Iface,
			RecommendedSignal:   bestSignal,
			DeltaDbm:            bestSignal - current.signal,
		})
	}

	sort.Slice(recs, func(i, j int) bool {
		if recs[i].DeltaDbm != recs[j].DeltaDbm {
			return recs[i].DeltaDbm > recs[j].DeltaDbm
		}
		return recs[i].MAC < recs[j].MAC
	})
	return recs, true
}

// usteerRemoteHostname devuelve un nombre amigable para un nodo remoto. Si el
// prefijo IP coincide con un router conocido, usa su nombre; si no, muestra
// la propia IP.
func usteerRemoteHostname(node string, routers []RouterConfig, bssid string) string {
	const sep = "#"
	idx := strings.Index(node, sep)
	if idx < 0 {
		return bssid
	}
	ip := node[:idx]
	for _, r := range routers {
		if r.Host == ip {
			name := r.Name
			if name == "" {
				name = r.ID
			}
			return name
		}
	}
	return ip
}

// usteerRemoteIface extrae la parte de iface del nombre de nodo remoto
// ("IP#iface" -> "iface").
func usteerRemoteIface(node string) string {
	const sep = "#"
	idx := strings.Index(node, sep)
	if idx < 0 {
		return node
	}
	return node[idx+len(sep):]
}

// ---------------------------------------------------------------------------
// DAWN (fallback)
// ---------------------------------------------------------------------------

// DawnNetworkFromHost ejecuta `ubus call dawn get_network` en un host concreto
// y devuelve los APs parseados. Devuelve nil si falla el SSH o el parseo.
func DawnNetworkFromHost(ctx context.Context, host string, runner sshRunner) []DawnAP {
	if host == "" {
		return nil
	}
	out, err := runner.Run(host, "ubus call dawn get_network", 0)
	if err != nil {
		return nil
	}
	var data map[string]map[string]json.RawMessage
	if json.Unmarshal([]byte(out), &data) != nil {
		return nil
	}
	return dawnAPsFromNetwork(data)
}

// DawnHearingMapFromHost ejecuta `ubus call dawn get_hearing_map` en un host
// concreto. Devuelve map[MAC] -> map[BSSID] -> signal. Los MAC y BSSID se
// normalizan a mayúsculas.
func DawnHearingMapFromHost(ctx context.Context, host string, runner sshRunner) map[string]map[string]int {
	if host == "" {
		return nil
	}
	out, err := runner.Run(host, "ubus call dawn get_hearing_map", 0)
	if err != nil {
		return nil
	}
	return parseDawnHearingMap(out)
}

// parseDawnHearingMap parsea la salida JSON de `ubus call dawn get_hearing_map`.
// Estructura: { SSID: { MAC: { BSSID: { signal, freq, ... } } } }
func parseDawnHearingMap(out string) map[string]map[string]int {
	var raw map[string]map[string]map[string]map[string]json.RawMessage
	if json.Unmarshal([]byte(out), &raw) != nil {
		return nil
	}
	res := make(map[string]map[string]int)
	for _, clients := range raw {
		for mac, bssids := range clients {
			mac = strings.ToUpper(mac)
			if res[mac] == nil {
				res[mac] = make(map[string]int)
			}
			for bssid, fields := range bssids {
				bssid = strings.ToUpper(bssid)
				signalRaw, ok := fields["signal"]
				if !ok {
					continue
				}
				var s int
				if json.Unmarshal(signalRaw, &s) != nil {
					continue
				}
				// signal=0 suele significar "no medido"; lo ignoramos.
				if s >= 0 {
					continue
				}
				// Quedarse con la mejor señal (mayor valor, -50 > -80).
				if prev, ok := res[mac][bssid]; !ok || s > prev {
					res[mac][bssid] = s
				}
			}
		}
	}
	return res
}

// dawnAPsFromNetwork extrae los APs del JSON de `ubus call dawn get_network`.
// Estructura: { SSID: { BSSID: { ... } } }
func dawnAPsFromNetwork(data map[string]map[string]json.RawMessage) []DawnAP {
	var aps []DawnAP
	for ssid, bssids := range data {
		for bssidRaw, raw := range bssids {
			bssid := strings.ToUpper(bssidRaw)
			var ap dawnAPData
			if json.Unmarshal(raw, &ap) != nil {
				continue
			}
			band := "2.4 GHz"
			if ap.Freq >= 5000 {
				band = "5 GHz"
			}
			var clients []DawnClient
			for mac, c := range ap.Clients {
				if c.Signal >= 0 {
					continue
				}
				clients = append(clients, DawnClient{
					MAC:    strings.ToUpper(mac),
					Signal: c.Signal,
					HT:     c.HT,
					VHT:    c.VHT,
				})
			}
			sort.Slice(clients, func(i, j int) bool { return clients[i].MAC < clients[j].MAC })
			aps = append(aps, DawnAP{
				SSID:           ssid,
				BSSID:          bssid,
				Hostname:       ap.Hostname,
				Band:           band,
				Channel:        ap.Channel,
				UtilizationPct: ap.UtilizationPct,
				ClientCount:    ap.ClientCount,
				Clients:        clients,
				Local:          ap.Local,
				Iface:          ap.Iface,
			})
		}
	}
	return aps
}

// dawnAPData es la forma interna de un AP en `get_network`.
type dawnAPData struct {
	Hostname       string                     `json:"hostname"`
	Freq           int                        `json:"freq"`
	Channel        int                        `json:"channel"`
	UtilizationPct float64                    `json:"utilization"`
	ClientCount    int                        `json:"num_sta"`
	Clients        map[string]dawnClientData  `json:"clients"`
	Local          bool                       `json:"local"`
	Iface          string                     `json:"iface"`
}

type dawnClientData struct {
	Signal int  `json:"signal"`
	HT     bool `json:"ht"`
	VHT    bool `json:"vht"`
}

// dawnReanchor devuelve (recomendaciones, true) si al menos un router responde
// con DAWN. Si no hay DAWN devuelve (nil, false).
func dawnReanchor(ctx context.Context, routers []RouterConfig, cfg ReanchorConfig, runner sshRunner) ([]ReanchorRecommendation, bool) {
	apByBSSID := make(map[string]DawnAP)
	currentByMAC := make(map[string]struct {
		bssid  string
		signal int
		host   string
	})
	hearing := make(map[string]map[string]int)
	found := false

	for _, rt := range routers {
		if rt.AgentOnly || rt.Host == "" {
			continue
		}
		aps := DawnNetworkFromHost(ctx, rt.Host, runner)
		if len(aps) > 0 {
			found = true
		}
		for _, ap := range aps {
			ap.BSSID = strings.ToUpper(ap.BSSID)
			if _, exists := apByBSSID[ap.BSSID]; !exists {
				apByBSSID[ap.BSSID] = ap
			}
			if !ap.Local {
				continue
			}
			for _, c := range ap.Clients {
				c.MAC = strings.ToUpper(c.MAC)
				currentByMAC[c.MAC] = struct {
					bssid  string
					signal int
					host   string
				}{bssid: ap.BSSID, signal: c.Signal, host: rt.Host}
			}
		}

		hm := DawnHearingMapFromHost(ctx, rt.Host, runner)
		for mac, bssids := range hm {
			if hearing[mac] == nil {
				hearing[mac] = make(map[string]int)
			}
			for bssid, signal := range bssids {
				if prev, ok := hearing[mac][bssid]; !ok || signal > prev {
					hearing[mac][bssid] = signal
				}
			}
		}
	}

	if !found {
		return nil, false
	}

	var recs []ReanchorRecommendation
	for mac, current := range currentByMAC {
		hearingMap, ok := hearing[mac]
		if !ok {
			continue
		}
		currentAP := apByBSSID[current.bssid]
		var bestBSSID string
		var bestSignal int
		for bssid, signal := range hearingMap {
			if bssid == current.bssid {
				continue
			}
			if bestBSSID == "" || signal > bestSignal {
				bestBSSID = bssid
				bestSignal = signal
			}
		}
		if bestBSSID == "" {
			continue
		}
		if bestSignal < cfg.MinRecommendedSignal {
			continue
		}
		if bestSignal-current.signal < cfg.MinDeltaDbm {
			continue
		}
		recommendedAP := apByBSSID[bestBSSID]
		recs = append(recs, ReanchorRecommendation{
			MAC:                 mac,
			CurrentBSSID:        current.bssid,
			CurrentHostname:     currentAP.Hostname,
			CurrentIface:        currentAP.Iface,
			CurrentHost:         current.host,
			CurrentSignal:       current.signal,
			RecommendedBSSID:    bestBSSID,
			RecommendedHostname: recommendedAP.Hostname,
			RecommendedIface:    recommendedAP.Iface,
			RecommendedSignal:   bestSignal,
			DeltaDbm:            bestSignal - current.signal,
		})
	}

	sort.Slice(recs, func(i, j int) bool {
		if recs[i].DeltaDbm != recs[j].DeltaDbm {
			return recs[i].DeltaDbm > recs[j].DeltaDbm
		}
		return recs[i].MAC < recs[j].MAC
	})
	return recs, true
}

// ReanchorKickScript genera el comando hostapd del_client para expulsar a un
// cliente de un AP. Se ejecuta en el host donde está el AP actual (DAWN).
func ReanchorKickScript(mac string, iface string) string {
	mac = strings.ToUpper(mac)
	return fmt.Sprintf("ubus call hostapd.%s del_client '{\"addr\":\"%s\",\"reason\":5,\"deauth\":true,\"ban_time\":120000}'",
		iface, mac)
}
