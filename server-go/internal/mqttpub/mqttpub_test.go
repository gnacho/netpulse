package mqttpub

import (
	"context"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestConfigFromEnv(t *testing.T) {
	// Disabled by default.
	if cfg := ConfigFromEnv(lookupFrom(nil)); cfg.Enabled {
		t.Fatal("config must be disabled by default")
	}

	// Enabled without host stays disabled.
	cfg := ConfigFromEnv(lookupFrom(map[string]string{"NETPULSE_MQTT_ENABLED": "1"}))
	if cfg.Enabled {
		t.Fatal("enabled without a host must be disabled")
	}

	// Full config.
	cfg = ConfigFromEnv(lookupFrom(map[string]string{
		"NETPULSE_MQTT_ENABLED":  "true",
		"NETPULSE_MQTT_HOST":     "broker.local",
		"NETPULSE_MQTT_PORT":     "1884",
		"NETPULSE_MQTT_USER":     "netpulse",
		"NETPULSE_MQTT_PASS":     "s3cret",
		"NETPULSE_MQTT_INSTANCE": "home",
		"NETPULSE_MQTT_INTERVAL": "15",
	}))
	if !cfg.Enabled || cfg.Host != "broker.local" || cfg.Port != 1884 ||
		cfg.User != "netpulse" || cfg.Pass != "s3cret" || cfg.Instance != "home" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Interval.Seconds() != 15 {
		t.Fatalf("interval = %v, want 15s", cfg.Interval)
	}

	// Instance is sanitized for MQTT topics.
	cfg = ConfigFromEnv(lookupFrom(map[string]string{
		"NETPULSE_MQTT_ENABLED":  "1",
		"NETPULSE_MQTT_HOST":     "b",
		"NETPULSE_MQTT_INSTANCE": "home/#1",
	}))
	if cfg.Instance != "home--1" {
		t.Fatalf("instance = %q, want home--1", cfg.Instance)
	}
}

func sampleOverview() *adapters.Overview {
	cpu, ram, temp := 12, 40, 55
	return &adapters.Overview{
		Health:       adapters.HealthScore{Score: 92, Label: "Excelente"},
		DeviceTotals: adapters.DeviceTotals{Total: 30, Online: 27},
		UnreadAlerts: 2,
		WAN:          adapters.WAN{Plan: "600/600"},
		Routers: []adapters.Router{
			{ID: "gw", Name: "Gateway", Model: "MT6000", Status: "online", Health: 100,
				CPU: &cpu, RAM: &ram, Temp: &temp, Clients: 18, Uptime: "3d 4h"},
			{ID: "ap-1", Name: "AP 1", Status: "offline", Health: 0},
		},
		Alerts: []adapters.AlertEvent{{ID: "a1", Severity: "warn", Title: "Temp",
			RouterID: "gw", Ts: 1000}},
	}
}

func TestBuildStatus(t *testing.T) {
	st := buildStatus("home", "v1.2.3", sampleOverview())
	if st.HealthScore != 92 || st.ClientsOnline != 27 || st.ClientsTotal != 30 {
		t.Fatalf("unexpected status: %+v", st)
	}
	if st.RoutersOnline != 1 || st.RoutersTotal != 2 {
		t.Fatalf("router counts = %d/%d, want 1/2", st.RoutersOnline, st.RoutersTotal)
	}
	if st.UnreadAlerts != 2 || st.Plan != "600/600" {
		t.Fatalf("unexpected status: %+v", st)
	}
}

func TestBuildRouterState(t *testing.T) {
	r := sampleOverview().Routers[0]
	rs := buildRouterState(r)
	if rs.Slug != "gw" || rs.Status != "online" || rs.Clients != 18 {
		t.Fatalf("unexpected router state: %+v", rs)
	}
	if rs.CPU == nil || *rs.CPU != 12 || rs.Temp == nil || *rs.Temp != 55 {
		t.Fatalf("vitals not carried over: %+v", rs)
	}
}

func TestRouterSlugFallsBackToName(t *testing.T) {
	if got := routerSlug(adapters.Router{Name: "AP 1"}); got != "AP-1" {
		t.Fatalf("slug = %q, want AP-1", got)
	}
}

func TestRouterSetKeyIsStable(t *testing.T) {
	ov := sampleOverview()
	k1 := routerSetKey(ov)
	// Reorder the routers: the key must not change.
	ov.Routers[0], ov.Routers[1] = ov.Routers[1], ov.Routers[0]
	if k2 := routerSetKey(ov); k2 != k1 {
		t.Fatalf("routerSetKey changed with order: %q vs %q", k1, k2)
	}
}

func TestHAEntities(t *testing.T) {
	ents := haEntities("home", "v1.2.3", sampleOverview())
	if len(ents) == 0 {
		t.Fatal("no discovery entities")
	}
	seen := map[string]bool{}
	var instanceEntities, routerEntities int
	for _, e := range ents {
		uid, _ := e.config["unique_id"].(string)
		if uid == "" {
			t.Fatalf("%s/%s: no unique_id", e.nodeID, e.objectID)
		}
		if seen[uid] {
			t.Fatalf("duplicate unique_id %q", uid)
		}
		seen[uid] = true
		if got, _ := e.config["availability_topic"].(string); got != availabilityTopic("home") {
			t.Fatalf("%s: availability_topic = %q", uid, got)
		}
		if e.nodeID == "netpulse_home" {
			instanceEntities++
		} else {
			routerEntities++
		}
	}
	if instanceEntities == 0 || routerEntities == 0 {
		t.Fatalf("expected instance and router entities, got %d/%d", instanceEntities, routerEntities)
	}
	// The per-router state topic must be the one we publish.
	for _, e := range ents {
		if e.nodeID == "netpulse_home_gw" && e.objectID == "cpu_usage" {
			if got, _ := e.config["state_topic"].(string); got != "netpulse/home/router/gw/state" {
				t.Fatalf("router state_topic = %q", got)
			}
			return
		}
	}
	t.Fatal("no cpu_usage entity for the gw router")
}

func TestStartIsNoopWhenDisabled(t *testing.T) {
	p := New(Config{}, "v1", func() *adapters.Overview { return nil }, false)
	if p.Enabled() {
		t.Fatal("publisher must be disabled")
	}
	p.Start(context.Background()) // must not panic nor block
}

func TestHAEntitiesSelfExposeMarkedForRemoval(t *testing.T) {
	ov := sampleOverview()
	self := true
	ov.Routers[1].SelfExpose = &self // ap-1 se expone solo

	ents := haEntities("home", "v1.2.3", ov)
	var removed, kept int
	for _, e := range ents {
		if e.nodeID == "netpulse_home_ap-1" {
			if !e.remove {
				t.Fatalf("ap-1 %s must be marked for removal", e.objectID)
			}
			removed++
			continue
		}
		if e.remove {
			t.Fatalf("%s/%s must not be marked for removal", e.nodeID, e.objectID)
		}
		kept++
	}
	if removed != 6 || kept != 11 {
		t.Fatalf("removed=%d kept=%d, want 6/11", removed, kept)
	}

	// La marca forma parte de la clave de conjunto: un cambio republica discovery.
	if routerSetKey(ov) == routerSetKey(sampleOverview()) {
		t.Fatal("routerSetKey must change when a router starts self-exposing")
	}
}
