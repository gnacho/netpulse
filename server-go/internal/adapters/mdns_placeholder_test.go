package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestBuildDevicesMdnsReplacesOnlyUnpinnedPlaceholders(t *testing.T) {
	tests := []struct {
		name            string
		mac             string
		ip              string
		dhcpName        string
		mdnsName        string
		reservationName string
		aliasName       string
		overrideName    string
		overrideType    string
		wantName        string
		wantType        string
	}{
		{
			name:     "mDNS name replaces an Android DHCP placeholder",
			mac:      "02:00:00:00:10:01",
			ip:       "192.0.2.101",
			dhcpName: "android-7F3A9C2D",
			mdnsName: "iPhone-de-Nacho",
			wantName: "iPhone-de-Nacho",
			wantType: "movil",
		},
		{
			name:     "mDNS name and type replace an Android DHCP placeholder",
			mac:      "02:00:00:00:10:02",
			ip:       "192.0.2.102",
			dhcpName: "android-7F3A9C2D",
			mdnsName: "roomba-cocina",
			wantName: "roomba-cocina",
			wantType: "aspirador",
		},
		{
			name:     "meaningful DHCP hostname stays",
			mac:      "02:00:00:00:10:03",
			ip:       "192.0.2.103",
			dhcpName: "laptop-ana",
			mdnsName: "roomba-cocina",
			wantName: "laptop-ana",
			wantType: "portatil",
		},
		{
			name:     "mDNS placeholder cannot replace a meaningful DHCP hostname",
			mac:      "02:00:00:00:10:04",
			ip:       "192.0.2.104",
			dhcpName: "laptop-ana",
			mdnsName: "android-7F3A9C2D",
			wantName: "laptop-ana",
			wantType: "portatil",
		},
		{
			name:            "DHCP reservation name stays",
			mac:             "02:00:00:00:10:05",
			ip:              "192.0.2.105",
			mdnsName:        "roomba-cocina",
			reservationName: "Reserva Ana",
			wantName:        "Reserva Ana",
			wantType:        "desconocido",
		},
		{
			name:      "known MAC alias stays",
			mac:       "02:00:00:00:10:06",
			ip:        "192.0.2.106",
			mdnsName:  "roomba-cocina",
			aliasName: "Alias Ana",
			wantName:  "Alias Ana",
			wantType:  "desconocido",
		},
		{
			name:         "manual name and type stay",
			mac:          "02:00:00:00:10:07",
			ip:           "192.0.2.107",
			dhcpName:     "android-7F3A9C2D",
			mdnsName:     "roomba-cocina",
			overrideName: "Consola salón",
			overrideType: "consola",
			wantName:     "Consola salón",
			wantType:     "consola",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := RouterConfig{ID: "gateway", Host: "192.0.2.1", Name: "openwrt", IsGateway: true, AgentOnly: true}
			var l *Live
			if tt.aliasName != "" || tt.overrideName != "" || tt.overrideType != "" {
				d := openLiveTestDB(t)
				if tt.aliasName != "" {
					if _, err := d.Exec("INSERT INTO known_macs (mac, name, created_at) VALUES (?, ?, 1)", tt.mac, tt.aliasName); err != nil {
						t.Fatalf("seed alias: %v", err)
					}
				}
				if tt.overrideName != "" || tt.overrideType != "" {
					if _, err := d.Exec(`INSERT INTO device_overrides (mac, icon, name, device_type, banned_bands, created_at, updated_at)
						VALUES (?, '', ?, ?, '', 1, 1)`, tt.mac, tt.overrideName, tt.overrideType); err != nil {
						t.Fatalf("seed override: %v", err)
					}
				}
				l = NewLive(nil, d, []RouterConfig{cfg}, nil)
			} else {
				l = NewLive(nil, nil, []RouterConfig{cfg}, nil)
			}

			dhcp := &probe.DHCPData{}
			if tt.dhcpName != "" {
				dhcp.Leases = []probe.DhcpLease{{MAC: tt.mac, IP: tt.ip, Hostname: tt.dhcpName}}
			}
			if tt.reservationName != "" {
				dhcp.Reservations = []probe.DhcpReservation{{MAC: tt.mac, IP: tt.ip, Name: tt.reservationName}}
			}
			payload := &probe.Payload{
				Router: "gateway", Ts: 1, Version: "2.54.0",
				Data: probe.PayloadData{
					FDB:  &probe.FDBData{MACs: map[string]string{tt.mac: "lan2"}},
					Arp:  map[string]string{tt.mac: tt.ip},
					DHCP: dhcp,
					Discovery: &probe.DiscoveryData{
						HostByIP: map[string]string{tt.ip: tt.mdnsName},
						Services: map[string][]string{tt.mdnsName: nil},
					},
				},
			}

			devices := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, payload)}, "")
			for _, d := range devices {
				if d.MAC != tt.mac {
					continue
				}
				if d.Name != tt.wantName {
					t.Errorf("name = %q, want %q", d.Name, tt.wantName)
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

func TestMdnsImprovedNameSurvivesMissingAnnouncement(t *testing.T) {
	d := openLiveTestDB(t)
	cfg := RouterConfig{ID: "gateway", Host: "192.0.2.1", Name: "openwrt", IsGateway: true, AgentOnly: true}
	l := NewLive(nil, d, []RouterConfig{cfg}, nil)
	const mac = "02:00:00:00:10:08"
	const ip = "192.0.2.108"

	firstPayload := mdnsPlaceholderPayload(mac, ip, "android-7F3A9C2D", "roomba-cocina", true)
	first := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, firstPayload)}, "")
	if len(first) != 1 || first[0].Name != "roomba-cocina" || first[0].Type != "aspirador" {
		t.Fatalf("first cycle = %+v, want roomba-cocina/aspirador", first)
	}
	l.noteDevicesSeen(first, 1000)
	l.applyDeviceSeen(first)

	secondPayload := mdnsPlaceholderPayload(mac, ip, "android-7F3A9C2D", "", false)
	second := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, secondPayload)}, "")
	l.noteDevicesSeen(second, 2000)
	l.applyDeviceSeen(second)
	if len(second) != 1 {
		t.Fatalf("second cycle has %d devices, want 1", len(second))
	}
	if second[0].Name != "roomba-cocina" {
		t.Errorf("second cycle name = %q, want retained roomba-cocina", second[0].Name)
	}
	if second[0].Type != "aspirador" {
		t.Errorf("second cycle type = %q, want retained aspirador", second[0].Type)
	}

	var storedName, storedType string
	if err := d.QueryRow("SELECT name, device_type FROM device_seen WHERE mac = ?", mac).Scan(&storedName, &storedType); err != nil {
		t.Fatal(err)
	}
	if storedName != "roomba-cocina" || storedType != "aspirador" {
		t.Errorf("stored identity = %q/%q, want roomba-cocina/aspirador", storedName, storedType)
	}
}

func mdnsPlaceholderPayload(mac, ip, dhcpName, mdnsName string, withMDNS bool) *probe.Payload {
	data := probe.PayloadData{
		Wireless: &probe.WirelessData{Clients: map[string]probe.WirelessClient{
			mac: {SignalDbm: -50, Band: "5 GHz"},
		}},
		FDB:  &probe.FDBData{MACs: map[string]string{mac: "lan2"}},
		Arp:  map[string]string{mac: ip},
		DHCP: &probe.DHCPData{Leases: []probe.DhcpLease{{MAC: mac, IP: ip, Hostname: dhcpName}}},
	}
	if withMDNS {
		data.Discovery = &probe.DiscoveryData{
			HostByIP: map[string]string{ip: mdnsName},
			Services: map[string][]string{mdnsName: nil},
		}
	}
	return &probe.Payload{Router: "gateway", Ts: 1, Version: "2.54.0", Data: data}
}
