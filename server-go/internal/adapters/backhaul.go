// backhaul.go — choosing the uplink port that carries a non-gateway unit's
// backhaul toward the rest of the network (issue #1544).
package adapters

// pickBackhaulUplink returns the port that carries a non-gateway unit's uplink,
// or nil when it has no up port. The uplink is a port the enrichment flagged as
// a router link (PeerKind router-link / router-link-lldp). A managed switch has
// several router-links - its parent uplink plus downlinks to mesh APs - so the
// fastest one is the core uplink (e.g. 1/xg27 10G beats 1G AP downlinks). The
// previous matcher looked for Detail == "enlace entre routers", a string that is
// never set server-side (it is a frontend i18n label), so it always fell through
// to the first UP port - which on a switch is an arbitrary access port, making a
// 10G-uplinked switch report a camera port's 100 Mbps as its backhaul. The
// first-UP fallback is kept only when there is no router-link peer at all (a
// single-cable AP without LLDP).
func pickBackhaulUplink(enriched []EthPort) *EthPort {
	var uplink *EthPort
	bestMbps := -1
	for i := range enriched {
		ep := &enriched[i]
		if !ep.Up || (ep.PeerKind != PeerKindRouterLink && ep.PeerKind != PeerKindRouterLinkLLDP) {
			continue
		}
		if mb := parseSpeedMbps(ep.Speed); mb > bestMbps {
			bestMbps, uplink = mb, ep
		}
	}
	if uplink != nil {
		return uplink
	}
	for i := range enriched {
		if enriched[i].Up {
			return &enriched[i]
		}
	}
	return nil
}
