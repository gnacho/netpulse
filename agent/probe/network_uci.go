package probe

import (
	"strconv"
	"strings"
)

// UciNetwork is one logical network as `uci show network` declares it (#1889):
// the way the box is configured, not the way its jacks look. A segmented
// OpenWrt router has one of these per network, with its own device, subnet and
// member ports, and that is the view an operator recognises.
type UciNetwork struct {
	Name   string   `json:"name"`
	Device string   `json:"device,omitempty"`
	Proto  string   `json:"proto,omitempty"`
	Subnet string   `json:"subnet,omitempty"`
	Vlan   int      `json:"vlan,omitempty"`
	Ports  []string `json:"ports,omitempty"`
}

// ParseUciNetworks reads the interface sections of `uci show network` and
// resolves each one against the device sections, so a row carries the members
// of its bridge and the VLAN it lives on. Both models are read the same way:
// a bridge with VLAN filtering declares br_switch.999 as the device, and a box
// that segments with subinterfaces declares br-iot fed by eth0.999.
func ParseUciNetworks(out string) []UciNetwork {
	sections, order := uciNetworkSections(out)
	var nets []UciNetwork
	for _, name := range order {
		s := sections[name]
		if s["@type"] != "interface" {
			continue
		}
		dev := s["device"]
		if dev == "" {
			dev = s["ifname"]
		}
		if dev == "" {
			continue
		}
		n := UciNetwork{
			Name:   name,
			Device: dev,
			Proto:  s["proto"],
			Subnet: vlanSubnet(s["ipaddr"], s["netmask"]),
		}
		// An address written as a prefix already says its subnet.
		if ip := s["ipaddr"]; n.Subnet == "" && strings.Contains(ip, "/") {
			n.Subnet = ip
		}
		n.Vlan = subifVlan(dev)
		// The device section may name the device itself (br-iot) or its parent
		// bridge when the network sits on a subinterface (br_switch.999).
		base := dev
		if i := strings.IndexByte(dev, '.'); i > 0 {
			base = dev[:i]
		}
		var devSec uciSection
		for _, dname := range order {
			d := sections[dname]
			if d["@type"] != "device" {
				continue
			}
			if d["name"] == dev {
				devSec = d
				break
			}
			if d["name"] == base && devSec == nil {
				devSec = d
			}
		}
		if devSec != nil {
			n.Ports = splitUciList(devSec["ports"])
			// The bridge itself has no VLAN in its name: the tagged
			// subinterface it is fed with says which one it carries.
			if n.Vlan == 0 {
				for _, port := range n.Ports {
					if vid := subifVlan(port); vid > 0 {
						n.Vlan = vid
						break
					}
				}
			}
		}
		// With bridge VLAN filtering the members of one network are the ports
		// its bridge-vlan section lists, not every port of the bridge.
		if n.Vlan > 0 && base != dev {
			for _, bname := range order {
				b := sections[bname]
				if b["@type"] != "bridge-vlan" || b["device"] != base || b["vlan"] != strconv.Itoa(n.Vlan) {
					continue
				}
				ports := splitUciList(b["ports"])
				for i, port := range ports {
					// eth1:t means tagged, eth1:u* untagged and primary.
					if j := strings.IndexByte(port, ':'); j > 0 {
						ports[i] = port[:j]
					}
				}
				n.Ports = ports
			}
		}
		nets = append(nets, n)
	}
	return nets
}

// subifVlan returns the VLAN id of an 802.1Q subinterface name, 0 otherwise.
func subifVlan(name string) int {
	m := vlanSubifRe.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	vid, err := strconv.Atoi(m[2])
	if err != nil {
		return 0
	}
	return vid
}
