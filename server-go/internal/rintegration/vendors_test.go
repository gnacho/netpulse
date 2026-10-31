package rintegration

import "testing"

func TestVendorsManifest(t *testing.T) {
	vs := Vendors()
	if len(vs) != 3 {
		t.Fatalf("expected 3 vendors, got %d", len(vs))
	}

	r, ok := LookupVendor("routeros")
	if !ok {
		t.Fatal("routeros vendor missing")
	}
	if r.Mode != "local" || r.TagPrefix != "routeros/v" || r.Binary != "routeros-pusher" {
		t.Fatalf("routeros manifest mismatch: %+v", r)
	}
	if len(r.Arches) != 1 || r.Arches[0] != "amd64" {
		t.Fatalf("routeros arches = %v, want [amd64]", r.Arches)
	}
	if got := r.AssetName("1.2.3", "amd64"); got != "routeros_1.2.3_linux_amd64.tar.gz" {
		t.Fatalf("routeros asset name = %q", got)
	}

	a, ok := LookupVendor("asuswrt")
	if !ok {
		t.Fatal("asuswrt vendor missing")
	}
	if a.Mode != "ssh" || a.TagPrefix != "asuswrt/v" || a.Binary != "asuswrt-pusher" {
		t.Fatalf("asuswrt manifest mismatch: %+v", a)
	}
	if a.InitSystem() != "merlin" {
		t.Fatalf("asuswrt init system = %q, want merlin", a.InitSystem())
	}
	r2, _ := LookupVendor("routeros")
	if r2.InitSystem() != "procd" {
		t.Fatalf("routeros init system = %q, want procd (empty Init default)", r2.InitSystem())
	}
	if len(a.Arches) != 1 || a.Arches[0] != "arm64" {
		t.Fatalf("asuswrt arches = %v, want [arm64]", a.Arches)
	}
	if got := a.AssetName("1.0.0", "arm64"); got != "asuswrt_1.0.0_linux_arm64.tar.gz" {
		t.Fatalf("asuswrt asset name = %q", got)
	}

	u, ok := LookupVendor("unifi")
	if !ok {
		t.Fatal("unifi vendor missing")
	}
	if u.Mode != "local" || u.TagPrefix != "unifi/v" || u.Binary != "unifi-pusher" {
		t.Fatalf("unifi manifest mismatch: %+v", u)
	}
	if len(u.Arches) != 1 || u.Arches[0] != "amd64" {
		t.Fatalf("unifi arches = %v, want [amd64]", u.Arches)
	}
	if got := u.AssetName("1.0.0", "amd64"); got != "unifi_1.0.0_linux_amd64.tar.gz" {
		t.Fatalf("unifi asset name = %q", got)
	}

	if _, ok := LookupVendor("bbox"); ok {
		t.Fatal("bbox must not be present")
	}
}
