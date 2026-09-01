package adapters

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// reanchorRunner es un fake sshRunner genérico para los tests de re-anchor.
type reanchorRunner struct {
	responses map[string]string
	errors    map[string]error
}

func (r *reanchorRunner) Run(host, cmd string, timeout time.Duration) (string, error) {
	key := host + "|" + cmd
	if e, ok := r.errors[key]; ok {
		return "", e
	}
	return r.responses[key], nil
}

func TestReanchorUsteer(t *testing.T) {
	const (
		local1 = `{
  "hostapd.phy0-ap0": { "bssid": "9c:9d:7e:1b:ea:b3", "ssid": "temiscira", "freq": 5260, "n_assoc": 1, "load": 3 },
  "hostapd.phy1-ap0": { "bssid": "9c:9d:7e:1b:ea:b2", "ssid": "temiscira", "freq": 2442, "n_assoc": 0, "load": 10 }
}`
		local2 = `{
  "hostapd.phy0-ap0": { "bssid": "cc:d8:43:b1:e5:97", "ssid": "temiscira", "freq": 5180, "n_assoc": 0, "load": 5 }
}`
		remote2 = `{
  "192.168.1.2#hostapd.phy0-ap0": { "bssid": "9c:9d:7e:1b:ea:b3", "ssid": "temiscira", "freq": 5260, "n_assoc": 1, "load": 3 },
  "192.168.1.2#hostapd.phy1-ap0": { "bssid": "9c:9d:7e:1b:ea:b2", "ssid": "temiscira", "freq": 2442, "n_assoc": 0, "load": 10 }
}`
		connected1 = `{
  "hostapd.phy1-ap0": { "aa:bb:cc:dd:ee:ff": { "signal": -80 } }
}`
		hearing1 = `{
  "aa:bb:cc:dd:ee:ff": {
    "hostapd.phy1-ap0": { "connected": true, "signal": -80 },
    "192.168.1.2#hostapd.phy0-ap0": { "connected": false, "signal": -55 }
  }
}`
	)

	runner := &reanchorRunner{
		responses: map[string]string{
			"192.168.1.1|ubus call usteer local_info":        local1,
			"192.168.1.1|ubus call usteer remote_info":       "{}",
			"192.168.1.1|ubus call usteer connected_clients": connected1,
			"192.168.1.1|ubus call usteer get_clients":       hearing1,
			"192.168.1.2|ubus call usteer local_info":        local2,
			"192.168.1.2|ubus call usteer remote_info":       remote2,
			"192.168.1.2|ubus call usteer connected_clients": "{}",
			"192.168.1.2|ubus call usteer get_clients":       "{}",
		},
	}
	routers := []RouterConfig{
		{ID: "rt1", Name: "rt1", Host: "192.168.1.1"},
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2"},
	}

	recs, ok := usteerReanchor(context.Background(), routers, ReanchorConfig{MinRecommendedSignal: -65, MinDeltaDbm: 10}, runner)
	if !ok {
		t.Fatal("expected usteer available")
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}
	r := recs[0]
	if r.MAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("mac = %q", r.MAC)
	}
	if r.CurrentBSSID != "9C:9D:7E:1B:EA:B2" || r.RecommendedBSSID != "9C:9D:7E:1B:EA:B3" {
		t.Errorf("current/recommended bssid = %q / %q", r.CurrentBSSID, r.RecommendedBSSID)
	}
	if r.DeltaDbm != 25 {
		t.Errorf("delta = %d, want 25", r.DeltaDbm)
	}
	if r.CurrentHost != "192.168.1.1" {
		t.Errorf("current host = %q, want 192.168.1.1", r.CurrentHost)
	}
}

