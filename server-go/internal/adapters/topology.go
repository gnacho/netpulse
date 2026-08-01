package adapters

import (
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Inferencia de topología v5 (FDB): puertos físicos, switches/bridges e
// hipervisores. Función pura (sin SSH ni BD) para ser testeable.
//
// Reglas (plan 2-Ago-2026):
//   - Un puerto físico (lanN) con UNA MAC aprendida = cableado directo →
//     Device.Port.
//   - Un puerto con VARIAS MACs = algo multiplexándolo (switch, hipervisor,
//     PLC…). El FDB no dice qué: OUI heterogéneo → "inferred" (círculo
//     dashed, sin afirmar); alguna MAC con OUI de hipervisor y exactamente
//     un host → "hypervisor" (sus VMs se anidan bajo el host).
//   - Se excluyen las MACs de los propios routers (su bridge br-lan) y las
//     de clientes atribuidos a OTROS routers (los clientes wifi de un AP
//     aparecen en el FDB del gateway por el puerto del uplink).
// ---------------------------------------------------------------------------

// hypervisorOUI: prefijos de MAC de hipervisores conocidos.
var hypervisorOUI = []string{
	"BC:24:11", // Proxmox VE
	"00:50:56", // VMware (ESXi/Workstation)
	"00:0C:29", // VMware
	"00:15:5D", // Hyper-V
	"52:54:00", // KVM/QEMU
}

func isHypervisorMAC(mac string) bool {
	for _, p := range hypervisorOUI {
		if strings.HasPrefix(mac, p) {
			return true
		}
	}
	return false
}

// inferTopology enriquece devices con Port/AttachTo e infiere los
// DistributionNode del FDB sondeado. `polled` lleva fdb (MAC→puerto, ya
// filtrado a lanN/wan por parseBridgeFdb) y brMac por router.
func inferTopology(polled map[string]*routerPolled, devices []Device) ([]Device, []DistributionNode) {
	byMAC := make(map[string]int, len(devices))
	for i, d := range devices {
		byMAC[d.MAC] = i
	}
	routerMACs := map[string]bool{}
	for _, p := range polled {
		if p.brMac != "" {
			routerMACs[p.brMac] = true
		}
	}

	var dists []DistributionNode
	// routers en orden determinista (el mapa polled no lo es)
	routerIDs := make([]string, 0, len(polled))
	for id := range polled {
		routerIDs = append(routerIDs, id)
	}
	sort.Strings(routerIDs)

	for _, routerID := range routerIDs {
		p := polled[routerID]
		// invertir fdb: puerto → MACs (el WAN no es distribución LAN)
		byPort := map[string][]string{}
		for mac, port := range p.fdb {
			if port == "wan" {
				continue
			}
			byPort[port] = append(byPort[port], mac)
		}
		ports := make([]string, 0, len(byPort))
		for port := range byPort {
			ports = append(ports, port)
		}
		sort.Strings(ports)

		for _, port := range ports {
			var kept []string
			for _, mac := range byPort[port] {
				if routerMACs[mac] {
					continue // el propio router/AP no es un cliente
				}
				if idx, ok := byMAC[mac]; ok {
					d := devices[idx]
					if d.RouterID != routerID {
						continue // cliente de otro router (visto por el uplink)
					}
					if d.Band != "cable" && d.Band != "—" {
						continue // wifi no cuelga de un puerto físico
					}
				}
				kept = append(kept, mac)
			}
			if len(kept) == 0 {
				continue
			}
			setPort := func() {
				for _, mac := range kept {
					if idx, ok := byMAC[mac]; ok && devices[idx].RouterID == routerID {
						devices[idx].Port = port
					}
				}
			}
			if len(kept) == 1 {
				// cableado directo: puerto conocido, sin nodo de distribución
				setPort()
				continue
			}

			// puerto multi-MAC → nodo de distribución
			setPort()
			id := fmt.Sprintf("dist-%s-%s", routerID, port)
			var vmMACs, hostMACs []string
			for _, mac := range kept {
				if isHypervisorMAC(mac) {
					vmMACs = append(vmMACs, mac)
				} else {
					hostMACs = append(hostMACs, mac)
				}
			}
			if len(vmMACs) > 0 && len(hostMACs) == 1 {
				if hidx, ok := byMAC[hostMACs[0]]; ok && devices[hidx].RouterID == routerID {
					// hipervisor con host identificado: VMs anidadas bajo él
					dists = append(dists, DistributionNode{
						ID: id, Kind: "hypervisor", RouterID: routerID, Port: port,
						MacCount: len(kept), HostDeviceID: devices[hidx].ID, Name: devices[hidx].Name,
					})
					for _, mac := range vmMACs {
						if idx, ok := byMAC[mac]; ok && devices[idx].RouterID == routerID {
							devices[idx].AttachTo = devices[hidx].ID
						}
					}
					continue
				}
			}
			// OUI heterogéneo (o hipervisor ambiguo sin host claro): inferido
			dists = append(dists, DistributionNode{
				ID: id, Kind: "inferred", RouterID: routerID, Port: port, MacCount: len(kept),
			})
			for _, mac := range kept {
				if idx, ok := byMAC[mac]; ok && devices[idx].RouterID == routerID {
					devices[idx].AttachTo = id
				}
			}
		}
	}
	return devices, dists
}
