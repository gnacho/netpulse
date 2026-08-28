package adapters

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/portseries"
)

func TestComputePortHealth_LinkDown(t *testing.T) {
	port := EthPort{ID: "lan1", Label: "LAN 1", Up: false}
	health := ComputePortHealth(port, nil, 0)
	if health.Score != 0 {
		t.Errorf("expected score 0 for down port, got %d", health.Score)
	}
	if len(health.Breakdown) != 1 {
		t.Fatalf("expected 1 breakdown item, got %d", len(health.Breakdown))
	}
	if health.Breakdown[0].Signal != "link_down" {
		t.Errorf("expected signal link_down, got %s", health.Breakdown[0].Signal)
	}
}

func TestComputePortHealth_Perfect(t *testing.T) {
	port := EthPort{
		ID:     "lan1",
		Label:  "LAN 1",
		Up:     true,
		Speed:  "1 Gbps",
		RxBps:  1000,
		TxBps:  1000,
		RxErrs: 0,
		TxErrs: 0,
	}
	health := ComputePortHealth(port, nil, 0)
	if health.Score != 100 {
		t.Errorf("expected score 100 for perfect port, got %d", health.Score)
	}
	if len(health.Breakdown) != 4 {
		t.Fatalf("expected 4 breakdown items, got %d", len(health.Breakdown))
	}
}

func TestComputePortHealth_HighUtilization(t *testing.T) {
	port := EthPort{
		ID:    "lan1",
		Label: "LAN 1",
		Up:    true,
		Speed: "1 Gbps",
		RxBps: 950e6,
		TxBps: 100e6,
	}
	health := ComputePortHealth(port, nil, 0)
	if health.Score >= 90 {
		t.Errorf("expected score <90 for 95%% util, got %d", health.Score)
	}
	found := false
	for _, item := range health.Breakdown {
		if item.Signal == "utilization" && item.Status != "ok" {
			found = true
		}
	}
	if !found {
		t.Error("expected utilization signal to be warn/crit")
	}
}

func TestComputePortHealth_Errors(t *testing.T) {
	now := time.Now()
	series := []portseries.PortPoint{
		{TS: now.Add(-10 * time.Second), RxErrors: 0, TxErrors: 0},
		{TS: now, RxErrors: 100, TxErrors: 100},
	}
	port := EthPort{ID: "lan1", Label: "LAN 1", Up: true, Speed: "1 Gbps"}
	health := ComputePortHealth(port, series, 0)
	if health.Score >= 90 {
		t.Errorf("expected score <90 for high error rate, got %d", health.Score)
	}
	found := false
	for _, item := range health.Breakdown {
		if item.Signal == "errors" && item.Status == "crit" {
			found = true
		}
	}
	if !found {
		t.Error("expected errors signal to be crit")
	}
}

func TestComputePortHealth_Flapping(t *testing.T) {
	port := EthPort{ID: "lan1", Label: "LAN 1", Up: true, Speed: "1 Gbps"}
	health := ComputePortHealth(port, nil, 15)
	if health.Score >= 85 {
		t.Errorf("expected score <85 for 15 flaps, got %d", health.Score)
	}
	found := false
	for _, item := range health.Breakdown {
		if item.Signal == "flapping" && item.Status == "crit" {
			found = true
		}
	}
	if !found {
		t.Error("expected flapping signal to be crit")
	}
}

func TestParseSpeedMbps(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1 Gbps", 1000},
		{"100 Mbps", 100},
		{"2.5 Gbps", 2500},
		{"10 Mbps", 10},
		{"", 0},
		{"invalid", 0},
	}
	for _, tc := range tests {
		got := parseSpeedMbps(tc.input)
		if got != tc.want {
			t.Errorf("parseSpeedMbps(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestComputeUtilization(t *testing.T) {
	port := EthPort{Speed: "1 Gbps", RxBps: 500e6, TxBps: 100e6}
	util := computeUtilization(port, nil)
	if util < 49 || util > 51 {
		t.Errorf("expected ~50%% util, got %.1f%%", util)
	}

	port2 := EthPort{Speed: "1 Gbps", RxBps: 0, TxBps: 0}
	util2 := computeUtilization(port2, nil)
	if util2 != 0 {
		t.Errorf("expected 0%% util, got %.1f%%", util2)
	}
}

func TestComputeErrorRate(t *testing.T) {
	now := time.Now()
	series := []portseries.PortPoint{
		{TS: now.Add(-10 * time.Second), RxErrors: 0, TxErrors: 0},
		{TS: now, RxErrors: 30, TxErrors: 20},
	}
	rate := computeErrorRate(series)
	if rate < 4.9 || rate > 5.1 {
		t.Errorf("expected ~5 errors/sec, got %.2f", rate)
	}

	rate2 := computeErrorRate(nil)
	if rate2 != 0 {
		t.Errorf("expected 0 for empty series, got %.2f", rate2)
	}
}
