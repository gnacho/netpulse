// demo_extras.go — routerExtras canónicos + series sintéticas deterministas
// (port de routerExtras.ts vía dataset.js, SPEC §7.1).
package adapters

import (
	"fmt"
	"math"
	"time"
)

// demoBandSplit es el {band24, band5, cable} de los extras demo.
type demoBandSplit struct {
	Band24 int `json:"band24"`
	Band5  int `json:"band5"`
	Cable  int `json:"cable"`
}

// demoBackhaul es el objeto backhaul de los extras demo ({kind, headline, latencyMs}).
type demoBackhaul struct {
	Kind      string  `json:"kind"`
	Headline  string  `json:"headline"`
	LatencyMs float64 `json:"latencyMs"`
}

// demoPortState es una entrada de extras.ports ({name, up, speed, role}).
type demoPortState struct {
	Name  string `json:"name"`
	Up    bool   `json:"up"`
	Speed string `json:"speed"`
	Role  string `json:"role"`
}

// demoExtras es el objeto routerExtras por router (shape JSON exacto de
// routerExtras.ts). Claves opcionales ausentes con omitempty (paridad
// `undefined`): firmwareBase, firmwareAvailable, gatewayLatencyMs, backhaul.
type demoExtras struct {
	MAC                 string          `json:"mac"`
	Firmware            string          `json:"firmware"`
	FirmwareBase        string          `json:"firmwareBase,omitempty"`
	FirmwareUpdated     bool            `json:"firmwareUpdated"`
	FirmwareAvailable   string          `json:"firmwareAvailable,omitempty"`
	LastReboot          string          `json:"lastReboot"`
	Soc                 string          `json:"soc"`
	Flash               string          `json:"flash"`
	RamMb               int             `json:"ramMb"`
	BandSplit           demoBandSplit   `json:"bandSplit"`
	TrafficNow          float64         `json:"trafficNow"`
	GatewayLatencyMs    *float64        `json:"gatewayLatencyMs,omitempty"`
	GatewayLatencySpark []float64       `json:"gatewayLatencySpark"`
	Backhaul            *demoBackhaul   `json:"backhaul,omitempty"`
	BackhaulSignal      []float64       `json:"backhaulSignal"`
	Radios              []Radio         `json:"radios"`
	Ports               []demoPortState `json:"ports"`
	EthPorts            []EthPort       `json:"ethPorts"`
}

func fptr(v float64) *float64 { return &v }

