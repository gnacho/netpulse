// multiwan.go — the uplink policy a router's own panel manages.
//
// A router with two internet connections runs a policy: one carries traffic
// and the other waits, or both carry a share of it. NetPulse does not decide
// any of that — the router's panel does — but it has to report it, because
// without it the connection panel shows the address of whichever uplink holds
// the cheapest route, which under a mark-based policy is not the one traffic
// leaves by. The address on screen is then the address nobody is using.
package probe

// MultiWanInfo mirrors the policy as the panel reports it. nil means this
// router reports no multi-WAN at all — no policy manager installed, a single
// uplink, or simply an older agent.
type MultiWanInfo struct {
	// Mode: off | failover | balance | custom. "custom" is a configuration
	// the panel recognises but did not write.
	Mode string `json:"mode"`
	// Managed: the panel wrote this configuration itself.
	Managed bool `json:"managed,omitempty"`
	// Active: the uplink carrying traffic right now.
	Active string `json:"active,omitempty"`
	// Primary: the preferred uplink, in failover mode.
	Primary string `json:"primary,omitempty"`
	// Sticky: while balancing, each device stays on one connection. Worth
	// saying out loud, or the percentages read as a per-device mix.
	Sticky  bool        `json:"sticky,omitempty"`
	Uplinks []WanUplink `json:"uplinks"`
}

// WanUplink is one internet connection. Deliberately fewer fields than the
// panel holds: metrics, weights, tracking targets and section names are how
// the policy is expressed, not what somebody reading a dashboard needs.
type WanUplink struct {
	Name    string `json:"name"`
	Proto   string `json:"proto,omitempty"`
	Port    string `json:"port,omitempty"` // physical socket; empty on a modem
	IP      string `json:"ip,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	Up      bool   `json:"up"`
	Active  bool   `json:"active"`
	Primary bool   `json:"primary,omitempty"`
	// SharePct: portion of traffic this one carries while balancing.
	SharePct int `json:"sharePct,omitempty"`
	// Metered: mobile broadband, where traffic costs money.
	Metered bool `json:"metered,omitempty"`
	// Online: the policy manager's own verdict on the link —
	// online | offline | unknown. Empty when it is not running.
	Online string `json:"online,omitempty"`
}

// ActiveUplinkName is the uplink carrying traffic, or "" when there is no
// policy saying so. Fed to the prober, which then reports that connection's
// address instead of the one holding the cheapest route.
func ActiveUplinkName(m *MultiWanInfo) string {
	if m == nil {
		return ""
	}
	return m.Active
}
