// demo_dataset.go — Dataset demo canónico COMPLETO (port literal de
// server/src/demo/dataset.js, SPEC §7.1): 4 routers, WAN, trafficByRange,
// health score, AdGuard, WireGuard, alertas, los 47 dispositivos (10
// canónicos + 37 adicionales, sparklines deterministas con el mismo LCG),
// routerExtras, perfSeries, WAN_LATENCY_24H, adguardSeries24h, wgPeerExtras
// y WG_TOTALS_30D. Cada llamada a los builders devuelve estructuras NUEVAS
// (el adapter muta su copia con el random walk; el canon no se toca).
package adapters

import (
	"fmt"
	"math"
)

func iptr(v int) *int { return &v }

// ---------------------------------------------------------------------------
// Routers / WAN / tráfico / health / AdGuard / WireGuard / alertas (canon)
// ---------------------------------------------------------------------------

func canonRouters() []Router {
	return []Router{
		{ID: "flint2", Name: "Gateway", Model: "GL.iNet Flint 2 (GL-MT6000)", ModelShort: "GL.iNet Flint 2",
			Role: "Gateway principal", RoleBadge: "Principal", IP: "192.168.8.1", Status: "online",
			Health: 98, CPU: iptr(23), RAM: iptr(41), Temp: iptr(54), Uptime: "32d 14h", Clients: 33,
			Backhaul:  "cable",
			Sparkline: []float64{8, 6, 5, 5, 6, 9, 18, 32, 41, 38, 35, 44, 52, 48, 45, 55, 68, 84, 96, 120, 150, 110, 84, 40}},
		{ID: "living", Name: "Salón", Model: "OpenWrt AP (Xiaomi AX3000T)", ModelShort: "Xiaomi AX3000T",
			Role: "Punto de acceso", RoleBadge: "AP", IP: "192.168.8.2", Status: "online",
			Health: 95, CPU: iptr(12), RAM: iptr(38), Temp: iptr(47), Uptime: "32d 14h", Clients: 22,
			Backhaul:  "cable",
			Sparkline: []float64{4, 3, 3, 2, 3, 5, 10, 22, 28, 26, 24, 30, 38, 35, 33, 42, 55, 72, 88, 105, 132, 92, 61, 28}},
		{ID: "estudio", Name: "Estudio", Model: "OpenWrt (NanoPi R4S)", ModelShort: "NanoPi R4S",
			Role: "AP + switch", RoleBadge: "AP", IP: "192.168.8.3", Status: "online",
			Health: 92, CPU: iptr(18), RAM: iptr(44), Temp: iptr(51), Uptime: "11d 3h", Clients: 9,
			Backhaul:  "cable",
			Sparkline: []float64{2, 2, 1, 1, 2, 4, 8, 15, 22, 25, 24, 22, 26, 24, 21, 24, 28, 31, 29, 24, 18, 12, 8, 4}},
		// Patio: único AP con uplink inalámbrico del dataset (backhaul "wifi").
		{ID: "patio", Name: "Patio", Model: "OpenWrt (TP-Link EAP225)", ModelShort: "TP-Link EAP225",
			Role: "AP exterior", RoleBadge: "AP", IP: "192.168.8.4", Status: "warn",
			Health: 68, CPU: iptr(31), RAM: iptr(57), Temp: iptr(71), Uptime: "4d 2h", Clients: 6, HotMetric: "temp",
			Backhaul:  "wifi",
			Sparkline: []float64{1, 1, 1, 1, 1, 2, 3, 5, 7, 8, 8, 9, 10, 9, 8, 9, 11, 12, 13, 12, 9, 6, 4, 2}},
	}
}

func canonWAN() WAN {
	return WAN{
		Plan: "600/600 Mbps", DownMbps: 84.2, UpMbps: 12.6, LatencyMs: 8, LossPct: 0,
		PublicIP: "84.122.x.x", Isp: "Digi", PeakTodayMbps: 412, PeakTodayTime: "21:14",
		AvgDownMbps: 61, Total24h: "1,32 TB",
	}
}

func tp(t string, down, up float64) TrafficPoint { return TrafficPoint{T: t, Down: down, Up: up} }

func canonTraffic() TrafficByRange {
	return TrafficByRange{
		H1: []TrafficPoint{
			tp("21:30", 68, 9), tp("21:33", 74, 10), tp("21:36", 81, 11), tp("21:39", 92, 12),
			tp("21:42", 88, 10), tp("21:45", 96, 13), tp("21:48", 104, 14), tp("21:51", 98, 12),
			tp("21:54", 90, 11), tp("21:57", 86, 10), tp("22:00", 82, 11), tp("22:03", 78, 12),
			tp("22:06", 84, 13), tp("22:09", 91, 14), tp("22:12", 88, 12), tp("22:15", 84, 11),
			tp("22:18", 80, 10), tp("22:21", 83, 12), tp("22:24", 86, 13), tp("22:27", 84, 12.6),
		},
		H24: []TrafficPoint{
			tp("00", 42, 8), tp("01", 28, 6), tp("02", 15, 5), tp("03", 9, 4),
			tp("04", 7, 4), tp("05", 8, 5), tp("06", 14, 6), tp("07", 32, 9),
			tp("08", 58, 14), tp("09", 74, 18), tp("10", 88, 21), tp("11", 96, 24),
			tp("12", 112, 26), tp("13", 124, 28), tp("14", 118, 25), tp("15", 132, 27),
			tp("16", 148, 30), tp("17", 176, 34), tp("18", 224, 42), tp("19", 298, 55),
			tp("20", 356, 71), tp("21", 412, 96), tp("22", 284, 48), tp("23", 122, 21),
		},
		D7: []TrafficPoint{
			tp("Lun", 61, 12), tp("Mar", 58, 11), tp("Mié", 64, 13), tp("Jue", 71, 15),
			tp("Vie", 88, 19), tp("Sáb", 96, 22), tp("Dom", 84, 18),
		},
		D30: []TrafficPoint{
			tp("1", 54, 10), tp("4", 58, 11), tp("7", 62, 12), tp("10", 57, 11),
			tp("13", 66, 13), tp("16", 72, 15), tp("19", 68, 14), tp("22", 75, 16),
			tp("25", 81, 18), tp("28", 78, 17), tp("30", 84, 19),
		},
	}
}

