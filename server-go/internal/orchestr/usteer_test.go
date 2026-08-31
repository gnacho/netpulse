package orchestr

import (
	"strings"
	"testing"

	"github.com/gnacho/netpulse/agent/executor"
)

const usteerProbeApkOK = `===PKG_MGR===
apk
===USTEER_INST===
yes
===USTEER_RUN===
yes
===WPAD===
wpad-mbedtls-2025.05.11~5.5.11-1 aarch64_cortex-a53 {wpad} () installed
===HOSTAPD_DIR===
yes
===USTEER_UCI===
usteer.@usteer[0]=usteer
usteer.@usteer[0].enabled='1'
usteer.@usteer[0].network='lan'
usteer.@usteer[0].syslog='1'
usteer.@usteer[0].debug_level='2'
usteer.@usteer[0].aggressiveness='3'
usteer.@usteer[0].band_steering_threshold='5'
usteer.@usteer[0].load_balancing_threshold='0'
usteer.@usteer[0].min_connect_snr='0'
usteer.@usteer[0].min_snr='0'
usteer.@usteer[0].roam_trigger_snr='0'
usteer.@usteer[0].roam_scan_snr='0'
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

const usteerProbeOpkgMissing = `===PKG_MGR===
opkg
===USTEER_INST===
no
===USTEER_RUN===
no
===WPAD===
wpad-basic - 2024.03.09~695a0ff9-1 - wpad-basic
===HOSTAPD_DIR===
no
===USTEER_UCI===
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

func TestParseUsteerApkOK(t *testing.T) {
	sc := parseUsteer(usteerProbeApkOK)
	if sc.Manager != "apk" {
		t.Errorf("Manager: got %q want apk", sc.Manager)
	}
	if !sc.UsteerInstalled {
		t.Error("UsteerInstalled: esperaba true")
	}
	if !sc.UsteerRunning {
		t.Error("UsteerRunning: esperaba true")
	}
	if sc.WpadVariant != "mbedtls" {
		t.Errorf("WpadVariant: got %q want mbedtls", sc.WpadVariant)
	}
	if !sc.HostapdDirOK {
		t.Error("HostapdDirOK: esperaba true")
	}
	if sc.Config["aggressiveness"] != "3" {
		t.Errorf("aggressiveness: got %q want 3", sc.Config["aggressiveness"])
	}
	if sc.Config["band_steering_threshold"] != "5" {
		t.Errorf("band_steering_threshold: got %q want 5", sc.Config["band_steering_threshold"])
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

func TestParseUsteerOpkgMissing(t *testing.T) {
	sc := parseUsteer(usteerProbeOpkgMissing)
	if sc.Manager != "opkg" {
		t.Errorf("Manager: got %q want opkg", sc.Manager)
	}
	if sc.UsteerInstalled {
		t.Error("UsteerInstalled: esperaba false")
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

func TestUsteerOpsInstallAndConfigure(t *testing.T) {
	sc := parseUsteer(usteerProbeOpkgMissing)
	ops := UsteerOps(UsteerDesired{
		Enabled: true, SSID: "temiscira", MobilityDomain: "2025",
	}, sc)
	if err := validateUsteerOps(ops); err != nil {
		t.Fatalf("ops no validan: %v", err)
	}
	kinds := usteerKindsOf(ops)
	if !strings.Contains(kinds, "install") {
		t.Error("falta install de usteer")
	}
	if !strings.Contains(kinds, "uci_set_named") {
		t.Error("falta uci_set_named para la sección usteer")
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

func TestUsteerOpsNoOpWhenMatches(t *testing.T) {
	sc := parseUsteer(usteerProbeApkOK)
	ops := UsteerOps(UsteerDesired{
		Enabled: true, SSID: "temiscira", MobilityDomain: "2025",
	}, sc)
	if len(ops) != 0 {
		t.Errorf("esperaba 0 ops cuando todo coincide, got %d: %v", len(ops), ops)
	}
}

func TestUsteerOpsDisable(t *testing.T) {
	sc := parseUsteer(usteerProbeApkOK)
	ops := UsteerOps(UsteerDesired{Enabled: false}, sc)
	if err := validateUsteerOps(ops); err != nil {
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

func TestUsteerDriftWarnings(t *testing.T) {
	sc := parseUsteer(usteerProbeOpkgMissing)
	warns := UsteerDriftWarnings(UsteerDesired{Enabled: true, SSID: "temiscira", MobilityDomain: "2025"}, sc)
	joined := strings.Join(warns, ",")
	if !strings.Contains(joined, "usteer no esta instalado") {
		t.Error("falta advertencia de usteer no instalado")
	}
	if !strings.Contains(joined, "random_bssid activo") {
		t.Error("falta advertencia de random_bssid")
	}
	if !strings.Contains(joined, "802.11k desactivado") {
		t.Error("falta advertencia de 802.11k")
	}
}

func TestUsteerMethod(t *testing.T) {
	if UsteerMethod(UsteerScenario{UsteerRunning: true}) != "active" {
		t.Error("method active")
	}
	if UsteerMethod(UsteerScenario{}) != "inactive" {
		t.Error("method inactive")
	}
}

func usteerKindsOf(ops []executor.Op) string {
	out := make([]string, len(ops))
	for i, o := range ops {
		out[i] = o.Kind
	}
	return strings.Join(out, ",")
}
