package probe

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// vlanSubifRe matches an 802.1Q subinterface: the parent device and the VLAN
// id in its name, the way Linux and OpenWrt build them (eth0.666).
var vlanSubifRe = regexp.MustCompile(`^([^.]+)\.(\d{1,4})$`)

// ParseUciNetworkVlans derives the VLANs of a router that segments with
// 802.1Q subinterfaces instead of bridge VLAN filtering (#1757).
//
// `bridge vlan show` only knows about bridges with vlan_filtering enabled. A
// box that creates one bridge per network and feeds it a tagged subinterface
// (eth0.999 into br-iot) reports every bridge on its default VLAN 1, so the
// kernel view is blind to the real segmentation and the panel shows a flat
// list of ports. The configuration is the only place that says which VLAN a
// bridge carries, which interface it belongs to and what subnet it serves, so
// it is read as a source of its own and merged with the kernel output, never
// as a fallback.
func ParseUciNetworkVlans(out string) []VlanPort {
	type section map[string]string
	sections := map[string]section{}
	order := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "network.") {
			continue
		}
		key, val, ok := strings.Cut(strings.TrimPrefix(line, "network."), "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, "'")
		name, attr, isAttr := strings.Cut(key, ".")
		if _, seen := sections[name]; !seen {
			sections[name] = section{}
			order = append(order, name)
		}
		if !isAttr {
			sections[name]["@type"] = val
			continue
		}
		sections[name][attr] = val
	}

	// Interfaces first: they carry the name and the subnet of whatever device
	// they sit on.
	type netinfo struct {
		label  string
		subnet string
	}
	byDevice := map[string]netinfo{}
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
		if _, seen := byDevice[dev]; seen {
			continue
		}
		byDevice[dev] = netinfo{label: name, subnet: vlanSubnet(s["ipaddr"], s["netmask"])}
	}

	byPort := map[string][]VlanEntry{}
	portOrder := []string{}
	add := func(port string, e VlanEntry) {
		if _, seen := byPort[port]; !seen {
			portOrder = append(portOrder, port)
		}
		byPort[port] = append(byPort[port], e)
	}

	for _, name := range order {
		s := sections[name]
		// The section type is what matters, not the header: a named device
		// section prints as `network.x=device`, an anonymous one as
		// `network.@device[0]=device`, and both declare type 'bridge'.
		if s["type"] != "bridge" {
			continue
		}
		bridge := s["name"]
		if bridge == "" {
			continue
		}
		info := byDevice[bridge]
		native := true
		for _, port := range splitUciList(s["ports"]) {
			m := vlanSubifRe.FindStringSubmatch(port)
			if m == nil {
				continue
			}
			vid, err := strconv.Atoi(m[2])
			if err != nil || vid < 1 || vid > 4094 {
				continue
			}
			native = false
			// The bridge carries the VLAN the subinterface tags on the parent
			// port: the members of the VLAN are the ports of the bridge.
			add(bridge, VlanEntry{ID: vid, PVID: true, Name: info.label, Subnet: info.subnet})
			// The subinterface itself is the bridge port the forwarding
			// database reports for every device behind it, so it needs its own
			// entry: without it the topology has no way to colour a device that
			// hangs from eth0.999 (#1757).
			add(port, VlanEntry{ID: vid, PVID: true, Name: info.label, Subnet: info.subnet})
			add(m[1], VlanEntry{ID: vid, Tagged: true})
		}
		// No subinterface among the members: the bridge is a plain untagged
		// network, which still has a name and a subnet worth showing.
		if native {
			add(bridge, VlanEntry{ID: 1, PVID: true, Name: info.label, Subnet: info.subnet})
		}
	}

	// An interface bound straight to a subinterface, with no bridge in
	// between (eth0.50 for a PPPoE or a routed VLAN).
	for _, name := range order {
		s := sections[name]
		if s["@type"] != "interface" {
			continue
		}
		dev := s["device"]
		m := vlanSubifRe.FindStringSubmatch(dev)
		if m == nil {
			continue
		}
		vid, err := strconv.Atoi(m[2])
		if err != nil || vid < 1 || vid > 4094 {
			continue
		}
		add(dev, VlanEntry{ID: vid, PVID: true, Name: name, Subnet: vlanSubnet(s["ipaddr"], s["netmask"])})
	}

	res := make([]VlanPort, 0, len(portOrder))
	for _, port := range portOrder {
		res = append(res, VlanPort{Port: port, Vlans: byPort[port]})
	}
	return res
}