func canonHealthScore() HealthScore {
	return HealthScore{
		Score: 92, Label: "Excelente", Caption: "Puntuación de salud de la red",
		Note: "Penalizado por la temperatura del Patio.",
		Breakdown: []HealthDelta{
			{Label: "temp. Patio", Delta: -4},
			{Label: "cobertura Patio", Delta: -2},
			{Label: "canal 2.4 GHz congestionado", Delta: -2},
		},
	}
}

func canonAdguard() AdGuardStats {
	return AdGuardStats{
		Host: "192.168.8.1", Port: 3000, Status: "active",
		Queries24h: 84312, Blocked24h: 15687, BlockedPct: 18.6, TrackersBlocked: 9204,
		DNSLatencyMs: 14, ClientsUsing: 60, ClientsTotal: 67,
		TopBlocked: []TopBlocked{
			{Domain: "graph.facebook.com", Count: 1204},
			{Domain: "adservice.google.com", Count: 986},
			{Domain: "metrics.icloud.com", Count: 731},
			{Domain: "telemetry.nvidia.com", Count: 512},
			{Domain: "ads.tiktok.com", Count: 448},
		},
		FilterLists: 6, Rules: 218442,
	}
}

func canonWireguard() WireGuardStats {
	return WireGuardStats{
		Interface: "wg0", Subnet: "10.0.0.1/24", Status: "active",
		Peers: []WGPeer{
			{ID: "pixel-8-pro", Name: "Pixel 8 Pro", Type: "movil", TunnelIP: "10.0.0.2", Active: true, LastHandshake: "hace 38 s", Rx: "1,2 GB", Tx: "214 MB"},
			{ID: "macbook-air", Name: "MacBook Air", Type: "portatil", TunnelIP: "10.0.0.3", Active: true, LastHandshake: "hace 1 min", Rx: "640 MB", Tx: "88 MB"},
			{ID: "ipad-air", Name: "iPad Air", Type: "tablet", TunnelIP: "10.0.0.4", Active: false, LastHandshake: "hace 2 días", Rx: "3,1 GB", Tx: "402 MB"},
			{ID: "portatil-trabajo", Name: "Portátil trabajo", Type: "portatil", TunnelIP: "10.0.0.5", Active: false, LastHandshake: "hace 6 h", Rx: "812 MB", Tx: "121 MB"},
			{ID: "casa-familia", Name: "Casa familia", Type: "sitio", TunnelIP: "10.0.0.6", Active: false, LastHandshake: "hace 9 días", Rx: "12 GB", Tx: "4,2 GB"},
		},
	}
}

func canonAlerts() []AlertEvent {
	return []AlertEvent{
		{ID: "alert-temp-patio", Severity: "warn", Title: "Temperatura alta en Patio",
			Description: "71 °C, por encima del umbral (65 °C)", Time: "hace 12 min", Read: false, RouterID: "patio"},
		{ID: "alert-firmware-estudio", Severity: "warn", Title: "Firmware disponible",
			Description: "OpenWrt 24.10.1 para Estudio", Time: "hace 3 h", Read: false, RouterID: "estudio"},
		{ID: "alert-nuevo-tab", Severity: "info", Title: "Nuevo dispositivo",
			Description: "'Galaxy Tab S9' se ha unido a Salón", Time: "hace 26 min", Read: true, RouterID: "living"},
		{ID: "alert-handshake-wg", Severity: "info", Title: "Handshake WireGuard",
			Description: "Pixel 8 Pro conectado desde 5.224.x.x", Time: "hace 38 s", Read: true, RouterID: "flint2"},
		{ID: "alert-backup-adguard", Severity: "ok", Title: "Copia de AdGuard completada",
			Description: "Configuración y listas respaldadas en el NAS", Time: "hace 1 día", Read: true, RouterID: "flint2"},
	}
}

// ---------------------------------------------------------------------------
// Dispositivos: sparkline determinista (mismo LCG que devices-data.ts) y
// helpers extra()/bulb() del dataset.js
// ---------------------------------------------------------------------------

// spark replica spark(seed, base, spread) de dataset.js: LCG
// s = (s*9301 + 49297) % 233280, 12 puntos, el último es `base`.
func spark(seed int, base, spread float64) []float64 {
	s := seed
	out := make([]float64, 0, 12)
	for i := 0; i < 12; i++ {
		s = (s*9301 + 49297) % 233280
		r := float64(s) / 233280
		v := base + (r-0.45)*spread
		v = math.Round(v*100) / 100 // toFixed(2)
		if v < 0 {
			v = 0
		}
		out = append(out, v)
	}
	out[len(out)-1] = base
	return out
}

// devExtra replica extra(spec): sparkline vacío ([]) si offline.
func devExtra(d Device, seed int, spread float64) Device {
	if d.Online {
		if spread == 0 {
			spread = d.TrafficMbps * 0.8
		}
		d.Sparkline = spark(seed, d.TrafficMbps, spread)
	} else {
		d.Sparkline = []float64{}
	}
	return d
}

