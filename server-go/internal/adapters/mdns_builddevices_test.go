package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestBuildDevicesClassifiesMdnsHostname(t *testing.T) {
	tests := []struct {
		name         string
		mac          string
		ip           string
		mdnsName     string
		services     []string
		vendorClass  string
		overrideType string
		wantType     string
	}{
		{
			name:     "vacuum hostname feeds the existing hostname rules",
			mac:      "02:00:00:00:00:01",
			ip:       "192.0.2.51",
			mdnsName: "roomba-cocina",
			wantType: "aspirador",
		},
		{
			name:     "console hostname feeds the existing hostname rules",
			mac:      "02:00:00:00:00:02",
			ip:       "192.0.2.52",
			mdnsName: "PS5-salon",
			wantType: "consola",
		},
		{
			name:     "opaque hostname stays unknown",
			mac:      "02:00:00:00:00:03",
			ip:       "192.0.2.53",
			mdnsName: "device-123",
			wantType: "desconocido",
		},
		{
			name:     "generic service fallback still applies",
			mac:      "02:00:00:00:00:04",
			ip:       "192.0.2.54",
			mdnsName: "device-123",
			services: []string{"_ipp._tcp"},
			wantType: "iot",
		},
		{
			name:        "type already resolved from DHCP does not change",
			mac:         "02:00:00:00:00:05",
			ip:          "192.0.2.55",
			mdnsName:    "roomba-cocina",
			vendorClass: "MSFT 5.0",
			wantType:    "ordenador",
		},
		{
			name:         "manual type override wins",
			mac:          "02:00:00:00:00:06",
			ip:           "192.0.2.56",
			mdnsName:     "roomba-cocina",
			overrideType: "tv",
			wantType:     "tv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := RouterConfig{ID: "gateway", Host: "192.0.2.1", Name: "openwrt", IsGateway: true, AgentOnly: true}
			l := NewLive(nil, nil, []RouterConfig{cfg}, nil)
			if tt.overrideType != "" {
				d := openLiveTestDB(t)
				if _, err := d.Exec(`INSERT INTO device_overrides (mac, icon, name, device_type, banned_bands, created_at, updated_at)
					VALUES (?, '', '', ?, '', 1, 1)`, tt.mac, tt.overrideType); err != nil {
					t.Fatalf("seed override: %v", err)
				}
				l = NewLive(nil, d, []RouterConfig{cfg}, nil)
			}
			var dhcp *probe.DHCPData
			if tt.vendorClass != "" {
				dhcp = &probe.DHCPData{Leases: []probe.DhcpLease{{MAC: tt.mac, IP: tt.ip, VendorClass: tt.vendorClass}}}
			}
			payload := &probe.Payload{
				Router: "gateway", Ts: 1, Version: "2.54.0",
				Data: probe.PayloadData{
					FDB:  &probe.FDBData{MACs: map[string]string{tt.mac: "lan2"}},
					Arp:  map[string]string{tt.mac: tt.ip},
					DHCP: dhcp,
					Discovery: &probe.DiscoveryData{
						HostByIP: map[string]string{tt.ip: tt.mdnsName},
						Services: map[string][]string{tt.mdnsName: tt.services},
					},
				},
			}

			devices := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, payload)}, "")
			for _, d := range devices {
				if d.MAC != tt.mac {
					continue
				}
				if d.Name != tt.mdnsName {
					t.Fatalf("name = %q, want mDNS hostname %q", d.Name, tt.mdnsName)
				}
				if d.Type != tt.wantType {
					t.Errorf("type = %q, want %q", d.Type, tt.wantType)
				}
				return
			}
			t.Fatalf("device %s not found", tt.mac)
		})
	}
}
