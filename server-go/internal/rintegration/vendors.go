// Package rintegration manages external router integrations (#1674): pushers
// that run on the NetPulse server (mode "local") or on the router itself
// (mode "ssh") and push data to the agent ingest endpoint with the external
// agent contract.
package rintegration

import "strings"

// Vendor is one supported external pusher as resolved from the vendor
// catalog. The catalog entries (vendors.json in netpulse-integrations) decide
// which vendors exist; the fields the schema does not carry (Repo, AssetTmpl,
// Init) are derived server-side by the Catalog when it builds this struct.
type Vendor struct {
	ID          string // catalog "id"
	Mode        string // "local" (runs on this host) | "ssh" (installed on the router)
	Repo        string // GitHub repo that publishes the releases (derived: CatalogRepo)
	TagPrefix   string // release tags for this vendor (e.g. "routeros/v")
	AssetTmpl   string // asset name template with {version} and {arch} (derived)
	Arches      []string
	Binary      string // binary name inside the tarball (catalog "binary")
	Init        string // on-router init system for Mode "ssh": "procd" (default when empty) | "merlin" (#1681, derived)
	Description string
}

// InitSystem returns the on-router layout used by the ssh install scripts:
// "merlin" (/jffs plus a services-start hook) or "procd" (OpenWrt init.d).
// An empty Init means procd, the layout vendors shipped with before #1681.
func (v Vendor) InitSystem() string {
	if v.Init == "merlin" {
		return "merlin"
	}
	return "procd"
}

// AssetName renders the asset name for a resolved version and architecture.
func (v Vendor) AssetName(version, arch string) string {
	name := v.AssetTmpl
	name = strings.ReplaceAll(name, "{version}", version)
	name = strings.ReplaceAll(name, "{arch}", arch)
	return name
}