// bulb replica bulb(n, name, ip, mac, dbm) del dataset.js.
func bulb(n int, name, ip, mac string, dbm int) Device {
	return Device{
		ID: fmt.Sprintf("bombilla-%d", n), Name: name, Type: "iot", Manufacturer: "Ikea Trådfri",
		IP: ip, MAC: mac, RouterID: "living", Band: "2.4 GHz", SignalDbm: iptr(dbm),
		TrafficMbps: 0, Online: true, Sparkline: spark(40+n, 0.005, 0.02),
		Hostname: fmt.Sprintf("tradfri-%d", n), DHCPLease: "renueva en 12 h 0 min", FirstSeen: "hace 280 días",
		Traffic24hRx: "4 MB", Traffic24hTx: "1 MB", Adguard: boolp(true), Group: "iot",
	}
}

// canonDevices son los 10 destacados (el agregado bombillas-ikea NO está:
// se expande en 6 bulb()), ya fusionados con CANON_DETAILS.
func canonDevices() []Device {
	return []Device{
		{ID: "imac-salon", Name: "iMac Salón", Type: "ordenador", Manufacturer: "Apple",
			IP: "192.168.8.21", MAC: "A4:83:E7:21:0B:3C", RouterID: "living", Band: "5 GHz",
			SignalDbm: iptr(-48), TrafficMbps: 32.4, Online: true,
			Sparkline: []float64{12, 15, 18, 22, 26, 30, 34, 32, 28, 30, 32, 31},
			Hostname:  "imac-de-marc", DHCPLease: "IP fija (reserva)", FirstSeen: "hace 320 días",
			Traffic24hRx: "38 GB", Traffic24hTx: "2,1 GB", Adguard: boolp(true), Group: "ordenadores"},
		{ID: "tv-samsung", Name: "TV Samsung", Type: "tv", Manufacturer: "Samsung",
			IP: "192.168.8.34", MAC: "8C:EA:48:5D:2F:91", RouterID: "living", Band: "5 GHz",
			SignalDbm: iptr(-52), TrafficMbps: 18.1, Online: true,
			Sparkline: []float64{8, 10, 14, 16, 18, 20, 19, 18, 17, 18, 18, 18},
			Hostname:  "samsung-tv-salon", DHCPLease: "renueva en 7 h 48 min", FirstSeen: "hace 290 días",
			Traffic24hRx: "54 GB", Traffic24hTx: "1,2 GB", Adguard: boolp(true), Group: "tv"},
		{ID: "pixel-8-pro", Name: "Pixel 8 Pro", Type: "movil", Manufacturer: "Google",
			IP: "192.168.8.45", MAC: "F2:6D:19:A8:44:C2", RouterID: "flint2", Band: "5 GHz",
			SignalDbm: iptr(-41), TrafficMbps: 6.2, Online: true,
			Sparkline: []float64{3, 4, 5, 7, 6, 8, 6, 5, 7, 6, 6, 6},
			Hostname:  "pixel-8-pro", DHCPLease: "renueva en 9 h 12 min", FirstSeen: "hace 214 días",
			Traffic24hRx: "4,2 GB", Traffic24hTx: "310 MB", Adguard: boolp(true), Group: "moviles"},
		{ID: "macbook-air", Name: "MacBook Air", Type: "portatil", Manufacturer: "Apple",
			IP: "192.168.8.23", MAC: "3C:22:FB:71:9E:05", RouterID: "estudio", Band: "5 GHz",
			SignalDbm: iptr(-45), TrafficMbps: 4.8, Online: true,
			Sparkline: []float64{2, 3, 4, 5, 5, 6, 5, 4, 5, 5, 5, 5},
			Hostname:  "macbook-air-de-ana", DHCPLease: "renueva en 5 h 31 min", FirstSeen: "hace 260 días",
			Traffic24hRx: "18 GB", Traffic24hTx: "2,2 GB", Adguard: boolp(true), Group: "ordenadores"},
		{ID: "ps5", Name: "PS5", Type: "consola", Manufacturer: "Sony",
			IP: "192.168.8.31", MAC: "78:C8:81:0A:6B:D4", RouterID: "living", Band: "cable",
			SignalDbm: nil, TrafficMbps: 12.7, Online: true, Port: "lan2",
			Sparkline: []float64{4, 6, 8, 10, 12, 14, 13, 12, 13, 12, 13, 13},
			Hostname:  "ps5-salon", DHCPLease: "IP fija (reserva)", FirstSeen: "hace 300 días",
			Traffic24hRx: "92 GB", Traffic24hTx: "4,8 GB", Adguard: boolp(true), Group: "tv"},
		{ID: "robot-aspirador", Name: "Robot aspirador", Type: "iot", Manufacturer: "Roborock",
			IP: "192.168.8.61", MAC: "B0:4A:39:2E:77:10", RouterID: "patio", Band: "2.4 GHz",
			SignalDbm: iptr(-67), TrafficMbps: 0.02, Online: true,
			Sparkline: []float64{0.01, 0.02, 0.02, 0.03, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02},
			Hostname:  "roborock-s8", DHCPLease: "renueva en 10 h 2 min", FirstSeen: "hace 180 días",
			Traffic24hRx: "340 MB", Traffic24hTx: "28 MB", Adguard: boolp(true), Group: "iot"},
		{ID: "camara-porche", Name: "Cámara porche", Type: "camara", Manufacturer: "Reolink",
			IP: "192.168.8.71", MAC: "EC:71:DB:44:12:8A", RouterID: "patio", Band: "2.4 GHz",
			SignalDbm: iptr(-72), TrafficMbps: 1.1, Online: true,
			Sparkline: []float64{1, 1.1, 1.1, 1.2, 1.1, 1, 1.1, 1.1, 1.2, 1.1, 1.1, 1.1},
			Hostname:  "reolink-porche", DHCPLease: "renueva en 3 h 57 min", FirstSeen: "hace 150 días",
			Traffic24hRx: "11 GB", Traffic24hTx: "640 MB", Adguard: boolp(true), Group: "iot"},
		{ID: "nest-mini", Name: "Nest Mini", Type: "altavoz", Manufacturer: "Google",
			IP: "192.168.8.52", MAC: "1A:2B:3C:4D:5E:6F", RouterID: "estudio", Band: "2.4 GHz",
			SignalDbm: iptr(-55), TrafficMbps: 0.4, Online: true,
			Sparkline: []float64{0.3, 0.4, 0.4, 0.5, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4},
			Hostname:  "nest-mini-estudio", DHCPLease: "renueva en 8 h 20 min", FirstSeen: "hace 240 días",
			Traffic24hRx: "1,4 GB", Traffic24hTx: "88 MB", Adguard: boolp(true), Group: "iot"},
		{ID: "nas-synology", Name: "NAS Synology", Type: "servidor", Manufacturer: "Synology",
			IP: "192.168.8.10", MAC: "00:11:32:9C:51:B7", RouterID: "flint2", Band: "cable",
			SignalDbm: nil, TrafficMbps: 2.3, Online: true, Port: "lan1",
			Sparkline: []float64{1, 1.5, 2, 2.5, 3, 2.8, 2.4, 2.2, 2.3, 2.3, 2.3, 2.3},
			Hostname:  "diskstation", DHCPLease: "IP fija (reserva)", FirstSeen: "hace 320 días",
			Traffic24hRx: "96 GB", Traffic24hTx: "1,1 TB", Adguard: boolp(true), Group: "red"},
		{ID: "galaxy-tab-s9", Name: "Galaxy Tab S9", Type: "tablet", Manufacturer: "Samsung",
			IP: "192.168.8.48", MAC: "D6:91:2F:07:B3:55", RouterID: "living", Band: "5 GHz",
			SignalDbm: iptr(-50), TrafficMbps: 1.8, Online: true, IsNew: true,
			Sparkline: []float64{0, 0, 0, 0, 0, 0, 0, 0, 0.5, 1.2, 1.6, 1.8},
			Hostname:  "galaxy-tab-s9", DHCPLease: "renueva en 11 h 5 min", FirstSeen: "hoy",
			Traffic24hRx: "640 MB", Traffic24hTx: "48 MB", Adguard: boolp(true), Group: "moviles", NewThisWeek: true},
	}
}

