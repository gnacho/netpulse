package mqttpub

import (
	"strconv"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// mapKV es un kvStore en memoria para tests.
type mapKV struct{ m map[string]string }

func (k mapKV) Get(key string) (string, bool) {
	v, ok := k.m[key]
	return v, ok
}
func (k mapKV) Set(key, value string) error {
	k.m[key] = value
	return nil
}

func TestLoadConfigEnvBaseAndKVOverride(t *testing.T) {
	kv := mapKV{m: map[string]string{
		kvKeyEnabled:  "true",
		kvKeyHost:     "10.0.0.10",
		kvKeyPort:     "1884",
		kvKeyInstance: "casa",
		kvKeyInterval: "45",
	}}
	cfg := LoadConfig(kv)
	if !cfg.Enabled || cfg.Host != "10.0.0.10" || cfg.Port != 1884 {
		t.Fatalf("kv override not applied: %+v", cfg)
	}
	if cfg.Instance != "casa" || cfg.Interval != 45*time.Second {
		t.Fatalf("instance/interval = %q/%v", cfg.Instance, cfg.Interval)
	}
	// Sin kv: el entorno manda (en los tests, los defaults: desactivado).
	cfg = LoadConfig(mapKV{m: map[string]string{}})
	if cfg.Enabled || cfg.Port != defaultPort || cfg.Instance != "default" {
		t.Fatalf("env base not used: %+v", cfg)
	}
}

func TestSaveConfigValidatesAndKeepsPassword(t *testing.T) {
	kv := mapKV{m: map[string]string{kvKeyPass: "secreto"}}
	cfg := Config{Enabled: true, Host: "10.0.0.10", Port: 1883, Interval: 30 * time.Second}
	if err := SaveConfig(kv, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if got := kv.m[kvKeyPass]; got != "secreto" {
		t.Fatalf("password must be kept, got %q", got)
	}
	if kv.m[kvKeyInstance] != "default" {
		t.Fatalf("instance default not applied: %q", kv.m[kvKeyInstance])
	}
	// Habilitado sin host: error.
	if err := SaveConfig(kv, Config{Enabled: true, Port: 1883}); err == nil {
		t.Fatal("enabled without host must fail")
	}
	// Puerto inválido: error.
	if err := SaveConfig(kv, Config{Port: 70000}); err == nil {
		t.Fatal("invalid port must fail")
	}
	// Redondo completo: todo lo guardado se lee igual.
	kv2 := mapKV{m: map[string]string{}}
	want := Config{Enabled: true, Host: "10.0.0.10", Port: 1884, User: "u", Pass: "p", Instance: "ofi", Interval: 15 * time.Second}
	if err := SaveConfig(kv2, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got := LoadConfig(kv2)
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestManagerLifecycle(t *testing.T) {
	disabled := Config{Enabled: false, Port: defaultPort, Interval: defaultInterval}
	snap := func() *adapters.Overview { return nil }
	m := NewManager(disabled, "v1", snap, false)
	if m.Running() {
		t.Fatal("must not start before Start")
	}
	m.Start()
	if m.Running() {
		t.Fatal("disabled config must not run")
	}
	// Apply con config activa: arranca (el broker es inalcanzable, da igual).
	on := Config{Enabled: true, Host: "127.0.0.1", Port: 1, Interval: defaultInterval, Instance: "default"}
	m.Apply(on)
	if !m.Running() {
		t.Fatal("Apply with an enabled config must start the publisher")
	}
	// Apply con la misma config: no-op, sigue corriendo.
	m.Apply(on)
	if !m.Running() {
		t.Fatal("Apply with the same config must keep it running")
	}
	if m.Config() != on {
		t.Fatalf("Config() = %+v", m.Config())
	}
}

func TestConfigIntervalSecondsRoundTrip(t *testing.T) {
	kv := mapKV{m: map[string]string{}}
	cfg := Config{Interval: 90 * time.Second}
	if err := SaveConfig(kv, cfg); err != nil {
		t.Fatal(err)
	}
	if got := kv.m[kvKeyInterval]; got != strconv.Itoa(90) {
		t.Fatalf("interval seconds = %q, want 90", got)
	}
}
