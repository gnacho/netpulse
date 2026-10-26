// switchporttraffic.go — per-client traffic for wired devices learned only in
// a managed SNMP switch's FDB (issue #1543).
//
// A managed switch has no per-MAC accounting (nlbwmon is an OpenWrt/agent
// feature), and switch-local flows (e.g. a camera streaming to an NVR on the
// same switch) never cross the gateway, so the gateway's per-MAC rate reports
// only the tiny gateway-bound slice and the client looks idle. The switch's own
// port byte counters (ifTable octets, surfaced as EthPort.RxBps/TxBps) DO see
// the whole flow, so we attribute a port's rx+tx rate to the clients the
// switch's FDB learns on it, split evenly so a trunk carrying several MACs is
// not duplicated onto each. Only SNMP-enabled routers are consulted and a
// client's rate is only ever raised, so agent-reported clients and any higher
// existing figure are left untouched.
package adapters

import "strings"

// applySnmpSwitchPortTraffic raises each online client attributed to an
// SNMP-enabled router to its FDB port's rate (Mbps), split across the online
// clients sharing that port. It runs at the end of buildDevices, before
// inferTopology fills Device.Port, so the client's port is resolved from the
// router's own FDB (mac -> port, uppercased) rather than Device.Port.
func (l *Live) applySnmpSwitchPortTraffic(polled map[string]*routerPolled, devices []Device) {
	type portKey struct{ router, port string }

	// Mbps per (router, port name). Indexed by every alias a port can carry so
	// the FDB's name (its DisplayName = Label/Iface) resolves.
	rate := map[portKey]float64{}
	for rID, p := range polled {
		if p == nil || !p.cfg.SnmpEnabled {
			continue
		}
		for _, ep := range p.ports {
			bps := ep.RxBps + ep.TxBps
			if bps <= 0 {
				continue
			}
			mp := bps / 1e6
			for _, name := range []string{ep.ID, ep.Label, ep.Iface} {
				if name != "" {
					rate[portKey{rID, name}] = mp
				}
			}
		}
	}
	if len(rate) == 0 {
		return
	}

	// A client's port comes from the switch's own FDB (mac -> port), the same
	// uppercased-MAC key the topology step uses.
	resolve := func(d *Device) (portKey, bool) {
		if !d.Online || d.RouterID == "" {
			return portKey{}, false
		}
		p := polled[d.RouterID]
		if p == nil || !p.cfg.SnmpEnabled || len(p.fdb) == 0 {
			return portKey{}, false
		}
		port, ok := p.fdb[strings.ToUpper(d.MAC)]
		if !ok {
			return portKey{}, false
		}
		k := portKey{d.RouterID, port}
		return k, rate[k] > 0
	}

	keys := make([]portKey, len(devices))
	matched := make([]bool, len(devices))
	share := map[portKey]int{}
	for i := range devices {
		if k, ok := resolve(&devices[i]); ok {
			keys[i], matched[i] = k, true
			share[k]++
		}
	}
	for i := range devices {
		if !matched[i] || share[keys[i]] <= 0 {
			continue
		}
		if mp := rate[keys[i]] / float64(share[keys[i]]); mp > devices[i].TrafficMbps {
			devices[i].TrafficMbps = mp
		}
	}
}