func TestReanchorUsteerNoRecommendation(t *testing.T) {
	const (
		local1 = `{
  "hostapd.phy0-ap0": { "bssid": "9c:9d:7e:1b:ea:b3", "ssid": "temiscira", "freq": 5260, "n_assoc": 1, "load": 3 }
}`
		connected1 = `{
  "hostapd.phy0-ap0": { "aa:bb:cc:dd:ee:ff": { "signal": -55 } }
}`
		hearing1 = `{
  "aa:bb:cc:dd:ee:ff": {
    "hostapd.phy0-ap0": { "connected": true, "signal": -55 },
    "192.168.1.2#hostapd.phy0-ap0": { "connected": false, "signal": -60 }
  }
}`
	)
	runner := &reanchorRunner{
		responses: map[string]string{
			"192.168.1.1|ubus call usteer local_info":        local1,
			"192.168.1.1|ubus call usteer remote_info":       "{}",
			"192.168.1.1|ubus call usteer connected_clients": connected1,
			"192.168.1.1|ubus call usteer get_clients":       hearing1,
		},
	}
	routers := []RouterConfig{{ID: "rt1", Name: "rt1", Host: "192.168.1.1"}}

	recs, ok := usteerReanchor(context.Background(), routers, ReanchorConfig{MinRecommendedSignal: -65, MinDeltaDbm: 10}, runner)
	if !ok {
		t.Fatal("expected usteer available")
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0 recommendations, got %+v", recs)
	}
}

func TestReanchorUsteerFallsBackToDawn(t *testing.T) {
	const (
		network = `{
  "temiscira": {
    "9c:9d:7e:1b:ea:b2": {
      "hostname": "rt1", "freq": 2442, "channel": 6, "utilization": 10, "num_sta": 1,
      "clients": { "aa:bb:cc:dd:ee:ff": { "signal": -80 } },
      "local": true, "iface": "wlan0"
    },
    "9c:9d:7e:1b:ea:b3": {
      "hostname": "rt2", "freq": 5260, "channel": 48, "utilization": 5, "num_sta": 0,
      "clients": {},
      "local": false, "iface": "wlan1"
    }
  }
}`
		hearing = `{
  "temiscira": {
    "aa:bb:cc:dd:ee:ff": {
      "9c:9d:7e:1b:ea:b2": { "signal": -80 },
      "9c:9d:7e:1b:ea:b3": { "signal": -55 }
    }
  }
}`
	)
	runner := &reanchorRunner{
		responses: map[string]string{
			"192.168.1.1|ubus call usteer local_info":    "{}",
			"192.168.1.1|ubus call dawn get_network":     network,
			"192.168.1.1|ubus call dawn get_hearing_map": hearing,
		},
	}
	routers := []RouterConfig{{ID: "rt1", Name: "rt1", Host: "192.168.1.1"}}

	recs, ok := dawnReanchor(context.Background(), routers, ReanchorConfig{MinRecommendedSignal: -65, MinDeltaDbm: 10}, runner)
	if !ok {
		t.Fatal("expected DAWN available")
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}
}

func TestReanchorNone(t *testing.T) {
	runner := &reanchorRunner{
		responses: map[string]string{
			"192.168.1.1|ubus call usteer local_info": "{}",
			"192.168.1.1|ubus call dawn get_network":  "{}",
		},
	}
	routers := []RouterConfig{{ID: "rt1", Name: "rt1", Host: "192.168.1.1"}}

	if _, ok := usteerReanchor(context.Background(), routers, ReanchorConfig{}, runner); ok {
		t.Fatal("expected no usteer")
	}
	if _, ok := dawnReanchor(context.Background(), routers, ReanchorConfig{}, runner); ok {
		t.Fatal("expected no DAWN")
	}
}

func TestReanchorKickScript(t *testing.T) {
	got := ReanchorKickScript("aa:bb:cc:dd:ee:ff", "wlan0")
	want := fmt.Sprintf("ubus call hostapd.wlan0 del_client '{\"addr\":\"AA:BB:CC:DD:EE:FF\",\"reason\":5,\"deauth\":true,\"ban_time\":120000}'")
	if got != want {
		t.Fatalf("script = %q, want %q", got, want)
	}
}
