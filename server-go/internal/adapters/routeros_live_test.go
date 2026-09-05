package adapters

import "testing"

func TestParseRouterOSUptime(t *testing.T) {
	cases := map[string]float64{
		"5s":         5,
		"4m5s":       245,
		"3h4m5s":     11045,
		"2d3h4m5s":   183845,
		"1w2d3h4m5s": 788645,
	}
	for input, want := range cases {
		got, err := parseRouterOSUptime(input)
		if err != nil {
			t.Fatalf("parseRouterOSUptime(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("parseRouterOSUptime(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestParseRouterOSUptimeInvalid(t *testing.T) {
	if _, err := parseRouterOSUptime("not-a-duration"); err == nil {
		t.Fatal("expected error for unparseable uptime")
	}
}

func TestBuildRouterOSPoll(t *testing.T) {
	cfg := RouterConfig{ID: "core-router", Type: "routeros"}
	res := routerOSResource{
		CPULoad:      "3",
		FreeMemory:   "33554432",  // 32 MiB free
		TotalMemory:  "134217728", // 128 MiB total → 75% used
		Uptime:       "1d02h03m04s",
		Version:      "7.15",
		BoardName:    "RB4011",
		Architecture: "arm",
	}
	id := routerOSIdentity{Name: "MikroTik-Core"}

	p, err := buildRouterOSPoll(cfg, res, id)
	if err != nil {
		t.Fatalf("buildRouterOSPoll: %v", err)
	}
	if p.cpu != 3 {
		t.Errorf("cpu = %d, want 3", p.cpu)
	}
	if p.ram != 75 {
		t.Errorf("ram = %d, want 75", p.ram)
	}
	if p.board == nil || p.board.Hostname != "MikroTik-Core" || p.board.Release.Version != "7.15" {
		t.Errorf("board = %+v", p.board)
	}
	// noVitals no debe activarse para routeros: buildRouter solo lo hace por
	// SnmpEnabled o agentKind=="external" (#441), ninguno aplica aquí.
	if p.cfg.SnmpEnabled {
		t.Error("routeros config should not have SnmpEnabled set")
	}
}

func TestBuildRouterOSPollBadCPU(t *testing.T) {
	res := routerOSResource{CPULoad: "n/a", Uptime: "1s"}
	if _, err := buildRouterOSPoll(RouterConfig{ID: "r1"}, res, routerOSIdentity{}); err == nil {
		t.Fatal("expected error for bad cpu-load")
	}
}
