// demo_canon.go — Serialización del canon demo a JSON (SPEC-65 D65-1).
//
// La fuente de verdad única del canon es Go (demo_dataset.go/demo_extras.go);
// app/src/data/demo-canon.json es un ARTEFACTO GENERADO (cmd/gen-demo-canon)
// que la app importa en build. El test de frescura
// (demo_canon_json_test.go) exige que el JSON commiteado coincida con lo que
// produce BuildDemoCanon — si diverge: go run ./cmd/gen-demo-canon.
//
// NO se incluyen: traffic (series con random walk), alerts (tienen Seed del
// engine) ni topDevices (la app los deriva ordenando por trafficMbps).
package adapters

// DemoCanonJSON es el contenido EXACTO de app/src/data/demo-canon.json
// (claves camelCase, mismas formas que el contrato API de types.go).
type DemoCanonJSON struct {
	Routers           []Router           `json:"routers"`
	Devices           []Device           `json:"devices"`
	DeviceTotals      DeviceTotals       `json:"deviceTotals"`
	DistributionNodes []DistributionNode `json:"distributionNodes"`
	Adguard           AdGuardStats       `json:"adguard"`
	Wireguard         WireGuardStats     `json:"wireguard"`
	WAN               WAN                `json:"wan"`
	Health            HealthScore        `json:"health"`
}

// BuildDemoCanon construye el canon completo con los agregados DERIVADOS del
// dataset (SPEC-CANON D5): deviceTotals con deviceTotalsOf (NO literal),
// router.clients con onlineClientsOf y adguard.clientsTotal = IDs únicos.
func BuildDemoCanon() DemoCanonJSON {
	devices := canonAllDevices()
	routers := canonRouters()
	for i := range routers {
		routers[i].Clients = onlineClientsOf(devices, routers[i].ID)
	}
	adguard := canonAdguard()
	adguard.ClientsTotal = len(devices)
	return DemoCanonJSON{
		Routers:           routers,
		Devices:           devices,
		DeviceTotals:      deviceTotalsOf(devices),
		DistributionNodes: canonDistributionNodes(),
		Adguard:           adguard,
		Wireguard:         canonWireguard(),
		WAN:               canonWAN(),
		Health:            canonHealthScore(),
	}
}
