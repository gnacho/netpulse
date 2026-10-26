// iwdev.go - `iw dev` parsing with the physical radio of every interface
// (#1560). The neighbour scan is the only probe that takes a radio off-channel,
// so it must run once per radio, not once per interface: a radio carrying
// several BSSIDs (LAN plus a VLAN SSID) used to be scanned once per BSSID in
// the same cycle, 2-4s each.
package probe

import (
	"strconv"
	"strings"
)

// IwDevIface is one interface from `iw dev` with the radio it belongs to.
type IwDevIface struct {
	Iface string
	Phy   int    // -1 when the output had no phy# section
	Type  string // "AP", "managed", "monitor", "P2P-device", "" if unknown
}

// ParseIwDevDetailed parses `iw dev` keeping the phy# section each interface
// sits under and its mode. iw prints one phy#N header followed by every
// interface of that radio, so the section in effect is the radio.
func ParseIwDevDetailed(out string) []IwDevIface {
	phy := -1
	var res []IwDevIface
	for _, raw := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(raw))
		if len(fields) == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(fields[0], "phy#"):
			if n, err := strconv.Atoi(strings.TrimPrefix(fields[0], "phy#")); err == nil {
				phy = n
			} else {
				phy = -1
			}
		case fields[0] == "Interface" && len(fields) >= 2:
			res = append(res, IwDevIface{Iface: fields[1], Phy: phy})
		case fields[0] == "type" && len(fields) >= 2 && len(res) > 0:
			// First `type` wins: some iw builds repeat the line once per
			// virtual interface inside the same block.
			if res[len(res)-1].Type == "" {
				res[len(res)-1].Type = fields[1]
			}
		}
	}
	return res
}

// IwDevScanTargets picks the interface to scan for neighbours on every
// physical radio, preferring one in AP mode. What a scan returns depends on
// the radio, not on the BSSID it is run from, so scanning the second BSSID of
// the same radio only keeps that radio off-channel for another 2-4s (#1557).
// Without phy information (old iw, trimmed output) it falls back to one target
// per interface, which is what the agent did before #1560.
func IwDevScanTargets(ifaces []IwDevIface) []IwDevIface {
	hasPhy := false
	for _, i := range ifaces {
		if i.Phy >= 0 {
			hasPhy = true
			break
		}
	}
	if !hasPhy {
		return ifaces
	}
	var order []int
	groups := make(map[int][]IwDevIface, len(ifaces))
	var loose []IwDevIface
	for _, i := range ifaces {
		if i.Phy < 0 {
			loose = append(loose, i)
			continue
		}
		if _, ok := groups[i.Phy]; !ok {
			order = append(order, i.Phy)
		}
		groups[i.Phy] = append(groups[i.Phy], i)
	}
	res := make([]IwDevIface, 0, len(order)+len(loose))
	for _, phy := range order {
		res = append(res, pickScanIface(groups[phy]))
	}
	return append(res, loose...)
}

// pickScanIface prefers an AP interface because it is the one carrying
// clients, which keeps the interface name in wifi_scans meaningful. Monitor
// and P2P-device interfaces can also leave the radio off-channel without the
// AP, so they are the last resort.
func pickScanIface(group []IwDevIface) IwDevIface {
	for _, i := range group {
		if strings.EqualFold(i.Type, "AP") {
			return i
		}
	}
	for _, i := range group {
		if !strings.EqualFold(i.Type, "monitor") && !strings.EqualFold(i.Type, "P2P-device") {
			return i
		}
	}
	return group[0]
}
