package orchestr

import (
	"strings"
	"testing"
)

// Fixture real de rt2 (Redmi AX6, OpenWrt stock) — config tras crear la guest.
const guestWiFiOut = `===WIRELESS===
wireless.radio0=wifi-device
wireless.radio0.type='mac80211'
wireless.radio0.band='2g'
wireless.radio0.channel='6'
wireless.radio0.htmode='HE20'
wireless.radio0.disabled='0'
wireless.radio1=wifi-device
wireless.radio1.type='mac80211'
wireless.radio1.band='5g'
wireless.radio1.channel='36'
wireless.radio1.htmode='HE80'
wireless.radio1.disabled='0'
wireless.default_radio0=wifi-iface
wireless.default_radio0.device='radio0'
wireless.default_radio0.mode='ap'
wireless.default_radio0.ssid='Home'
wireless.default_radio0.network='lan'
wireless.wifinet2=wifi-iface
wireless.wifinet2.device='radio1'
wireless.wifinet2.mode='ap'
wireless.wifinet2.ssid='Home-5G'
wireless.wifinet2.network='lan'
wireless.guestwifi=wifi-iface
wireless.guestwifi.device='radio0'
wireless.guestwifi.mode='ap'
wireless.guestwifi.ssid='NetPulse-Guest'
wireless.guestwifi.network='guest'
wireless.guestwifi.isolate='1'
===NETWORK===
network.lan=interface
network.lan.device='br-lan'
network.lan.proto='dhcp'
network.guest=interface
network.guest.proto='static'
network.guest.ipaddr='192.168.8.1'
network.guest.netmask='255.255.255.0'
===FIREWALL===
firewall.@zone[0]=zone
firewall.@zone[0].name='lan'
firewall.@zone[0].input='ACCEPT'
firewall.@zone[1]=zone
firewall.@zone[1].name='wan'
firewall.@zone[1].input='REJECT'
firewall.@zone[2]=zone
firewall.@zone[2].name='guest'
firewall.@zone[2].network='guest'
firewall.@zone[2].input='REJECT'
firewall.@forwarding[0]=forwarding
firewall.@forwarding[0].src='lan'
firewall.@forwarding[0].dest='wan'
firewall.@forwarding[1]=forwarding
firewall.@forwarding[1].src='guest'
firewall.@forwarding[1].dest='wan'
===END===`

// TestParseGuestWiFiRadiosYGuest: detecta radios, iface guest y secciones.
func TestParseGuestWiFiRadiosYGuest(t *testing.T) {
	sc := parseGuestWiFi(guestWiFiOut)
	if sc.Radio2G != "radio0" {
		t.Errorf("Radio2G: got %q want radio0", sc.Radio2G)
	}
	if sc.Radio5G != "radio1" {
		t.Errorf("Radio5G: got %q want radio1", sc.Radio5G)
	}
	if !sc.GuestPresent {
		t.Error("GuestPresent: esperaba true (existe iface con NetPulse-Guest)")
	}
	if sc.GuestIfaceIdx != "@wifi-iface[2]" {
		t.Errorf("GuestIfaceIdx: got %q want @wifi-iface[2]", sc.GuestIfaceIdx)
	}
	if sc.GuestZoneIdx != "@zone[2]" {
		t.Errorf("GuestZoneIdx: got %q want @zone[2]", sc.GuestZoneIdx)
	}
	if sc.GuestFwdIdx != "@forwarding[1]" {
		t.Errorf("GuestFwdIdx: got %q want @forwarding[1]", sc.GuestFwdIdx)
	}
}

// TestParseGuestWiFiLimpio: sin guest → todas las referencias vacías.
func TestParseGuestWiFiLimpio(t *testing.T) {
	out := strings.Replace(guestWiFiOut, "wireless.guestwifi.ssid='NetPulse-Guest'", "wireless.guestwifi.ssid='Other'", 1)
	// Sin zone guest ni forwarding guest (router sin guest network).
	out = strings.Replace(out, "firewall.@zone[2].name='guest'\nfirewall.@zone[2].network='guest'\nfirewall.@zone[2].input='REJECT'\n", "", 1)
	out = strings.Replace(out, "firewall.@forwarding[1]=forwarding\nfirewall.@forwarding[1].src='guest'\nfirewall.@forwarding[1].dest='wan'\n", "", 1)
	sc := parseGuestWiFi(out)
	if sc.GuestPresent {
		t.Error("GuestPresent: esperaba false sin ssid NetPulse-Guest")
	}
	if sc.GuestIfaceIdx != "" {
		t.Errorf("GuestIfaceIdx: got %q want vacío", sc.GuestIfaceIdx)
	}
	if sc.GuestZoneIdx != "" || sc.GuestFwdIdx != "" {
		t.Errorf("indices zone/fwd no vacíos sin guest: %q %q", sc.GuestZoneIdx, sc.GuestFwdIdx)
	}
}

// TestGuestWiFiOpsEnable: secuencia enable + todas las ops validan en el
// executor (contrato orchestr→executor garantizado).
func TestGuestWiFiOpsEnable(t *testing.T) {
	sc := parseGuestWiFi(guestWiFiOut)
	ops := GuestWiFiOps(GuestWiFiDesired{Enabled: true, SSID: "NetPulse-Guest", Password: "supersecret123", Band: "2g"}, sc)
	if len(ops) == 0 {
		t.Fatal("enable generó 0 ops")
	}
	if err := validateGuestWiFiOps(ops); err != nil {
		t.Fatalf("ops enable no validan en executor: %v", err)
	}
	// Contiene uci_add wireless wifi-iface + uci_set network.guest + zone + reload
	found := map[string]bool{}
	for _, o := range ops {
		found[o.Kind] = true
	}
	for _, k := range []string{"uci_add", "uci_set", "uci_commit", "uci_set_named", "service"} {
		if !found[k] {
			t.Errorf("enable sin kind %q", k)
		}
	}
}

// TestGuestWiFiOpsDisable: disable revierte con los índices detectados.
func TestGuestWiFiOpsDisable(t *testing.T) {
	sc := parseGuestWiFi(guestWiFiOut)
	ops := GuestWiFiOps(GuestWiFiDesired{Enabled: false}, sc)
	if err := validateGuestWiFiOps(ops); err != nil {
		t.Fatalf("ops disable no validan en executor: %v", err)
	}
	// Debe borrar la wifi-iface guest, la network guest, la zone y el fwd.
	del := map[string]bool{}
	for _, o := range ops {
		if o.Kind == "uci_delete_section" {
			del[o.Args["section"]] = true
		}
	}
	if !del["@wifi-iface[2]"] {
		t.Error("disable sin uci_delete_section de la wifi-iface guest")
	}
	if !del["guest"] {
		t.Error("disable sin uci_delete_section de la network guest")
	}
	if !del["@zone[2]"] {
		t.Error("disable sin uci_delete_section de la zone guest")
	}
	if !del["@forwarding[1]"] {
		t.Error("disable sin uci_delete_section del forwarding guest")
	}
}
