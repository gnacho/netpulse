package orchestr

import (
	"strings"
	"testing"

	"github.com/gnacho/netpulse/agent/executor"
)

const dawnProbeApkOK = `===PKG_MGR===
apk
===DAWN_INST===
yes
===DAWN_RUN===
yes
===WPAD===
wpad-mbedtls-2025.05.11~5.5.11-1 aarch64_cortex-a53 {wpad} () installed
===HOSTAPD_DIR===
yes
===DAWN_UCI===
dawn.global=global
dawn.global.kicking='3'
dawn.global.kicking_threshold='20'
dawn.global.duration='150'
dawn.global.min_number_to_kick='3'
dawn.global.bandwidth_threshold='6'
dawn.global.eval_auth_req='0'
dawn.global.eval_assoc_req='0'
dawn.global.eval_probe_req='0'
dawn.global.network_option='2'
dawn.global.hostapd_dir='/var/run/hostapd'
dawn.global.set_hostapd_nr='1'
dawn.global.rrm_mode='pat'
dawn.global.broadcast_ip='192.168.1.255'
dawn.global.tcp_port='1026'
dawn.global.shared_key='TemisciraDawn2026'
dawn.global.iv='TemisciraDawn2026'
===WIRELESS_UCI===
wireless.temiscira2g=wifi-iface
wireless.temiscira2g.device='radio0'
wireless.temiscira2g.mode='ap'
wireless.temiscira2g.ssid='temiscira'
wireless.temiscira2g.ieee80211k='1'
wireless.temiscira2g.ieee80211r='1'
wireless.temiscira2g.ieee80211v='1'
wireless.temiscira2g.bss_transition='1'
wireless.temiscira2g.ft_over_ds='0'
wireless.temiscira2g.ft_psk_generate_local='1'
wireless.temiscira2g.mobility_domain='2025'
wireless.radio0=wifi-device
wireless.radio0.random_bssid='0'
===END===`

const dawnProbeOpkgMissing = `===PKG_MGR===
opkg
===DAWN_INST===
no
===DAWN_RUN===
no
===WPAD===
wpad-basic - 2024.03.09~695a0ff9-1 - wpad-basic
===HOSTAPD_DIR===
no
===DAWN_UCI===
===WIRELESS_UCI===
wireless.temiscira2g=wifi-iface
wireless.temiscira2g.device='radio0'
wireless.temiscira2g.mode='ap'
wireless.temiscira2g.ssid='temiscira'
wireless.temiscira2g.ieee80211k='0'
wireless.temiscira2g.ieee80211r='0'
wireless.temiscira2g.ieee80211v='0'
wireless.temiscira2g.mobility_domain='2021'
wireless.radio0=wifi-device
wireless.radio0.random_bssid='1'
===END===`

func TestParseDawnApkOK(t *testing.T) {
	sc := parseDawn(dawnProbeApkOK)
	if sc.Manager != "apk" {
		t.Errorf("Manager: got %q want apk", sc.Manager)
	}
	if !sc.DawnInstalled {
		t.Error("DawnInstalled: esperaba true")
	}
	if !sc.DawnRunning {
		t.Error("DawnRunning: esperaba true")
	}
	if sc.WpadVariant != "mbedtls" {
		t.Errorf("WpadVariant: got %q want mbedtls", sc.WpadVariant)
	}
	if !sc.HostapdDirOK {
		t.Error("HostapdDirOK: esperaba true")
	}
	if sc.Global["kicking"] != "3" {
		t.Errorf("kicking: got %q want 3", sc.Global["kicking"])
	}
	if len(sc.SSIDs) != 1 {
		t.Fatalf("SSIDs: got %d want 1", len(sc.SSIDs))
	}
	if sc.SSIDs[0].MobilityDomain != "2025" {
		t.Errorf("mobility_domain: got %q want 2025", sc.SSIDs[0].MobilityDomain)
	}
	if len(sc.Radios) != 1 {
		t.Fatalf("Radios: got %d want 1", len(sc.Radios))
	}
	if sc.Radios[0].RandomBSSID != "0" {
		t.Errorf("random_bssid: got %q want 0", sc.Radios[0].RandomBSSID)
	}
}

func TestParseDawnOpkgMissing(t *testing.T) {
	sc := parseDawn(dawnProbeOpkgMissing)
	if sc.Manager != "opkg" {
		t.Errorf("Manager: got %q want opkg", sc.Manager)
	}
	if sc.DawnInstalled {
		t.Error("DawnInstalled: esperaba false")
	}
	if sc.WpadVariant != "basic" {
		t.Errorf("WpadVariant: got %q want basic", sc.WpadVariant)
	}
	if len(sc.SSIDs) != 1 || sc.SSIDs[0].IEEE80211K != "0" {
		t.Error("esperaba 802.11k desactivado")
	}
	if sc.Radios[0].RandomBSSID != "1" {
		t.Error("esperaba random_bssid=1")
	}
}

