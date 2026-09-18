// multiwan_alert_test.go — the internet moving from one connection to
// another is news; it bouncing for a few seconds is not.
package adapters

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

func uplinkLive(t *testing.T) *Live {
	t.Helper()
	return NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Host: "192.0.2.1", Name: "Gateway", IsGateway: true, AgentOnly: true},
	}, nil)
}

// polledWith builds a poll result reporting two connections, the named one
// carrying traffic.
func polledWith(active, primary string, metered ...string) *routerPolled {
	mw := &probe.MultiWanInfo{
		Mode: "failover", Managed: true, Active: active, Primary: primary,
		Uplinks: []probe.WanUplink{{Name: "fiber", Up: true}, {Name: "cell", Up: true}},
	}
	for i := range mw.Uplinks {
		for _, m := range metered {
			if mw.Uplinks[i].Name == m {
				mw.Uplinks[i].Metered = true
			}
		}
		mw.Uplinks[i].Active = mw.Uplinks[i].Name == active
	}
	return &routerPolled{multiWan: mw}
}

func alertTypes(l *Live) []string {
	out := []string{}
	for _, ev := range l.engine.List() {
		out = append(out, ev.Type)
	}
	return out
}

// The first sighting is recorded, not announced: a server restart must not
// report a failover that happened while it was down.
func TestUplinkFirstObservationIsSilent(t *testing.T) {
	l := uplinkLive(t)
	now := time.Now()
	l.trackUplinkChange(l.routers[0], polledWith("cell", "fiber"), now)
	if got := len(l.engine.List()); got != 0 {
		t.Fatalf("%d alerts on the first poll, want none", got)
	}
}

func TestUplinkSwitchAlertsOnceItHolds(t *testing.T) {
	l := uplinkLive(t)
	now := time.Now()
	cfg := l.routers[0]
	l.trackUplinkChange(cfg, polledWith("fiber", "fiber"), now)

	// Seen on the other connection, but not yet believed. The settle
	// clock starts here, at the first sighting, not at the first poll.
	seen := now.Add(5 * time.Second)
	l.trackUplinkChange(cfg, polledWith("cell", "fiber", "cell"), seen)
	if got := len(l.engine.List()); got != 0 {
		t.Fatalf("%d alerts before the change settled, want none", got)
	}
	l.trackUplinkChange(cfg, polledWith("cell", "fiber", "cell"), seen.Add(uplinkSettle-time.Second))
	if got := len(l.engine.List()); got != 0 {
		t.Fatalf("%d alerts one second short of the window, want none", got)
	}
	// Still there past the window: now it counts.
	l.trackUplinkChange(cfg, polledWith("cell", "fiber", "cell"), seen.Add(uplinkSettle+time.Second))
	list := l.engine.List()
	if len(list) != 1 || list[0].Type != "uplink-switched" {
		t.Fatalf("alerts = %v", alertTypes(l))
	}
	ev := list[0]
	if ev.Vars["from"] != "fiber" || ev.Vars["to"] != "cell" || ev.Vars["router"] != "Gateway" {
		t.Fatalf("vars = %v", ev.Vars)
	}
	// Moving onto a pay-per-use line says so: it starts costing money.
	if ev.Vars["metered"] != "1" {
		t.Fatalf("vars = %v, want the destination marked as metered", ev.Vars)
	}
	if !ev.Urgent {
		t.Fatal("the internet category drops anything not urgent by default")
	}
}

// A line that drops and comes straight back produces nothing at all —
// neither the switch nor a recovery for it.
func TestUplinkFlapIsNotReported(t *testing.T) {
	l := uplinkLive(t)
	now := time.Now()
	cfg := l.routers[0]
	l.trackUplinkChange(cfg, polledWith("fiber", "fiber"), now)
	l.trackUplinkChange(cfg, polledWith("cell", "fiber"), now.Add(5*time.Second))
	l.trackUplinkChange(cfg, polledWith("fiber", "fiber"), now.Add(20*time.Second))
	l.trackUplinkChange(cfg, polledWith("fiber", "fiber"), now.Add(2*time.Minute))
	if got := alertTypes(l); len(got) != 0 {
		t.Fatalf("alerts = %v, want none for a bounce", got)
	}
	// And a real move afterwards still alerts.
	l.trackUplinkChange(cfg, polledWith("cell", "fiber"), now.Add(3*time.Minute))
	l.trackUplinkChange(cfg, polledWith("cell", "fiber"), now.Add(5*time.Minute))
	if got := alertTypes(l); len(got) != 1 || got[0] != "uplink-switched" {
		t.Fatalf("alerts = %v", got)
	}
}

