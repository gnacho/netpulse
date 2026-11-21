package probe

import (
	"strings"
	"testing"
)

// Real AirPlay records captured from umdns on 21 Nov 2026. IPs are TEST-NET-1
// and serials, device IDs, public keys and other identifiers are omitted.
const mdnsNestedAirplayFixture = `{
  "_airplay._tcp": {
    "marantz": {
      "iface": "br-lan",
      "txt": "acl=0",
      "txt": "model=Marantz NR1608",
      "txt": "manufacturer=Marantz",
      "host": "marantz.local",
      "port": 7000,
      "ipv4": "192.0.2.22"
    },
    "[LG] webOS TV OLED77CX6LA": {
      "iface": "br-lan",
      "txt": "acl=0",
      "txt": "model=OLED77CX6LA",
      "txt": "manufacturer=LG Electronics"
    }
  }
}`

func TestParseMdnsBrowseNestedAirplayWithRepeatedTXT(t *testing.T) {
	dd := ParseMdnsBrowse([]byte(mdnsNestedAirplayFixture))
	if dd == nil {
		t.Fatal("expected nested mDNS records to parse")
	}
	dd.MergeMdnsHosts(map[string]string{
		"192.0.2.22": "marantz",
		"192.0.2.33": "LGwebOSTV",
	})

	if got := dd.HostByIP["192.0.2.22"]; got != "marantz" {
		t.Errorf("Marantz host = %q, want marantz", got)
	}
	if got := dd.HostByIP["192.0.2.33"]; got != "LGwebOSTV" {
		t.Errorf("LG host = %q, want LGwebOSTV", got)
	}
	if got := dd.Models["marantz"]; got != "Marantz NR1608" {
		t.Errorf("Marantz model = %q, want Marantz NR1608", got)
	}
	if got := dd.Manufacturers["marantz"]; got != "Marantz" {
		t.Errorf("Marantz manufacturer = %q, want Marantz", got)
	}
	if got := dd.Models["LGwebOSTV"]; got != "OLED77CX6LA" {
		t.Errorf("LG model = %q, want OLED77CX6LA", got)
	}
	if got := dd.Manufacturers["LGwebOSTV"]; got != "LG Electronics" {
		t.Errorf("LG manufacturer = %q, want LG Electronics", got)
	}
	for _, host := range []string{"marantz", "LGwebOSTV"} {
		services := dd.Services[host]
		found := false
		for _, service := range services {
			if service == "_airplay._tcp" {
				found = true
			}
		}
		if !found {
			t.Errorf("services for %q = %v, want _airplay._tcp", host, services)
		}
	}
}

func TestParseMdnsBrowseMetadataBounds(t *testing.T) {
	raw := `{"_airplay._tcp":{"bad":{"txt":"model=` + strings.Repeat("x", maxMdnsMetadataLength+1) + `","txt":"manufacturer=LG Electronics","ipv4":"192.0.2.33"}}}`
	dd := ParseMdnsBrowse([]byte(raw))
	if dd == nil {
		t.Fatal("expected record with valid manufacturer")
	}
	if got := dd.Models["bad"]; got != "" {
		t.Errorf("oversized model = %q, want empty", got)
	}
	if got := dd.Manufacturers["bad"]; got != "LG Electronics" {
		t.Errorf("manufacturer = %q, want LG Electronics", got)
	}

	control := ParseMdnsBrowse([]byte(`{"_airplay._tcp":{"bad":{"txt":"model=OLED\n77","ipv4":"192.0.2.33"}}}`))
	if control == nil || control.Models["bad"] != "" {
		t.Errorf("model with control character = %q, want empty", control.Models["bad"])
	}
}

func TestMergeMdnsHostsDoesNotGuessAmbiguousModel(t *testing.T) {
	raw := `{
		"_airplay._tcp": {
			"[LG] webOS TV OLED77CX6LA": {"txt":"model=OLED77CX6LA"},
			"[LG] webOS TV OLED55CX6LA": {"txt":"model=OLED55CX6LA"}
		}
	}`
	dd := ParseMdnsBrowse([]byte(raw))
	dd.MergeMdnsHosts(map[string]string{"192.0.2.33": "LGwebOSTV"})
	if got := dd.Models["LGwebOSTV"]; got != "" {
		t.Errorf("ambiguous model = %q, want empty", got)
	}
	if got := dd.Services["LGwebOSTV"]; len(got) != 0 {
		t.Errorf("ambiguous services = %v, want none", got)
	}
}