// additionalDevices: ADDITIONAL (flint2) + LIVING + ESTUDIO + PATIO.
func additionalDevices() []Device {
	out := []Device{
		// —— Gateway (flint2) ——
		withDetails(devExtra(Device{ID: "iphone-ana", Name: "iPhone de Ana", Type: "movil", Manufacturer: "Apple",
			IP: "192.168.8.44", MAC: "F4:D4:88:19:C2:71", RouterID: "flint2", Band: "5 GHz",
			SignalDbm: iptr(-46), TrafficMbps: 2.1, Online: true}, 11, 0),
			"iphone-de-ana", "renueva en 6 h 40 min", "hace 190 días", "2,8 GB", "240 MB", true, "moviles", ""),
		withDetails(devExtra(Device{ID: "macbook-pro", Name: "MacBook Pro de Marc", Type: "portatil", Manufacturer: "Apple",
			IP: "192.168.8.26", MAC: "F0:18:98:5A:11:E9", RouterID: "flint2", Band: "5 GHz",
			SignalDbm: iptr(-44), TrafficMbps: 8.6, Online: true}, 12, 0),
			"macbook-pro-marc", "renueva en 4 h 16 min", "hace 230 días", "12,4 GB", "1,9 GB", true, "ordenadores", ""),
		withDetails(devExtra(Device{ID: "pc-sobremesa", Name: "PC de sobremesa", Type: "ordenador", Manufacturer: "ASUSTeK",
			IP: "192.168.8.11", MAC: "04:D4:C4:8B:30:A7", RouterID: "flint2", Band: "cable",
			SignalDbm: nil, TrafficMbps: 21.3, Online: true, AttachTo: "dist-flint2-lan3"}, 13, 0),
			"desktop-8f2k1", "IP fija (reserva)", "hace 310 días", "84 GB", "6,2 GB", true, "ordenadores", ""),
		withDetails(devExtra(Device{ID: "raspberry-pi", Name: "Raspberry Pi 4", Type: "servidor", Manufacturer: "Raspberry Pi",
			IP: "192.168.8.12", MAC: "DC:A6:32:4F:77:02", RouterID: "flint2", Band: "cable",
			SignalDbm: nil, TrafficMbps: 0.8, Online: true, AttachTo: "dist-flint2-lan3"}, 14, 0),
			"raspberrypi", "IP fija (reserva)", "hace 320 días", "3,4 GB", "890 MB", true, "red", ""),
		// Switch gestionado identificado por LLDP (topología v5: type switch + Salón lan3)
		withDetails(devExtra(Device{ID: "switch-netgear", Name: "Switch Netgear", Type: "switch", Manufacturer: "Netgear GS308E",
			IP: "192.168.8.13", MAC: "28:C6:8E:1D:90:44", RouterID: "living", Band: "cable",
			SignalDbm: nil, TrafficMbps: 0.02, Online: true, Port: "lan3",
			Lldp: &LldpInfo{Chassis: "GS308E", Mgmt: "192.168.8.13", Caps: "Bridge", PortDesc: "ge5"}}, 15, 0.04),
			"gs308e", "IP fija (reserva)", "hace 300 días", "64 MB", "12 MB", false, "red", "Network"),
		withDetails(devExtra(Device{ID: "timbre-nest", Name: "Timbre Nest", Type: "camara", Manufacturer: "Google",
			IP: "192.168.8.72", MAC: "F4:F5:D8:66:01:B8", RouterID: "flint2", Band: "2.4 GHz",
			SignalDbm: iptr(-58), TrafficMbps: 0.6, Online: true}, 16, 0),
			"nest-doorbell", "renueva en 9 h 44 min", "hace 170 días", "1,2 GB", "140 MB", true, "iot", ""),
		withDetails(devExtra(Device{ID: "enchufe-lavadora", Name: "Enchufe lavadora", Type: "iot", Manufacturer: "TP-Link",
			IP: "192.168.8.81", MAC: "50:C7:BF:22:E1:9C", RouterID: "flint2", Band: "2.4 GHz",
			SignalDbm: iptr(-62), TrafficMbps: 0.01, Online: true}, 17, 0.02),
			"tapo-p110-lavadora", "renueva en 12 h 0 min", "hace 140 días", "8 MB", "2 MB", false, "iot", ""),
		withDetails(devExtra(Device{ID: "pixel-7", Name: "Pixel 7", Type: "movil", Manufacturer: "Google",
			IP: "192.168.8.46", MAC: "3C:5A:B4:08:D7:5E", RouterID: "flint2", Band: "5 GHz",
			SignalDbm: iptr(-49), TrafficMbps: 1.4, Online: true}, 18, 0),
			"pixel-7", "renueva en 7 h 3 min", "hace 205 días", "1,9 GB", "180 MB", true, "moviles", ""),
		withDetails(devExtra(Device{ID: "impresora-hp", Name: "Impresora HP", Type: "desconocido", Manufacturer: "HP",
			IP: "192.168.8.14", MAC: "3C:52:82:AB:19:60", RouterID: "flint2", Band: "2.4 GHz",
			SignalDbm: iptr(-60), TrafficMbps: 0, Online: false}, 1, 0),
			"hp-laserjet-m209", "Expirado", "hace 250 días", "0 MB", "0 MB", true, "otros", "Printer"),
		withDetails(devExtra(Device{ID: "ipad-air", Name: "iPad Air", Type: "tablet", Manufacturer: "Apple",
			IP: "192.168.8.47", MAC: "8C:85:90:2F:B4:11", RouterID: "flint2", Band: "5 GHz",
			SignalDbm: iptr(-54), TrafficMbps: 0, Online: false}, 1, 0),
			"ipad-air", "Expirado", "hace 260 días", "6,4 GB", "520 MB", true, "moviles", ""),
		withDetails(devExtra(Device{ID: "portatil-trabajo", Name: "Portátil trabajo", Type: "portatil", Manufacturer: "Lenovo",
			IP: "192.168.8.27", MAC: "54:EE:75:9A:03:F1", RouterID: "flint2", Band: "5 GHz",
			SignalDbm: iptr(-50), TrafficMbps: 0, Online: false}, 1, 0),
			"thinkpad-t14", "Expirado", "hace 160 días", "812 MB", "121 MB", true, "ordenadores", ""),
		withDetails(devExtra(Device{ID: "kindle", Name: "Kindle Paperwhite", Type: "desconocido", Manufacturer: "Amazon",
			IP: "192.168.8.49", MAC: "44:65:0D:71:28:C3", RouterID: "flint2", Band: "2.4 GHz",
			SignalDbm: iptr(-58), TrafficMbps: 0, Online: false}, 1, 0),
			"kindle-paperwhite", "Expirado", "hace 220 días", "220 MB", "8 MB", true, "otros", "BookOpen"),
		// —— Salón (living) ——
		bulb(1, "Bombilla salón 1", "192.168.8.90", "CC:86:EC:10:04:21", -58),
		bulb(2, "Bombilla salón 2", "192.168.8.91", "CC:86:EC:10:04:22", -59),
		bulb(3, "Bombilla lámpara pie", "192.168.8.92", "CC:86:EC:10:04:23", -61),
		bulb(4, "Bombilla entrada", "192.168.8.93", "CC:86:EC:10:04:24", -64),
		bulb(5, "Bombilla pasillo", "192.168.8.94", "CC:86:EC:10:04:25", -66),
		bulb(6, "Bombilla cocina", "192.168.8.95", "CC:86:EC:10:04:26", -60),
		withDetails(devExtra(Device{ID: "chromecast", Name: "Chromecast HD", Type: "tv", Manufacturer: "Google",
			IP: "192.168.8.36", MAC: "54:60:09:E3:5B:0A", RouterID: "living", Band: "5 GHz",
			SignalDbm: iptr(-54), TrafficMbps: 3.9, Online: true}, 21, 0),
			"chromecast-hd", "renueva en 6 h 12 min", "hace 200 días", "21 GB", "380 MB", true, "tv", ""),
		withDetails(devExtra(Device{ID: "homepod-mini", Name: "HomePod mini", Type: "altavoz", Manufacturer: "Apple",
			IP: "192.168.8.53", MAC: "F0:D1:A9:3E:77:5C", RouterID: "living", Band: "5 GHz",
			SignalDbm: iptr(-47), TrafficMbps: 0.3, Online: true}, 22, 0),
			"homepod-mini", "renueva en 10 h 41 min", "hace 190 días", "2,2 GB", "96 MB", true, "iot", ""),
		withDetails(devExtra(Device{ID: "galaxy-s23", Name: "Galaxy S23", Type: "movil", Manufacturer: "Samsung",
			IP: "192.168.8.42", MAC: "5C:0A:5B:88:1D:E4", RouterID: "living", Band: "5 GHz",
			SignalDbm: iptr(-51), TrafficMbps: 0.9, Online: true}, 23, 0),
			"galaxy-s23", "renueva en 8 h 55 min", "hace 175 días", "3,2 GB", "290 MB", true, "moviles", ""),
		withDetails(devExtra(Device{ID: "echo-dot", Name: "Echo Dot", Type: "altavoz", Manufacturer: "Amazon",
			IP: "192.168.8.54", MAC: "74:C2:46:19:F0:6B", RouterID: "living", Band: "2.4 GHz",
			SignalDbm: iptr(-56), TrafficMbps: 0.2, Online: true}, 24, 0),
			"echo-dot-cocina", "renueva en 11 h 18 min", "hace 210 días", "1,1 GB", "74 MB", true, "iot", ""),
		withDetails(devExtra(Device{ID: "nintendo-switch", Name: "Nintendo Switch", Type: "consola", Manufacturer: "Nintendo",
			IP: "192.168.8.33", MAC: "58:BD:A3:4C:E2:09", RouterID: "living", Band: "5 GHz",
			SignalDbm: iptr(-53), TrafficMbps: 0.1, Online: true}, 25, 0.3),
			"switch-oled", "renueva en 5 h 47 min", "hace 240 días", "8,6 GB", "310 MB", true, "tv", ""),
		withDetails(devExtra(Device{ID: "portatil-invitado", Name: "Portátil invitado", Type: "portatil", Manufacturer: "Desconocido",
			IP: "192.168.8.29", MAC: "A2:7E:9C:41:0B:6D", RouterID: "living", Band: "5 GHz",
			SignalDbm: iptr(-58), TrafficMbps: 0.7, Online: true, IsNew: true}, 26, 0),
			"unknown-7f2a", "renueva en 2 h 9 min", "hoy", "480 MB", "62 MB", false, "ordenadores", ""),
		withDetails(devExtra(Device{ID: "xbox-series-s", Name: "Xbox Series S", Type: "consola", Manufacturer: "Microsoft",
			IP: "192.168.8.32", MAC: "7C:1E:52:06:AA:3F", RouterID: "living", Band: "5 GHz",
			SignalDbm: iptr(-55), TrafficMbps: 0, Online: false}, 1, 0),
			"xbox-series-s", "Expirado", "hace 230 días", "0 MB", "0 MB", true, "tv", ""),
		withDetails(devExtra(Device{ID: "portatil-antiguo", Name: "Portátil antiguo", Type: "portatil", Manufacturer: "HP",
			IP: "192.168.8.28", MAC: "3C:52:82:5D:90:17", RouterID: "living", Band: "2.4 GHz",
			SignalDbm: iptr(-62), TrafficMbps: 0, Online: false}, 1, 0),
			"hp-pavilion-15", "Expirado", "hace 300 días", "0 MB", "0 MB", true, "ordenadores", ""),
		// —— Estudio ——
		withDetails(devExtra(Device{ID: "mac-mini", Name: "Mac mini", Type: "ordenador", Manufacturer: "Apple",
			IP: "192.168.8.22", MAC: "A4:83:E7:66:2C:98", RouterID: "estudio", Band: "cable",
			SignalDbm: nil, TrafficMbps: 1.9, Online: true}, 31, 0),
			"mac-mini", "IP fija (reserva)", "hace 280 días", "44 GB", "5,6 GB", true, "ordenadores", ""),
		withDetails(devExtra(Device{ID: "enchufe-ventilador", Name: "Enchufe ventilador", Type: "iot", Manufacturer: "TP-Link",
			IP: "192.168.8.82", MAC: "9C:53:22:B1:4E:70", RouterID: "estudio", Band: "2.4 GHz",
			SignalDbm: iptr(-59), TrafficMbps: 0.01, Online: true}, 32, 0.02),
			"tapo-p110-ventilador", "renueva en 12 h 0 min", "hace 120 días", "7 MB", "2 MB", true, "iot", ""),
		withDetails(devExtra(Device{ID: "ipad-pro", Name: "iPad Pro", Type: "tablet", Manufacturer: "Apple",
			IP: "192.168.8.51", MAC: "F0:18:98:91:5A:2B", RouterID: "estudio", Band: "5 GHz",
			SignalDbm: iptr(-49), TrafficMbps: 0.8, Online: true}, 33, 0),
			"ipad-pro", "renueva en 9 h 26 min", "hace 160 días", "5,1 GB", "390 MB", true, "moviles", ""),
		withDetails(devExtra(Device{ID: "hue-hub", Name: "Hub Philips Hue", Type: "iot", Manufacturer: "Signify",
			IP: "192.168.8.15", MAC: "00:17:88:2A:91:CE", RouterID: "estudio", Band: "cable",
			SignalDbm: nil, TrafficMbps: 0.02, Online: true}, 34, 0.04),
			"philips-hue-bridge", "IP fija (reserva)", "hace 280 días", "96 MB", "22 MB", false, "iot", ""),
		withDetails(devExtra(Device{ID: "sonos-one", Name: "Sonos One", Type: "altavoz", Manufacturer: "Sonos",
			IP: "192.168.8.55", MAC: "48:A6:B8:14:72:E0", RouterID: "estudio", Band: "2.4 GHz",
			SignalDbm: iptr(-57), TrafficMbps: 0.5, Online: true}, 35, 0),
			"sonos-one-estudio", "renueva en 10 h 4 min", "hace 195 días", "3,8 GB", "110 MB", true, "iot", ""),
		withDetails(devExtra(Device{ID: "iphone-trabajo", Name: "iPhone de trabajo", Type: "movil", Manufacturer: "Apple",
			IP: "192.168.8.50", MAC: "8C:85:90:47:C1:93", RouterID: "estudio", Band: "5 GHz",
			SignalDbm: iptr(-47), TrafficMbps: 0.3, Online: true, IsNew: true}, 36, 0),
			"iphone-15-pro-work", "renueva en 3 h 38 min", "hoy", "320 MB", "41 MB", true, "moviles", ""),
		withDetails(devExtra(Device{ID: "macbook-viejo", Name: "MacBook viejo", Type: "portatil", Manufacturer: "Apple",
			IP: "192.168.8.25", MAC: "3C:22:FB:0E:66:A1", RouterID: "estudio", Band: "2.4 GHz",
			SignalDbm: iptr(-60), TrafficMbps: 0, Online: false}, 1, 0),
			"macbook-pro-2015", "Expirado", "hace 320 días", "0 MB", "0 MB", true, "ordenadores", ""),
		// —— Patio ——
		withDetails(devExtra(Device{ID: "camara-jardin", Name: "Cámara jardín", Type: "camara", Manufacturer: "Reolink",
			IP: "192.168.8.73", MAC: "EC:71:DB:44:12:9B", RouterID: "patio", Band: "2.4 GHz",
			SignalDbm: iptr(-74), TrafficMbps: 1.4, Online: true}, 41, 0),
			"reolink-jardin", "renueva en 8 h 49 min", "hace 4 días", "14 GB", "820 MB", true, "iot", ""),
		withDetails(devExtra(Device{ID: "sensor-riego", Name: "Sensor de riego", Type: "iot", Manufacturer: "Tuya",
			IP: "192.168.8.89", MAC: "D8:1F:12:5B:08:44", RouterID: "patio", Band: "2.4 GHz",
			SignalDbm: iptr(-71), TrafficMbps: 0.01, Online: true}, 42, 0.02),
			"tuya-riego-01", "renueva en 12 h 0 min", "hace 5 días", "2 MB", "1 MB", false, "iot", ""),
		withDetails(devExtra(Device{ID: "enchufe-calefactor", Name: "Enchufe calefactor", Type: "iot", Manufacturer: "TP-Link",
			IP: "192.168.8.80", MAC: "50:C7:BF:31:7A:05", RouterID: "patio", Band: "2.4 GHz",
			SignalDbm: iptr(-75), TrafficMbps: 0.02, Online: true}, 43, 0.03),
			"tapo-p110-calefactor", "renueva en 12 h 0 min", "hace 96 días", "6 MB", "2 MB", true, "iot", ""),
		withDetails(devExtra(Device{ID: "camara-garaje", Name: "Cámara garaje", Type: "camara", Manufacturer: "Reolink",
			IP: "192.168.8.74", MAC: "EC:71:DB:44:13:02", RouterID: "patio", Band: "2.4 GHz",
			SignalDbm: iptr(-78), TrafficMbps: 0, Online: false}, 1, 0),
			"reolink-garaje", "Expirado", "hace 130 días", "0 MB", "0 MB", false, "iot", ""),
	}
	// newThisWeek (queda fuera de CANON_DETAILS en el JS)
	for i := range out {
		switch out[i].ID {
		case "portatil-invitado", "iphone-trabajo", "camara-jardin", "sensor-riego":
			out[i].NewThisWeek = true
		}
	}
	return out
}

