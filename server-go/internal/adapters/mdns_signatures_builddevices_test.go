package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestBuildDevicesUsesMdnsProductSignatures(t *testing.T) {
	tests := []struct {
		name         string
		mac          string
		ip           string
		host         string
		model        string
		manufacturer string
		overrideType string
		wantType     string
	}{
		{
			name:         "LG AirPlay signature beats the generic speaker fallback",
			mac:          "02:00:00:00:20:01",
			ip:           "192.0.2.201",
			host:         "device-lg",
			model:        "OLED77CX6LA",
			manufacturer: "LG Electronics",
			wantType:     "tv",
		},
		{
			name:         "Marantz AirPlay signature stays a speaker",
			mac:          "02:00:00:00:20:02",
			ip:           "192.0.2.202",
			host:         "device-marantz",
			model:        "Marantz NR1608",
			manufacturer: "Marantz",
			wantType:     "altavoz",
		},
		{
			name:         "payload without metadata keeps the generic fallback",
			mac:          "02:00:00:00:20:03",
			ip:           "192.0.2.203",
			host:         "device-other",
			manufacturer: "Marantz",
			wantType:     "altavoz",
		},
		{
			name:         "simulated Apple TV candidate keeps the generic fallback",
			mac:          "02:00:00:00:20:04",
			ip:           "192.0.2.204",
			host:         "device-apple",
			model:        "AppleTV14,1",
			manufacturer: "Apple",
			wantType:     "altavoz",
		},
		{
			name:         "manual type override wins over a signature",
			mac:          "02:00:00:00:20:05",
			ip:           "192.0.2.205",
			host:         "device-lg",
			model:        "OLED77CX6LA",
			manufacturer: "LG Electronics",
			overrideType: "consola",
			wantType:     "consola",
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
			payload := &probe.Payload{
				Router: "gateway", Ts: 1, Version: "2.54.0",
				Data: probe.PayloadData{
					FDB: &probe.FDBData{MACs: map[string]string{tt.mac: "lan2"}},
					Arp: map[string]string{tt.mac: tt.ip},
					Discovery: &probe.DiscoveryData{
						HostByIP:      map[string]string{tt.ip: tt.host},
						Services:      map[string][]string{tt.host: {"_airplay._tcp"}},
						Models:        map[string]string{tt.host: tt.model},
						Manufacturers: map[string]string{tt.host: tt.manufacturer},
					},
				},
			}

			devices := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, payload)}, "")
			for _, d := range devices {
				if d.MAC != tt.mac {
					continue
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