// canonRouterExtras: port literal de routerExtras (dataset.js:661-781).
func canonRouterExtras() map[string]*demoExtras {
	return map[string]*demoExtras{
		"flint2": {
			MAC: "94:83:C4:2A:7F:10", Firmware: "GL 4.7.0", FirmwareBase: "OpenWrt 21.02",
			FirmwareUpdated: true, LastReboot: "12 nov, 03:12 (mantenimiento)",
			Soc: "MediaTek MT7986A", Flash: "8 GB eMMC", RamMb: 512,
			BandSplit: demoBandSplit{Band24: 4, Band5: 10, Cable: 3},
			TrafficNow: 84.2,
			// GL-MT6000: 1× WAN 2.5G + 4× LAN 1G
			GatewayLatencySpark: []float64{},
			BackhaulSignal:      []float64{},
			Radios:              []Radio{},
			Ports:               []demoPortState{},
			EthPorts: []EthPort{
				{ID: "wan", Label: "WAN", Up: true, Speed: "2.5 Gbps", ConnectedTo: "ONT fibra · Digi", Detail: "84.122.x.x · full duplex"},
				{ID: "lan1", Label: "LAN 1", Up: true, Speed: "1 Gbps", ConnectedTo: "Salón · AX3000T", Detail: "Uplink AP Salón (192.168.8.2)"},
				{ID: "lan2", Label: "LAN 2", Up: true, Speed: "1 Gbps", ConnectedTo: "NAS Synology", Detail: "192.168.8.10 · cable"},
				{ID: "lan3", Label: "LAN 3", Up: true, Speed: "1 Gbps", ConnectedTo: "Estudio · NanoPi R4S", Detail: "Uplink AP Estudio (192.168.8.3)"},
				{ID: "lan4", Label: "LAN 4", Up: false},
			},
		},
		"living": {
			MAC: "50:D2:F5:11:8C:2E", Firmware: "OpenWrt 23.05.5",
			FirmwareUpdated: true, LastReboot: "12 nov, 03:12 (mantenimiento)",
			Soc: "MediaTek MT7981B", Flash: "128 MB NAND", RamMb: 256,
			BandSplit:  demoBandSplit{Band24: 6, Band5: 10, Cable: 2},
			TrafficNow: 51.7, GatewayLatencyMs: fptr(1),
			GatewayLatencySpark: []float64{1, 1, 2, 1, 1, 1, 2, 1, 1, 1, 1, 2, 1, 1, 1, 2, 1, 2, 2, 1, 1, 1, 1, 1},
			Backhaul:            &demoBackhaul{Kind: "cable", Headline: "Cable · 1 Gbps · full duplex", LatencyMs: 1},
			BackhaulSignal:      []float64{58, 59, 60, 59, 58, 58, 60, 61, 60, 59, 60, 60, 61, 60, 59, 60, 61, 62, 61, 60, 60, 59, 60, 60},
			Radios: []Radio{
				{Name: "2.4 GHz", Channel: 6, WidthMhz: 20, PowerDbm: 20, Clients: 6},
				{Name: "5 GHz", Channel: 36, WidthMhz: 80, PowerDbm: 23, Clients: 10},
			},
			Ports: []demoPortState{
				{Name: "br-lan", Up: true, Speed: "—", Role: "Bridge LAN"},
				{Name: "eth0", Up: true, Speed: "1 Gbps", Role: "Uplink al Gateway"},
				{Name: "lan1", Up: true, Speed: "1 Gbps", Role: "PS5"},
				{Name: "phy0-ap0", Up: true, Speed: "—", Role: "Radio 2.4 GHz"},
				{Name: "phy1-ap0", Up: true, Speed: "—", Role: "Radio 5 GHz"},
			},
			// AX3000T: 1× WAN + 3× LAN
			EthPorts: []EthPort{
				{ID: "wan", Label: "WAN", Up: true, Speed: "1 Gbps", ConnectedTo: "Uplink → Gateway", Detail: "Gateway · LAN 1 (192.168.8.1)"},
				{ID: "lan1", Label: "LAN 1", Up: true, Speed: "1 Gbps", ConnectedTo: "PS5", Detail: "192.168.8.31 · cable"},
				{ID: "lan2", Label: "LAN 2", Up: false},
				{ID: "lan3", Label: "LAN 3", Up: false},
			},
		},
		"estudio": {
			MAC: "7A:1B:9C:03:F4:62", Firmware: "OpenWrt 23.05.5",
			FirmwareUpdated: false, FirmwareAvailable: "24.10.1", LastReboot: "2 dic, 21:40 (actualización)",
			Soc: "Rockchip RK3399", Flash: "32 GB microSD", RamMb: 1024,
			BandSplit:  demoBandSplit{Band24: 4, Band5: 4, Cable: 1},
			TrafficNow: 9.4, GatewayLatencyMs: fptr(1),
			GatewayLatencySpark: []float64{1, 1, 1, 2, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 2, 1, 1, 1, 1},
			Backhaul:            &demoBackhaul{Kind: "cable", Headline: "Cable · 1 Gbps · full duplex", LatencyMs: 1},
			BackhaulSignal:      []float64{55, 56, 56, 55, 55, 56, 57, 56, 55, 56, 56, 57, 56, 56, 55, 56, 57, 57, 56, 56, 56, 55, 56, 56},
			Radios: []Radio{
				{Name: "2.4 GHz", Channel: 11, WidthMhz: 20, PowerDbm: 18, Clients: 4},
				{Name: "5 GHz", Channel: 44, WidthMhz: 80, PowerDbm: 21, Clients: 4},
			},
			Ports: []demoPortState{
				{Name: "br-lan", Up: true, Speed: "—", Role: "Bridge LAN"},
				{Name: "eth0", Up: true, Speed: "1 Gbps", Role: "Uplink al Gateway"},
				{Name: "eth1", Up: true, Speed: "1 Gbps", Role: "Switch estudio"},
				{Name: "phy0-ap0", Up: true, Speed: "—", Role: "Radio 2.4 GHz"},
				{Name: "phy1-ap0", Up: true, Speed: "—", Role: "Radio 5 GHz"},
			},
			// NanoPi R4S: 2× 1G (WAN + LAN)
			EthPorts: []EthPort{
				{ID: "wan", Label: "WAN", Up: true, Speed: "1 Gbps", ConnectedTo: "Uplink → Gateway", Detail: "Gateway · LAN 3 (192.168.8.1)"},
				{ID: "lan1", Label: "LAN 1", Up: true, Speed: "1 Gbps", ConnectedTo: "Switch estudio", Detail: "Switch 8 puertos · 4 en uso"},
			},
		},
		"patio": {
			MAC: "C0:4A:00:9B:51:8D", Firmware: "OpenWrt 23.05.5",
			FirmwareUpdated: false, FirmwareAvailable: "24.10.1", LastReboot: "9 dic, 14:05 (corte eléctrico)",
			Soc: "Qualcomm QCA9563", Flash: "16 MB SPI", RamMb: 128,
			BandSplit:  demoBandSplit{Band24: 5, Band5: 1, Cable: 0},
			TrafficNow: 1.8, GatewayLatencyMs: fptr(2),
			GatewayLatencySpark: []float64{2, 2, 3, 2, 2, 2, 3, 2, 2, 3, 2, 2, 3, 3, 2, 2, 3, 4, 3, 2, 2, 2, 2, 2},
			Backhaul:            &demoBackhaul{Kind: "wireless", Headline: "−58 dBm · 866 Mbps PHY", LatencyMs: 2},
			BackhaulSignal:      []float64{-60, -61, -61, -62, -61, -60, -59, -60, -61, -60, -59, -58, -59, -60, -60, -59, -58, -57, -58, -59, -60, -59, -58, -58},
			Radios: []Radio{
				{Name: "2.4 GHz", Channel: 1, WidthMhz: 20, PowerDbm: 20, Clients: 5, Congested: true},
				{Name: "5 GHz", Channel: 149, WidthMhz: 80, PowerDbm: 22, Clients: 1},
			},
			Ports: []demoPortState{
				{Name: "br-lan", Up: true, Speed: "—", Role: "Bridge LAN"},
				{Name: "eth0", Up: false, Speed: "—", Role: "Puerto LAN (sin uso)"},
				{Name: "phy0-ap0", Up: true, Speed: "—", Role: "Radio 2.4 GHz"},
				{Name: "phy1-sta0", Up: true, Speed: "866 Mbps", Role: "Uplink wireless al Gateway"},
			},
			// EAP225: 1× 1G — sin uso (uplink inalámbrico mesh, ver backhaul)
			EthPorts: []EthPort{
				{ID: "lan1", Label: "LAN", Up: false, Detail: "Uplink por WiFi mesh"},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// perfSeries (dataset.js:816-845) — deterministas, terminan en el valor actual
// ---------------------------------------------------------------------------

// noise replica el ruido determinista 0..1 del JS.
func noise(i int, seed int) float64 {
	x := math.Sin(float64(i)*127.1+float64(seed)*311.7) * 43758.5453
	return x - math.Floor(x)
}

// hourRangeLabels: etiquetas 1h — puntos cada 3 minutos terminando "ahora".
func hourRangeLabels(n int) []string {
	labels := make([]string, 0, n)
	now := time.Now()
	for i := n - 1; i >= 0; i-- {
		d := now.Add(-time.Duration(i) * 3 * time.Minute)
		labels = append(labels, fmt.Sprintf("%02d:%02d", d.Hour(), d.Minute()))
	}
	return labels
}

var dayLabels = []string{"Lun", "Mar", "Mié", "Jue", "Vie", "Sáb", "Dom"}

// canonRouterIDs es el orden canónico (seed = índice + 1).
var canonRouterIDs = []string{"flint2", "living", "estudio", "patio"}

// perfSeries replica perfSeries(router, range): range ∈ "1h"|"24h"|"7d".
// cpu/ram/temp son los valores ACTUALES del router (canon o caminados).
func perfSeries(routerID string, cpu, ram, temp int, rang string) []PerfPoint {
	seed := 1
	for i, id := range canonRouterIDs {
		if id == routerID {
			seed = i + 1
		}
	}
	isGateway := routerID == "flint2"
	var n int
	var labels []string
	switch rang {
	case "1h":
		n = 20
		labels = hourRangeLabels(n)
	case "24h":
		n = 24
		labels = make([]string, n)
		for i := range labels {
			labels[i] = fmt.Sprintf("%02d:00", i)
		}
	default: // "7d"
		n = 7
		labels = dayLabels
	}

	peakIdx := 4 // 7d
	if rang == "24h" {
		peakIdx = 21
	} else if rang == "1h" {
		peakIdx = 15
	}
	cpuPeak := cpu + 34
	if cpuPeak > 88 {
		cpuPeak = 88
	}
	if isGateway {
		cpuPeak = 61
	}
	tempPeak := temp + 4
	if isGateway {
		tempPeak = 58
	}

	points := make([]PerfPoint, 0, n)
	for i := 0; i < n; i++ {
		var dayShape float64
		if rang == "7d" {
			dayShape = 0.7 + 0.3*(float64(i)/float64(n-1))
		} else {
			s := math.Sin((float64(i)-6)/float64(n)*math.Pi*2 - math.Pi/2)
			dayShape = 0.55 + 0.45*s*s
		}
		wob := noise(i, seed)
		c := math.Max(2, math.Round(float64(cpu)*dayShape+wob*6))
		r := math.Max(10, math.Round(float64(ram)-6+wob*10+(float64(i)/float64(n))*4))
		t := math.Max(30, math.Round(float64(temp)-5+wob*4+dayShape*4))
		if i == peakIdx {
			c = float64(cpuPeak)
			t = float64(tempPeak)
		}
		points = append(points, PerfPoint{T: labels[i], CPU: c, RAM: r, Temp: t})
	}
	// La serie termina SIEMPRE en el valor actual del router.
	points[n-1] = PerfPoint{T: labels[n-1], CPU: float64(cpu), RAM: float64(ram), Temp: float64(temp)}
	return points
}

// ---------------------------------------------------------------------------
// Latencia WAN 24h (canon: pico 34 ms a las 21:00)
// ---------------------------------------------------------------------------

func canonWANLatency24h() []float64 {
	return []float64{9, 8, 8, 7, 7, 8, 9, 10, 11, 12, 11, 12, 13, 12, 11, 12, 14, 16, 19, 24, 29, 34, 14, 8}
}

func canonWANLatencyStats() WANLatencyStats {
	return WANLatencyStats{AvgMs: 11, JitterMs: 2.1, LossPct: 0}
}

// ---------------------------------------------------------------------------
// adguardSeries24h (buildAdGuardSeries, dataset.js:862-884): suma exacta del
// canon (84312 / 15687), punto 21:00 fijado a 5412/1031, drift en la hora 20.
// ---------------------------------------------------------------------------

func buildAdGuardSeries() []AdGuardHour {
	weights := []float64{0.35, 0.22, 0.15, 0.12, 0.12, 0.18, 0.35, 0.6, 0.8, 0.9, 1, 1.05, 1.1, 1.05, 1, 1.05, 1.15, 1.3, 1.5, 1.7, 1.9, 2.1, 1.4, 0.7}
	const totalQ, totalB = 84312, 15687
	sumW := 0.0
	for _, w := range weights {
		sumW += w
	}
	q := make([]int64, len(weights))
	for i, w := range weights {
		q[i] = int64(math.Round(w / sumW * totalQ))
	}
	q[21] = 5412
	var sumQ int64
	for _, v := range q {
		sumQ += v
	}
	q[20] += totalQ - sumQ // drift absorbido en la hora 20
	b := make([]int64, len(weights))
	for i, v := range q {
		b[i] = int64(math.Round(float64(v) * (float64(totalB) / float64(totalQ))))
	}
	b[21] = 1031
	var sumB int64
	for _, v := range b {
		sumB += v
	}
	b[20] += totalB - sumB
	out := make([]AdGuardHour, len(weights))
	for i := range weights {
		out[i] = AdGuardHour{T: fmt.Sprintf("%02d:00", i), Permitidas: q[i] - b[i], Bloqueadas: b[i]}
	}
	return out
}

// ---------------------------------------------------------------------------
// WireGuard extras (wgPeerExtras + WG_TOTALS_30D)
// ---------------------------------------------------------------------------

// wgPeerExtra es {endpoint, allowedIps, lastIp} por peer (shape routerExtras.ts).
type wgPeerExtra struct {
	Endpoint   string `json:"endpoint"`
	AllowedIPs string `json:"allowedIps"`
	LastIP     string `json:"lastIp"`
}

func canonWGPeerExtras() map[string]wgPeerExtra {
	return map[string]wgPeerExtra{
		"pixel-8-pro":      {Endpoint: "5.224.x.x:51820", AllowedIPs: "10.0.0.2/32", LastIP: "5.224.x.x"},
		"macbook-air":      {Endpoint: "92.58.x.x:51820", AllowedIPs: "10.0.0.3/32", LastIP: "92.58.x.x"},
		"ipad-air":         {Endpoint: "—", AllowedIPs: "10.0.0.4/32", LastIP: "81.34.x.x"},
		"portatil-trabajo": {Endpoint: "—", AllowedIPs: "10.0.0.5/32", LastIP: "80.102.x.x"},
		"casa-familia":     {Endpoint: "—", AllowedIPs: "10.0.0.6/32", LastIP: "95.60.x.x"},
	}
}

func canonWGTotals30d() *WGTotals { return &WGTotals{Rx: "17,9 GB", Tx: "5,0 GB"} }