// MergeVlanPorts unions two views of the same VLAN tables, keeping the order
// of the kernel one. Where both describe the same port and VLAN, the kernel
// owns the flags (it is what the box is doing) and the declared view fills in
// what the kernel cannot know: the name of the network and its subnet.
func MergeVlanPorts(kernel, declared []VlanPort) []VlanPort {
	if len(declared) == 0 {
		return kernel
	}
	if len(kernel) == 0 {
		return declared
	}
	idx := make(map[string]int, len(kernel)+len(declared))
	out := make([]VlanPort, 0, len(kernel)+len(declared))
	for _, p := range kernel {
		idx[p.Port] = len(out)
		out = append(out, VlanPort{Port: p.Port, Vlans: append([]VlanEntry(nil), p.Vlans...)})
	}
	for _, p := range declared {
		if i, ok := idx[p.Port]; ok {
			out[i].Vlans = mergeVlanEntries(out[i].Vlans, p.Vlans)
			continue
		}
		idx[p.Port] = len(out)
		out = append(out, VlanPort{Port: p.Port, Vlans: append([]VlanEntry(nil), p.Vlans...)})
	}
	return out
}

func mergeVlanEntries(kernel, declared []VlanEntry) []VlanEntry {
	// A bridge without vlan_filtering answers every port with VLAN 1. That is
	// not a wrong answer, it is answering a different question, and keeping
	// it beside the real VLANs of the port would leave them invisible.
	if vlanOnlyDefault(kernel) && vlanHasReal(declared) {
		return append([]VlanEntry(nil), declared...)
	}
	res := append([]VlanEntry(nil), kernel...)
	byID := make(map[int]int, len(res))
	for i, e := range res {
		byID[e.ID] = i
	}
	for _, d := range declared {
		i, ok := byID[d.ID]
		if !ok {
			byID[d.ID] = len(res)
			res = append(res, d)
			continue
		}
		if res[i].Name == "" {
			res[i].Name = d.Name
		}
		if res[i].Subnet == "" {
			res[i].Subnet = d.Subnet
		}
	}
	return res
}

// splitUciList splits a uci list value as `uci show` prints it: every item
// quoted on the same line.
func splitUciList(val string) []string {
	var out []string
	for _, part := range strings.Split(val, "' '") {
		if p := strings.TrimSpace(strings.Trim(part, "'")); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// vlanSubnet renders an address and a netmask as CIDR. Any missing or
// malformed piece is dropped rather than guessed.
func vlanSubnet(ip, mask string) string {
	if ip == "" {
		return ""
	}
	m := net.ParseIP(mask).To4()
	if m == nil {
		return ip
	}
	ones, bits := net.IPMask(m).Size()
	if bits != 32 {
		return ip
	}
	return fmt.Sprintf("%s/%d", ip, ones)
}



// vlanOnlyDefault: the kernel reported nothing but the default VLAN.
func vlanOnlyDefault(entries []VlanEntry) bool {
	if len(entries) == 0 {
		return false
	}
	for _, e := range entries {
		if e.ID != 1 {
			return false
		}
	}
	return true
}

// vlanHasReal: the view carries at least one VLAN other than the default.
func vlanHasReal(entries []VlanEntry) bool {
	for _, e := range entries {
		if e.ID != 1 {
			return true
		}
	}
	return false
}
