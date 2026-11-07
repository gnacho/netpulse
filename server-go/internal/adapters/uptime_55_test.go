package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

// #55: un payload sin system.sysinfo (un pusher que no recoge uptime) dejaba
// uptimeSec en 0 y la tarjeta pintaba "0d 0h", que se lee como un uptime real
// de cero. Sin dato, la respuesta es "—".
func TestRouterWithoutSysInfoShowsNoUptime(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "opnsense", Host: "10.0.0.1", Name: "OPNsense"}}, nil)
	cfg := l.routers[0]
	payload := &probe.Payload{Router: "opnsense", Ts: 1000, Version: "0.1.0", Kind: "external"}
	payload.Data.System = &probe.SystemData{}

	p := l.polledFromAgent(cfg, payload)
	if p.hasUptime {
		t.Fatal("sin system.sysinfo, hasUptime debe quedar false")
	}
	if r := l.buildRouter(p, nil); r.Uptime != "—" {
		t.Fatalf("uptime = %q, esperaba —", r.Uptime)
	}
}

// Un uptime de cero declarado por la fuente sí es un dato: "0d 0h".
func TestRouterWithSysInfoShowsUptime(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "opnsense", Host: "10.0.0.1", Name: "OPNsense"}}, nil)
	cfg := l.routers[0]
	payload := &probe.Payload{Router: "opnsense", Ts: 1000, Version: "0.1.0", Kind: "external"}
	payload.Data.System = &probe.SystemData{SysInfo: &probe.SysInfo{Uptime: 0}}

	p := l.polledFromAgent(cfg, payload)
	if !p.hasUptime {
		t.Fatal("con system.sysinfo, hasUptime debe ser true")
	}
	if r := l.buildRouter(p, nil); r.Uptime != "0d 0h" {
		t.Fatalf("uptime = %q, esperaba 0d 0h", r.Uptime)
	}

	payload.Data.System.SysInfo.Uptime = 90061
	p = l.polledFromAgent(cfg, payload)
	if r := l.buildRouter(p, nil); r.Uptime != "1d 1h" {
		t.Fatalf("uptime = %q, esperaba 1d 1h", r.Uptime)
	}
}
