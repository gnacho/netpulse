package adapters

import (
	"math"
	"testing"
)

// applySnmpSwitchPortTraffic raises a switch client's traffic to its FDB port's
// byte-counter rate (switch-local flows the gateway's per-MAC nlbwmon misses),
// splits a port across its clients, and never lowers an existing higher figure.
func TestApplySnmpSwitchPortTraffic(t *testing.T) {
	polled := map[string]*routerPolled{
		"sw": {
			cfg: RouterConfig{ID: "sw", SnmpEnabled: true},
			ports: []EthPort{
				{ID: "snmp-3", Label: "1/g3", RxBps: 14e6, TxBps: 0.14e6},
				{ID: "snmp-4", Label: "1/g4", RxBps: 5e6, TxBps: 0},
			},
			fdb: map[string]string{
				"AA:01": "1/g3",
				"AA:02": "1/g3",
				"AA:03": "1/g4",
				"AA:04": "1/g3",
			},
		},
	}
	devices := []Device{
		// Two cameras sharing port 1/g3 (14.14 Mbps total → 7.07 each). Lowercase
		// MACs also prove the FDB uppercased-key lookup.
		{MAC: "aa:01", Online: true, RouterID: "sw", TrafficMbps: 0.0008},
		{MAC: "aa:02", Online: true, RouterID: "sw", TrafficMbps: 0},
		// One camera on 1/g4 (5 Mbps); its existing 20 must not be lowered.
		{MAC: "aa:03", Online: true, RouterID: "sw", TrafficMbps: 20},
		// Offline client is ignored (not counted toward the port split).
		{MAC: "aa:04", Online: false, RouterID: "sw", TrafficMbps: 0},
	}

	l := &Live{}
	l.applySnmpSwitchPortTraffic(polled, devices)

	if d := devices[0]; math.Abs(d.TrafficMbps-7.07) > 0.01 {
		t.Fatalf("AA:01 = %.4f, want ~7.07 (port 14.14 split over 2 online)", d.TrafficMbps)
	}
	if d := devices[1]; math.Abs(d.TrafficMbps-7.07) > 0.01 {
		t.Fatalf("AA:02 = %.4f, want ~7.07", d.TrafficMbps)
	}
	if d := devices[2]; d.TrafficMbps != 20 {
		t.Fatalf("AA:03 = %.4f, want 20 (never lowered from a higher existing figure)", d.TrafficMbps)
	}
	if d := devices[3]; d.TrafficMbps != 0 {
		t.Fatalf("offline AA:04 = %.4f, want 0", d.TrafficMbps)
	}
}

// Non-SNMP routers are untouched: a client on an OpenWrt/agent router keeps
// whatever per-MAC rate it had (no switch port to attribute).
func TestApplySnmpSwitchPortTrafficSkipsAgentRouters(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {
			cfg:   RouterConfig{ID: "gw"}, // SnmpEnabled false
			ports: []EthPort{{ID: "lan1", Label: "LAN 1", RxBps: 100e6}},
			fdb:   map[string]string{"BB:01": "lan1"},
		},
	}
	devices := []Device{{MAC: "BB:01", Online: true, RouterID: "gw", TrafficMbps: 1}}

	l := &Live{}
	l.applySnmpSwitchPortTraffic(polled, devices)

	if devices[0].TrafficMbps != 1 {
		t.Fatalf("agent-router client changed to %.4f, want 1 (helper is SNMP-only)", devices[0].TrafficMbps)
	}
}
