// wireguard_disconnect_test.go - WireGuard disconnect alerts (#1348).
package adapters

import (
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// wgAlertLive builds a Live whose engine accepts VPN alerts.
func wgAlertLive(t *testing.T) *Live {
	t.Helper()
	l := NewLive(nil, nil, nil, nil)
	if err := l.engine.SetConfig(map[string]string{alerts.CatVPN: alerts.LevelAll}); err != nil {
		t.Fatalf("SetConfig vpn: %v", err)
	}
	return l
}

func wgDisconnects(l *Live) []alerts.AlertEvent {
	out := []alerts.AlertEvent{}
	for _, ev := range l.engine.List() {
		if ev.Type == alerts.TypeWireguardDisconnect {
			out = append(out, ev)
		}
	}
	return out
}

func TestWireGuardDisconnectOnActiveToInactive(t *testing.T) {
	l := wgAlertLive(t)
	l.wgActive = map[string]bool{"peerA": true}
	l.wgActiveSince = map[string]time.Time{"peerA": time.Now().Add(-90 * time.Minute)}

	l.applyWireGuardTransitions(&WireGuardStats{Peers: []WGPeer{
		{ID: "peerA", Name: "Alice", Active: false},
	}}, "gw")

	got := wgDisconnects(l)
	if len(got) != 1 {
		t.Fatalf("disconnects: %d, want 1", len(got))
	}
	ev := got[0]
	if ev.Category != alerts.CatVPN {
		t.Errorf("category: %q, want %q", ev.Category, alerts.CatVPN)
	}
	if ev.Urgent {
		t.Errorf("urgent: true, want false")
	}
	if ev.Severity != "info" {
		t.Errorf("severity: %q, want info", ev.Severity)
	}
	if ev.Title != "WireGuard disconnect" {
		t.Errorf("title: %q, want %q", ev.Title, "WireGuard disconnect")
	}
	if ev.Vars["peer"] != "Alice" {
		t.Errorf("vars.peer: %q, want Alice", ev.Vars["peer"])
	}
	if ev.RouterID != "gw" {
		t.Errorf("routerId: %q, want gw", ev.RouterID)
	}
	if !strings.HasPrefix(ev.ID, "alert-wg-down-") {
		t.Errorf("id: %q, want prefix alert-wg-down-", ev.ID)
	}
}

func TestWireGuardNoDisconnectWhileActive(t *testing.T) {
	l := wgAlertLive(t)
	l.wgActive = map[string]bool{"peerA": true}
	l.wgActiveSince = map[string]time.Time{"peerA": time.Now().Add(-90 * time.Minute)}

	l.applyWireGuardTransitions(&WireGuardStats{Peers: []WGPeer{
		{ID: "peerA", Name: "Alice", Active: true},
	}}, "gw")

	if got := wgDisconnects(l); len(got) != 0 {
		t.Fatalf("disconnects: %d, want 0", len(got))
	}
	if !l.wgActive["peerA"] {
		t.Errorf("peerA no longer active")
	}
}

func TestWireGuardNoDisconnectForNeverActive(t *testing.T) {
	l := wgAlertLive(t) // wgActive empty (fresh boot)

	l.applyWireGuardTransitions(&WireGuardStats{Peers: []WGPeer{
		{ID: "peerA", Name: "Alice", Active: false},
	}}, "gw")

	if got := wgDisconnects(l); len(got) != 0 {
		t.Fatalf("disconnects: %d, want 0", len(got))
	}
}

func TestWireGuardDisconnectDuration(t *testing.T) {
	l := wgAlertLive(t)
	l.wgActive = map[string]bool{"peerA": true}
	l.wgActiveSince = map[string]time.Time{"peerA": time.Now().Add(-90 * time.Minute)}

	l.applyWireGuardTransitions(&WireGuardStats{Peers: []WGPeer{
		{ID: "peerA", Name: "Alice", Active: false},
	}}, "gw")

	got := wgDisconnects(l)
	if len(got) != 1 {
		t.Fatalf("disconnects: %d, want 1", len(got))
	}
	if !strings.Contains(got[0].Description, "after 1h 30m") {
		t.Errorf("description: %q, want to contain %q", got[0].Description, "after 1h 30m")
	}
	if got[0].Vars["duration"] != "1h 30m" {
		t.Errorf("vars.duration: %q, want 1h 30m", got[0].Vars["duration"])
	}
}

func TestWireGuardActiveSinceConsistency(t *testing.T) {
	l := wgAlertLive(t)
	base := time.Now().Add(-90 * time.Minute)
	l.wgActive = map[string]bool{"peerA": true}
	l.wgActiveSince = map[string]time.Time{"peerA": base}

	// Disconnect: peer removed from both maps.
	l.applyWireGuardTransitions(&WireGuardStats{Peers: []WGPeer{
		{ID: "peerA", Name: "Alice", Active: false},
	}}, "gw")

	if _, ok := l.wgActive["peerA"]; ok {
		t.Errorf("peerA still in wgActive after disconnect")
	}
	if _, ok := l.wgActiveSince["peerA"]; ok {
		t.Errorf("peerA still in wgActiveSince after disconnect")
	}

	// Reconnect: peer tracked again with a fresh since timestamp.
	l.applyWireGuardTransitions(&WireGuardStats{Peers: []WGPeer{
		{ID: "peerA", Name: "Alice", Active: true},
	}}, "gw")

	if !l.wgActive["peerA"] {
		t.Errorf("peerA not active after reconnect")
	}
	since, ok := l.wgActiveSince["peerA"]
	if !ok {
		t.Fatalf("peerA missing since after reconnect")
	}
	if !since.After(base) {
		t.Errorf("since: %v, want after %v", since, base)
	}
}

// Regression: the connect path still emits a handshake for a newly active peer
// and tracks its since timestamp.
func TestWireGuardHandshakeStillEmitted(t *testing.T) {
	l := wgAlertLive(t)
	l.wgActive = map[string]bool{"peerB": true}
	l.wgActiveSince = map[string]time.Time{"peerB": time.Now().Add(-5 * time.Minute)}

	l.applyWireGuardTransitions(&WireGuardStats{Peers: []WGPeer{
		{ID: "peerA", Name: "Alice", Active: true},
		{ID: "peerB", Name: "Bob", Active: true},
	}}, "gw")

	handshakes := 0
	for _, ev := range l.engine.List() {
		if ev.Type == alerts.TypeWireguardHandshake && ev.Vars["peer"] == "Alice" {
			handshakes++
		}
	}
	if handshakes != 1 {
		t.Fatalf("handshakes for Alice: %d, want 1", handshakes)
	}
	if _, ok := l.wgActiveSince["peerA"]; !ok {
		t.Errorf("peerA missing since")
	}
}
