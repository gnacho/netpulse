package probe

import "testing"

// A two-uplink router whose policy sends everything through the mobile link
// while the wired one still holds the cheapest default route — the state
// that made the panel show an address nobody was using. The mobile parent
// carries its address here the way a policy manager reports it.
const dumpPolicyDisagreesWithRoutes = `{"interface":[
 {"interface":"lan","up":true,"proto":"static","l3_device":"br-home.10","device":"br-home.10",
  "ipv4-address":[{"address":"192.0.2.1","mask":24}],"route":[]},
 {"interface":"cell","up":true,"proto":"qmi","l3_device":"wwan0",
  "ipv4-address":[{"address":"192.0.2.77","mask":32}],
  "route":[{"target":"0.0.0.0","mask":0,"nexthop":"192.0.2.78"}],
  "dns-server":["192.0.2.53"]},
 {"interface":"fiber","up":true,"proto":"pppoe","l3_device":"pppoe-fiber","device":"lan4",
  "ipv4-address":[{"address":"203.0.113.45","mask":32,"ptpaddress":"198.51.100.1"}],
  "route":[{"target":"0.0.0.0","mask":0,"nexthop":"198.51.100.1"}],
  "dns-server":["198.51.100.53"]}]}`

func TestParseWanStatusFollowsThePolicyOverTheRoutes(t *testing.T) {
	// Without being told, the first routed interface wins — the old answer.
	if info := ParseWanStatus([]byte(dumpPolicyDisagreesWithRoutes)); info.IP != "192.0.2.77" {
		t.Fatalf("unprompted pick = %+v", info)
	}
	// Told which uplink is carrying traffic, everything follows it: the
	// address, the gateway, the resolvers and the socket.
	info := ParseWanStatusPreferring([]byte(dumpPolicyDisagreesWithRoutes), "fiber")
	if info.IP != "203.0.113.45" || info.Gateway != "198.51.100.1" {
		t.Fatalf("ip/gateway = %+v, want the policy's uplink", info)
	}
	if info.Proto != "pppoe" || info.Port != "lan4" || info.Device != "pppoe-fiber" {
		t.Fatalf("identity = %+v", info)
	}
	if len(info.DNS) != 1 || info.DNS[0] != "198.51.100.53" {
		t.Fatalf("dns = %v, want that uplink's own", info.DNS)
	}
}

// The shape that matters on real hardware: the policy names the modem
// interface, which holds neither address nor route — the runtime child
// netifd spawned underneath it holds both, and cannot be named in a policy.
const dumpModemParentAndChild = `{"interface":[
 {"interface":"fiber","up":true,"proto":"pppoe","l3_device":"pppoe-fiber","device":"lan4",
  "ipv4-address":[{"address":"203.0.113.45","mask":32}],
  "route":[{"target":"0.0.0.0","mask":0,"nexthop":"198.51.100.1"}],
  "dns-server":["198.51.100.53"]},
 {"interface":"cell","up":true,"proto":"qmi","l3_device":"wwan0",
  "ipv4-address":[],"route":[],"dns-server":[]},
 {"interface":"cell_4","up":true,"proto":"dhcp","l3_device":"wwan0","device":"wwan0","dynamic":true,
  "ipv4-address":[{"address":"192.0.2.77","mask":32}],
  "route":[{"target":"0.0.0.0","mask":0,"nexthop":"192.0.2.78"}],
  "dns-server":["192.0.2.53"]}]}`

func TestPreferredModemUplinkTakesItsChildsAddress(t *testing.T) {
	info := ParseWanStatusPreferring([]byte(dumpModemParentAndChild), "cell")
	if info.IP != "192.0.2.77" || info.Gateway != "192.0.2.78" {
		t.Fatalf("ip/gateway = %+v, want the child's", info)
	}
	// The identity stays the parent's: that is the name the policy uses and
	// the only one with a configuration behind it.
	if info.Proto != "qmi" || info.Device != "wwan0" {
		t.Fatalf("identity = %+v, want the parent's", info)
	}
	if len(info.DNS) != 1 || info.DNS[0] != "192.0.2.53" {
		t.Fatalf("dns = %v", info.DNS)
	}
}

func TestParseWanStatusIgnoresAnUnusablePreference(t *testing.T) {
	// A name nothing matches falls back to the route table rather than
	// reporting a connection with nothing on it.
	if info := ParseWanStatusPreferring([]byte(dumpUplinkNotNamedWan), "gone"); info.IP != "203.0.113.45" {
		t.Fatalf("unknown preference gave %+v, want the routed uplink", info)
	}
	// And an empty preference is exactly the old behaviour.
	empty, old := ParseWanStatusPreferring([]byte(dumpUplinkNotNamedWan), ""), ParseWanStatus([]byte(dumpUplinkNotNamedWan))
	if empty.IP != old.IP || empty.Proto != old.Proto || empty.Port != old.Port || empty.Gateway != old.Gateway {
		t.Fatalf("an empty preference changed the answer: %+v vs %+v", empty, old)
	}
}

func TestActiveUplinkName(t *testing.T) {
	if got := ActiveUplinkName(nil); got != "" {
		t.Fatalf("no policy should name nothing, got %q", got)
	}
	if got := ActiveUplinkName(&MultiWanInfo{Mode: "failover", Active: "cell"}); got != "cell" {
		t.Fatalf("got %q", got)
	}
}
