package adapters

import (
	"testing"
)

func TestClassifyRoamingDaemon(t *testing.T) {
	cases := []struct {
		name     string
		usteer   bool
		dawn     bool
		expected RoamingDaemon
	}{
		{"none", false, false, RoamingDaemonNone},
		{"usteer", true, false, RoamingDaemonUsteer},
		{"dawn", false, true, RoamingDaemonDawn},
		{"both", true, true, RoamingDaemonBoth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyRoamingDaemon(tc.usteer, tc.dawn)
			if got != tc.expected {
				t.Errorf("classifyRoamingDaemon(%v, %v) = %q, want %q", tc.usteer, tc.dawn, got, tc.expected)
			}
		})
	}
}

func TestBuildRoamingAnomalies(t *testing.T) {
	t.Run("sin anomalias", func(t *testing.T) {
		got := buildRoamingAnomalies(map[string][]dot11rIfaceRef{
			"temiscira": {
				{routerID: "r1", iface: Dot11rIface{SSID: "temiscira", Dot11REnabled: true, MobilityDomain: "2025", FTOverDS: false}},
				{routerID: "r2", iface: Dot11rIface{SSID: "temiscira", Dot11REnabled: true, MobilityDomain: "2025", FTOverDS: false}},
			},
		})
		if len(got) != 0 {
			t.Fatalf("esperaba 0 anomalias, got %d: %+v", len(got), got)
		}
	})

	t.Run("mobility domain distinto", func(t *testing.T) {
		got := buildRoamingAnomalies(map[string][]dot11rIfaceRef{
			"temiscira": {
				{routerID: "r1", iface: Dot11rIface{SSID: "temiscira", Dot11REnabled: true, MobilityDomain: "2025", FTOverDS: false}},
				{routerID: "r2", iface: Dot11rIface{SSID: "temiscira", Dot11REnabled: true, MobilityDomain: "2026", FTOverDS: false}},
			},
		})
		if len(got) != 1 || got[0].Kind != "mobility_domain_mismatch" {
			t.Fatalf("esperaba mobility_domain_mismatch, got %+v", got)
		}
	})

	t.Run("ft mode mixto", func(t *testing.T) {
		got := buildRoamingAnomalies(map[string][]dot11rIfaceRef{
			"temiscira": {
				{routerID: "r1", iface: Dot11rIface{SSID: "temiscira", Dot11REnabled: true, MobilityDomain: "2025", FTOverDS: true}},
				{routerID: "r2", iface: Dot11rIface{SSID: "temiscira", Dot11REnabled: true, MobilityDomain: "2025", FTOverDS: false}},
			},
		})
		if len(got) != 1 || got[0].Kind != "ft_mode_mismatch" {
			t.Fatalf("esperaba ft_mode_mismatch, got %+v", got)
		}
	})

	t.Run("802.11r parcial", func(t *testing.T) {
		got := buildRoamingAnomalies(map[string][]dot11rIfaceRef{
			"temiscira": {
				{routerID: "r1", iface: Dot11rIface{SSID: "temiscira", Dot11REnabled: true, MobilityDomain: "2025", FTOverDS: false}},
				{routerID: "r2", iface: Dot11rIface{SSID: "temiscira", Dot11REnabled: false, MobilityDomain: "", FTOverDS: false}},
			},
		})
		if len(got) != 1 || got[0].Kind != "partial_11r" {
			t.Fatalf("esperaba partial_11r, got %+v", got)
		}
	})

	t.Run("varias anomalias", func(t *testing.T) {
		got := buildRoamingAnomalies(map[string][]dot11rIfaceRef{
			"temiscira": {
				{routerID: "r1", iface: Dot11rIface{SSID: "temiscira", Dot11REnabled: true, MobilityDomain: "2025", FTOverDS: true}},
				{routerID: "r2", iface: Dot11rIface{SSID: "temiscira", Dot11REnabled: true, MobilityDomain: "2026", FTOverDS: false}},
			},
		})
		if len(got) != 2 {
			t.Fatalf("esperaba 2 anomalias, got %d: %+v", len(got), got)
		}
	})
}