// Coming back to the main connection is its own event: after a failover,
// "it is over" is the part somebody is waiting for.
func TestUplinkReturnToMainIsItsOwnAlert(t *testing.T) {
	l := uplinkLive(t)
	now := time.Now()
	cfg := l.routers[0]
	l.trackUplinkChange(cfg, polledWith("fiber", "fiber"), now)
	l.trackUplinkChange(cfg, polledWith("cell", "fiber"), now.Add(time.Minute))
	l.trackUplinkChange(cfg, polledWith("cell", "fiber"), now.Add(2*time.Minute))
	l.trackUplinkChange(cfg, polledWith("fiber", "fiber"), now.Add(10*time.Minute))
	l.trackUplinkChange(cfg, polledWith("fiber", "fiber"), now.Add(11*time.Minute))

	got := alertTypes(l)
	if len(got) != 2 || got[1] != "uplink-switched" || got[0] != "uplink-restored" {
		t.Fatalf("alerts = %v, want a switch then a recovery", got)
	}
	// Both survive the engine's dedup window, which keys on the title: the
	// destination is in there for exactly this reason.
	if l.engine.List()[0].Title == l.engine.List()[1].Title {
		t.Fatal("the two events must not share a title")
	}
}

// Nothing to fail over between, nothing to say — and the router is
// forgotten, so a second line added later starts from silence.
func TestUplinkSingleConnectionIsForgotten(t *testing.T) {
	l := uplinkLive(t)
	now := time.Now()
	cfg := l.routers[0]
	l.trackUplinkChange(cfg, polledWith("fiber", "fiber"), now)

	one := &routerPolled{multiWan: &probe.MultiWanInfo{
		Mode: "off", Uplinks: []probe.WanUplink{{Name: "fiber", Up: true, Active: true}},
	}}
	l.trackUplinkChange(cfg, one, now.Add(time.Minute))
	if _, ok := l.uplinkWatch[cfg.ID]; ok {
		t.Fatal("a router with one connection should not be watched")
	}
	l.trackUplinkChange(cfg, polledWith("cell", "fiber"), now.Add(2*time.Minute))
	l.trackUplinkChange(cfg, polledWith("cell", "fiber"), now.Add(3*time.Minute))
	if got := alertTypes(l); len(got) != 0 {
		t.Fatalf("alerts = %v, want none: the history was dropped", got)
	}
}

// A router that reports nothing, or a policy steering nothing, is not a
// change and must not disturb what is already known.
func TestUplinkNoDataChangesNothing(t *testing.T) {
	l := uplinkLive(t)
	now := time.Now()
	cfg := l.routers[0]
	l.trackUplinkChange(cfg, polledWith("fiber", "fiber"), now)

	l.trackUplinkChange(cfg, nil, now.Add(time.Minute))
	l.trackUplinkChange(cfg, &routerPolled{}, now.Add(2*time.Minute))
	nothingSteering := &routerPolled{multiWan: &probe.MultiWanInfo{
		Mode: "off", Uplinks: []probe.WanUplink{{Name: "fiber"}, {Name: "cell"}},
	}}
	l.trackUplinkChange(cfg, nothingSteering, now.Add(3*time.Minute))

	if got := alertTypes(l); len(got) != 0 {
		t.Fatalf("alerts = %v, want none", got)
	}
	if w := l.uplinkWatch[cfg.ID]; w == nil || w.active != "fiber" {
		t.Fatalf("watch = %+v, want the known state kept", w)
	}
}

// A router can push whatever it likes, so the two shapes that could reach
// the page as something other than data are pinned here.
func TestMultiWanFromAnUnknownAgentIsSafeToServe(t *testing.T) {
	// An older agent sends no section at all: the field is absent from the
	// response entirely, and the page renders without the panel.
	var old probe.Payload
	if err := json.Unmarshal([]byte(`{"router":"gw","ts":1,"data":{"system":{}}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Data.MultiWan != nil {
		t.Fatalf("expected no section, got %+v", old.Data.MultiWan)
	}
	body, err := json.Marshal(RouterDetail{MultiWan: normalizeMultiWan(old.Data.MultiWan)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "multiWan") {
		t.Fatalf("an absent section must not appear in the response: %s", body)
	}

	// A section without its list would otherwise go out as a null, and the
	// page reads a length from it.
	var partial probe.Payload
	if err := json.Unmarshal([]byte(`{"router":"gw","ts":1,"data":{"multiWan":{"mode":"off"}}}`), &partial); err != nil {
		t.Fatal(err)
	}
	body, err = json.Marshal(RouterDetail{MultiWan: normalizeMultiWan(partial.Data.MultiWan)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"uplinks":null`) {
		t.Fatalf("the list must never be served as null: %s", body)
	}
	if !strings.Contains(string(body), `"uplinks":[]`) {
		t.Fatalf("the list must be an empty array: %s", body)
	}
}

// And the tracker must not act on any of it.
func TestUplinkTrackerIgnoresAMalformedSection(t *testing.T) {
	l := uplinkLive(t)
	cfg := l.routers[0]
	now := time.Now()
	l.trackUplinkChange(cfg, polledWith("fiber", "fiber"), now)
	l.trackUplinkChange(cfg, &routerPolled{multiWan: &probe.MultiWanInfo{Mode: "off"}}, now.Add(time.Minute))
	l.trackUplinkChange(cfg, &routerPolled{multiWan: &probe.MultiWanInfo{Active: "ghost"}}, now.Add(2*time.Minute))
	if got := alertTypes(l); len(got) != 0 {
		t.Fatalf("alerts = %v, want none from a section with no connections", got)
	}
}
