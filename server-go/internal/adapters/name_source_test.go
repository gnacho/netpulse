package adapters

import (
	"path/filepath"
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestBuildDevicesNameSourceGLiNet(t *testing.T) {
	cfg := RouterConfig{ID: "gateway", Host: "192.0.2.1", Name: "glinet", IsGateway: true, AgentOnly: true}
	l := NewLive(nil, nil, []RouterConfig{cfg}, nil)
	payload := &probe.Payload{
		Router: "gateway", Ts: 1, Version: "2.54.0",
		Data: probe.PayloadData{
			FDB: &probe.FDBData{MACs: map[string]string{"02:00:00:00:20:01": "lan2"}},
			DHCP: &probe.DHCPData{GlClients: []probe.DhcpLease{{
				MAC: "02:00:00:00:20:01", IP: "192.0.2.201", Hostname: "gl-client",
			}}},
		},
	}
	devices := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, payload)}, "")
	if len(devices) != 1 || devices[0].Name != "gl-client" || devices[0].NameSource != "glinet" {
		t.Fatalf("devices = %+v, want gl-client with glinet source", devices)
	}
}

func TestNameSourceSurvivesRetentionAndRestart(t *testing.T) {
	d := openLiveTestDB(t)
	cfg := RouterConfig{ID: "gateway", Host: "192.0.2.1", Name: "openwrt", IsGateway: true, AgentOnly: true}
	l := NewLive(nil, d, []RouterConfig{cfg}, nil)
	const mac = "02:00:00:00:10:09"
	const ip = "192.0.2.109"

	first := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, mdnsPlaceholderPayload(mac, ip, "android-7F3A9C2D", "roomba-cocina", true))}, "")
	l.noteDevicesSeen(first, 1000)
	l.applyDeviceSeen(first)
	if len(first) != 1 || first[0].NameSource != "mdns" {
		t.Fatalf("first cycle nameSource = %+v, want mdns", first)
	}

	second := l.buildDevices(map[string]*routerPolled{"gateway": l.polledFromAgent(cfg, mdnsPlaceholderPayload(mac, ip, "android-7F3A9C2D", "", false))}, "")
	l.noteDevicesSeen(second, 2000)
	l.applyDeviceSeen(second)
	if len(second) != 1 || second[0].NameSource != "mdns" {
		t.Fatalf("retained nameSource = %+v, want mdns", second)
	}
	var stored string
	if err := d.QueryRow("SELECT name_source FROM device_seen WHERE mac = ?", mac).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "mdns" {
		t.Fatalf("stored name_source = %q, want mdns", stored)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("close before reopen: %v", err)
	}
	reopened, err := db.Open(filepath.Dir(d.Path))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	liveAfterRestart := NewLive(nil, reopened, []RouterConfig{cfg}, nil)
	ghosts := liveAfterRestart.ghostDevices(nil)
	if len(ghosts) != 1 || ghosts[0].NameSource != "mdns" {
		t.Fatalf("after restart = %+v, want one ghost with mdns source", ghosts)
	}
}