func TestDawnOpsInstallAndConfigure(t *testing.T) {
	sc := parseDawn(dawnProbeOpkgMissing)
	ops := DawnOps(DawnDesired{
		Enabled: true, SSID: "temiscira", MobilityDomain: "2025",
		BroadcastIP: "192.168.1.255", SharedKey: "TemisciraDawn2026", IV: "TemisciraDawn2026",
	}, sc)
	if err := validateDawnOps(ops); err != nil {
		t.Fatalf("ops no validan: %v", err)
	}
	kinds := dawnKindsOf(ops)
	if !strings.Contains(kinds, "install") {
		t.Error("falta install de dawn")
	}
	if !strings.Contains(kinds, "uci_set_named") {
		t.Error("falta uci_set_named para dawn.global")
	}
	if !strings.Contains(kinds, "uci_set") {
		t.Error("falta uci_set")
	}
	if !strings.Contains(kinds, "uci_commit") {
		t.Error("falta uci_commit")
	}
	if !strings.Contains(kinds, "service") {
		t.Error("falta service start/enable")
	}
	// Debe forzar wpad completo en opkg.
	foundWpad := false
	for _, o := range ops {
		if o.Kind == "install" && o.Args["package"] == "wpad-wolfssl" {
			foundWpad = true
		}
	}
	if !foundWpad {
		t.Error("falta install de wpad-wolfssl al tener wpad-basic")
	}
	// Debe activar 802.11k/r/v.
	found80211r := false
	for _, o := range ops {
		if o.Kind == "uci_set" && o.Args["config"] == "wireless" && o.Args["option"] == "ieee80211r" && o.Args["value"] == "1" {
			found80211r = true
		}
	}
	if !found80211r {
		t.Error("falta uci_set ieee80211r=1")
	}
	// Debe poner random_bssid=0.
	foundRandom := false
	for _, o := range ops {
		if o.Kind == "uci_set" && o.Args["option"] == "random_bssid" && o.Args["value"] == "0" {
			foundRandom = true
		}
	}
	if !foundRandom {
		t.Error("falta uci_set random_bssid=0")
	}
}

func TestDawnOpsNoOpWhenMatches(t *testing.T) {
	sc := parseDawn(dawnProbeApkOK)
	ops := DawnOps(DawnDesired{
		Enabled: true, SSID: "temiscira", MobilityDomain: "2025",
		BroadcastIP: "192.168.1.255", SharedKey: "TemisciraDawn2026", IV: "TemisciraDawn2026",
	}, sc)
	if len(ops) != 0 {
		t.Errorf("esperaba 0 ops cuando todo coincide, got %d: %v", len(ops), ops)
	}
}

func TestDawnOpsDisable(t *testing.T) {
	sc := parseDawn(dawnProbeApkOK)
	ops := DawnOps(DawnDesired{Enabled: false}, sc)
	if err := validateDawnOps(ops); err != nil {
		t.Fatalf("ops disable no validan: %v", err)
	}
	found := false
	for _, o := range ops {
		if o.Kind == "uci_set" && o.Args["option"] == "enabled" && o.Args["value"] == "0" {
			found = true
		}
	}
	if !found {
		t.Error("disable no pone enabled=0")
	}
}

func TestDawnDriftWarnings(t *testing.T) {
	sc := parseDawn(dawnProbeOpkgMissing)
	warns := DawnDriftWarnings(DawnDesired{Enabled: true, SSID: "temiscira", MobilityDomain: "2025"}, sc)
	joined := strings.Join(warns, ",")
	if !strings.Contains(joined, "DAWN no está instalado") {
		t.Error("falta advertencia de DAWN no instalado")
	}
	if !strings.Contains(joined, "random_bssid activo") {
		t.Error("falta advertencia de random_bssid")
	}
	if !strings.Contains(joined, "802.11k desactivado") {
		t.Error("falta advertencia de 802.11k")
	}
}

func TestDawnMethod(t *testing.T) {
	if DawnMethod(DawnScenario{DawnRunning: true}) != "active" {
		t.Error("method active")
	}
	if DawnMethod(DawnScenario{}) != "inactive" {
		t.Error("method inactive")
	}
}

func dawnKindsOf(ops []executor.Op) string {
	out := make([]string, len(ops))
	for i, o := range ops {
		out[i] = o.Kind
	}
	return strings.Join(out, ",")
}
