// Package rintegration manages external router integrations (#1674): pushers
// that run on the NetPulse server (mode "local") or on the router itself
// (mode "ssh") and push data to the agent ingest endpoint with the external
// agent contract.
package rintegration

import "strings"

// Vendor is the static manifest of one supported external pusher.
type Vendor struct {
	ID          string // "routeros" | "asuswrt"
	Mode        string // "local" (runs on this host) | "ssh" (installed on the router)
	Repo        string // GitHub repo that publishes the releases
	TagPrefix   string // release tags for this vendor (e.g. "routeros/v")
	AssetTmpl   string // asset name template with {version} and {arch}
	Arches      []string
	Binary      string // binary name inside the tarball
	Description string
}

// AssetName renders the asset name for a resolved version and architecture.
func (v Vendor) AssetName(version, arch string) string {
	name := v.AssetTmpl
	name = strings.ReplaceAll(name, "{version}", version)
	name = strings.ReplaceAll(name, "{arch}", arch)
	return name
}

// vendors is the static manifest. bbox is intentionally absent: its PR is
// still open and is not part of this package.
var vendors = []Vendor{
	{
		ID:          "routeros",
		Mode:        "local",
		Repo:        "gnacho/netpulse-integrations",
		TagPrefix:   "routeros/v",
		AssetTmpl:   "routeros_{version}_linux_{arch}.tar.gz",
		Arches:      []string{"amd64"},
		Binary:      "routeros-pusher",
		Description: "MikroTik RouterOS external pusher",
	},
	{
		ID:          "asuswrt",
		Mode:        "ssh",
		Repo:        "gnacho/netpulse-integrations",
		TagPrefix:   "asuswrt/v",
		AssetTmpl:   "asuswrt_{version}_linux_{arch}.tar.gz",
		Arches:      []string{"arm64"},
		Binary:      "asuswrt-pusher",
		Description: "Asuswrt (Asuswrt-Merlin) external pusher",
	},
	{
		ID:          "unifi",
		Mode:        "local",
		Repo:        "gnacho/netpulse-integrations",
		TagPrefix:   "unifi/v",
		AssetTmpl:   "unifi_{version}_linux_{arch}.tar.gz",
		Arches:      []string{"amd64"},
		Binary:      "unifi-pusher",
		Description: "UniFi controller external pusher",
	},
}

// Vendors returns the supported vendor manifest.
func Vendors() []Vendor { return vendors }

// LookupVendor returns the vendor with the given id and whether it exists.
func LookupVendor(id string) (Vendor, bool) {
	for _, v := range vendors {
		if v.ID == id {
			return v, true
		}
	}
	return Vendor{}, false
}