// withDetails fusiona los literales de detalle (segundo objeto del spread JS).
func withDetails(d Device, hostname, lease, firstSeen, rx, tx string, adguard bool, group, icon string) Device {
	d.Hostname, d.DHCPLease, d.FirstSeen = hostname, lease, firstSeen
	d.Traffic24hRx, d.Traffic24hTx = rx, tx
	d.Adguard, d.Group, d.IconOverride = &adguard, group, icon
	return d
}

// canonAllDevices: los 67 clientes (canon expandido + adicionales + fixtures
// de topología v5: switch gestionado LLDP, hipervisor con CTs, switch
// inferido FDB — espejo del mock front app/src/data/mock.ts).
func canonAllDevices() []Device {
	out := canonDevices()
	out = append(out, additionalDevices()...)
	out = append(out, topologyDevices()...)
	return out
}

// topologyDevices: fixtures NUEVAS de topología v5 (mockup aprobado
// 2-Ago-2026). Los 3 clientes preexistentes (switch-netgear, pc-sobremesa,
// raspberry-pi) se enriquecen in situ en additionalDevices — no se duplican.
func topologyDevices() []Device {
	out := []Device{
		// 3 clientes detrás del switch gestionado (Salón)
		withDetails(Device{ID: "xbox-series-s", Name: "Xbox Series S", Type: "consola", Manufacturer: "Microsoft",
			IP: "192.168.8.35", MAC: "7C:ED:8D:4A:11:22", RouterID: "living", Band: "cable",
			SignalDbm: nil, TrafficMbps: 9.8, Online: true, AttachTo: "switch-netgear",
			Sparkline: []float64{3, 5, 7, 9, 11, 12, 10, 9, 10, 9, 10, 9.8}},
			"xbox-series-s", "renueva en 4 h 12 min", "hace 140 días", "31 GB", "1,9 GB", true, "tv", ""),
		withDetails(Device{ID: "apple-tv-4k", Name: "Apple TV 4K", Type: "tv", Manufacturer: "Apple",
			IP: "192.168.8.36", MAC: "F0:18:98:2B:33:44", RouterID: "living", Band: "cable",
			SignalDbm: nil, TrafficMbps: 15.2, Online: true, AttachTo: "switch-netgear",
			Sparkline: []float64{6, 8, 10, 12, 14, 16, 15, 14, 15, 15, 15, 15.2}},
			"apple-tv-4k", "renueva en 6 h 3 min", "hace 210 días", "88 GB", "3,4 GB", true, "tv", ""),
		withDetails(Device{ID: "receptor-denon", Name: "Receptor Denon", Type: "altavoz", Manufacturer: "Denon",
			IP: "192.168.8.37", MAC: "00:05:CD:55:66:77", RouterID: "living", Band: "cable",
			SignalDbm: nil, TrafficMbps: 0.6, Online: true, AttachTo: "switch-netgear",
			Sparkline: []float64{0.4, 0.5, 0.5, 0.6, 0.6, 0.7, 0.6, 0.6, 0.6, 0.6, 0.6, 0.6}},
			"denon-avr", "IP fija (reserva)", "hace 320 días", "640 MB", "42 MB", true, "iot", ""),
		// Hipervisor Proxmox (gateway lan2); sus 10 CTs se anidan (OUI BC:24:11)
		withDetails(Device{ID: "pve", Name: "Proxmox pve", Type: "servidor", Manufacturer: "Supermicro",
			IP: "192.168.8.5", MAC: "3C:52:82:10:20:30", RouterID: "flint2", Band: "cable",
			SignalDbm: nil, TrafficMbps: 12.3, Online: true, Port: "lan2",
			Sparkline: []float64{8, 9, 10, 11, 12, 13, 12, 12, 12, 12, 12, 12.3}},
			"pve", "IP fija (reserva)", "hace 400 días", "1,2 TB", "96 GB", true, "red", ""),
	}
	cts := []struct {
		id, name, typ, ip string
		mbps              float64
	}{
		{"ct-pihole", "Pi-hole", "servidor", "192.168.8.41", 6.1},
		{"ct-home-assistant", "Home Assistant", "iot", "192.168.8.42", 4.2},
		{"ct-nextcloud", "Nextcloud", "servidor", "192.168.8.43", 7.8},
		{"ct-jellyfin", "Jellyfin", "servidor", "192.168.8.44", 9.4},
		{"ct-immich", "Immich", "servidor", "192.168.8.45", 3.3},
		{"ct-gitea", "Gitea", "servidor", "192.168.8.46", 1.2},
		{"ct-uptime-kuma", "Uptime Kuma", "iot", "192.168.8.47", 0.4},
		{"ct-adguard-sync", "AdGuard sync", "servidor", "192.168.8.48", 0.8},
		{"ct-postgres", "Postgres", "servidor", "192.168.8.49", 2.1},
		{"ct-redis", "Redis", "servidor", "192.168.8.50", 0.9},
	}
	for i, c := range cts {
		mbps := c.mbps
		sp := make([]float64, 12)
		for j := range sp {
			sp[j] = max(0.1, mbps-2+float64((i+j)%5))
		}
		out = append(out, withDetails(Device{
			ID: c.id, Name: c.name, Type: c.typ, Manufacturer: "Proxmox VE (CT)",
			IP: c.ip, MAC: fmt.Sprintf("BC:24:11:00:2%d:%02X", i, 0x10+i), RouterID: "flint2", Band: "cable",
			SignalDbm: nil, TrafficMbps: mbps, Online: true, AttachTo: "pve", Sparkline: sp},
			c.id, "renueva en 3 h 10 min", "hace 90 días", "1,1 GB", "88 MB", true, "red", ""))
	}
	// Tras el switch/bridge inferido (gateway lan3, OUI heterogéneo, sin IP).
	// pc-sobremesa y raspberry-pi ya existen (enriquecidos con AttachTo en
	// additionalDevices): aquí solo los 6 nuevos.
	behind := []struct {
		id, name, typ, man, mac, ip string
		mbps                        float64
	}{
		{"tv-salon-cable", "TV Salón (cable)", "tv", "Samsung", "8C:EA:48:AA:02:02", "192.168.8.61", 24.4},
		{"impresora-hp", "Impresora HP", "iot", "HP", "3C:D9:2B:AA:03:03", "192.168.8.62", 0.1},
		{"xbox-one", "Xbox One", "consola", "Microsoft", "7C:ED:8D:AA:05:05", "192.168.8.64", 4.2},
		{"receptor-av", "Receptor AV", "altavoz", "Denon", "00:05:CD:AA:06:06", "192.168.8.65", 0.3},
		{"deco-orange", "Deco Orange", "tv", "Sagemcom", "48:83:B4:AA:07:07", "192.168.8.66", 1.1},
		{"pc-invitado", "PC invitado", "ordenador", "—", "A2:F4:11:AA:08:08", "192.168.8.67", 0.8},
	}
	for i, b := range behind {
		mbps := b.mbps
		sp := make([]float64, 12)
		for j := range sp {
			sp[j] = max(0.05, mbps-1.5+float64((i+j)%4)*0.5)
		}
		grp := "otros"
		switch b.typ {
		case "ordenador":
			grp = "ordenadores"
		case "tv":
			grp = "tv"
		case "iot":
			grp = "iot"
		case "altavoz":
			grp = "iot"
		}
		out = append(out, withDetails(Device{
			ID: b.id, Name: b.name, Type: b.typ, Manufacturer: b.man,
			IP: b.ip, MAC: b.mac, RouterID: "flint2", Band: "cable",
			SignalDbm: nil, TrafficMbps: mbps, Online: true, AttachTo: "dist-flint2-lan3", Sparkline: sp},
			b.id, "renueva en 5 h 22 min", "hace 60 días", "2,4 GB", "120 MB", true, grp, ""))
	}
	return out
}

// canonDistributionNodes: distnodes demo (en live los infiere el colector FDB).
func canonDistributionNodes() []DistributionNode {
	return []DistributionNode{
		{ID: "dist-flint2-lan3", Kind: "inferred", RouterID: "flint2", Port: "lan3", MacCount: 8},
		{ID: "dist-pve", Kind: "hypervisor", RouterID: "flint2", Port: "lan2", MacCount: 11, HostDeviceID: "pve", Name: "Proxmox pve"},
	}
}

// boolp: los literales demo fijan adguard explícito (true/false) como en el
// dataset JS; live lo deja nil para omitir el campo.
func boolp(b bool) *bool { return &b }
