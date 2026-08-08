package adapters

import "testing"

// TestParseUciWirelessFlint2 usa la salida real de `uci show wireless` del
// Flint2 (gateway de Mandor) como fixture. Cubre:
//   - 2 wifi-device (radio0 2g/ch1, radio1 5g/ch36) mapeados a channel+band.
//   - 2 wifi-iface (wifi2g/wifi5g) con el mismo SSID y 802.11r activo.
//   - campos 802.11k/v/w (todos a 1) + mobility_domain=2025 + ft_over_ds=0.
//   - rrm_nr_list (campo lista multi-línea) que NO debe romper el parser.
func TestParseUciWirelessFlint2(t *testing.T) {
	out := `wireless.radio0=wifi-device
wireless.radio0.band='2g'
wireless.radio0.channel='1'
wireless.radio0.htmode='HE20'
wireless.radio0.disabled='0'
wireless.radio0.country='ES'
wireless.radio0.type='mac80211'
wireless.radio0.hwmode='11g'
wireless.wifi2g=wifi-iface
wireless.wifi2g.device='radio0'
wireless.wifi2g.network='lan'
wireless.wifi2g.mode='ap'
wireless.wifi2g.ssid='temiscira'
wireless.wifi2g.encryption='psk2'
wireless.wifi2g.key='secret'
wireless.wifi2g.wds='1'
wireless.wifi2g.ieee80211k='1'
wireless.wifi2g.ieee80211r='1'
wireless.wifi2g.mobility_domain='2025'
wireless.wifi2g.ft_over_ds='0'
wireless.wifi2g.ft_psk_generate_local='1'
wireless.wifi2g.pmk_r1_push='1'
wireless.wifi2g.ifname='wlan0'
wireless.wifi2g.macaddr='1E:C1:05:C9:48:0D'
wireless.wifi2g.ieee80211v='1'
wireless.wifi2g.bss_transition='1'
wireless.wifi2g.ieee80211w='1'
wireless.wifi2g.rrm_nr_list='8c:de:f9:33:71:58,temiscira,foo' '8c:de:f9:33:71:59,temiscira,bar'
wireless.radio1=wifi-device
wireless.radio1.band='5g'
wireless.radio1.channel='36'
wireless.radio1.htmode='HE80'
wireless.radio1.disabled='0'
wireless.radio1.type='mac80211'
wireless.wifi5g=wifi-iface
wireless.wifi5g.device='radio1'
wireless.wifi5g.network='lan'
wireless.wifi5g.mode='ap'
wireless.wifi5g.ssid='temiscira'
wireless.wifi5g.encryption='psk2'
wireless.wifi5g.key='secret'
wireless.wifi5g.ieee80211k='1'
wireless.wifi5g.ieee80211r='1'
wireless.wifi5g.mobility_domain='2025'
wireless.wifi5g.ft_over_ds='0'
wireless.wifi5g.ft_psk_generate_local='1'
wireless.wifi5g.ifname='wlan1'
wireless.wifi5g.macaddr='94:83:c4:ba:bf:ab'
wireless.wifi5g.ieee80211v='1'
wireless.wifi5g.bss_transition='1'
wireless.wifi5g.ieee80211w='1'
`
	ifaces := parseUciWireless(out)
	if len(ifaces) != 2 {
		t.Fatalf("expected 2 ifaces, got %d", len(ifaces))
	}

	// wifi2g → radio0 (2g/ch1).
	if ifaces[0].Section != "wifi2g" {
		t.Errorf("iface[0].Section = %q, want wifi2g", ifaces[0].Section)
	}
	if ifaces[0].Device != "radio0" {
		t.Errorf("iface[0].Device = %q, want radio0", ifaces[0].Device)
	}
	if ifaces[0].Band != "2.4 GHz" {
		t.Errorf("iface[0].Band = %q, want 2.4 GHz", ifaces[0].Band)
	}
	if ifaces[0].Channel != 1 {
		t.Errorf("iface[0].Channel = %d, want 1", ifaces[0].Channel)
	}
	if ifaces[0].SSID != "temiscira" {
		t.Errorf("iface[0].SSID = %q, want temiscira", ifaces[0].SSID)
	}
	if !ifaces[0].Dot11REnabled {
		t.Errorf("iface[0].Dot11REnabled = false, want true")
	}
	if ifaces[0].MobilityDomain != "2025" {
		t.Errorf("iface[0].MobilityDomain = %q, want 2025", ifaces[0].MobilityDomain)
	}
	if ifaces[0].FTOverDS {
		t.Errorf("iface[0].FTOverDS = true, want false (over-the-air)")
	}
	if !ifaces[0].FTPSKGenerateLocal {
		t.Errorf("iface[0].FTPSKGenerateLocal = false, want true")
	}
	if !ifaces[0].Dot11KEnabled || !ifaces[0].Dot11VEnabled || !ifaces[0].BSSTransition || !ifaces[0].MFP {
		t.Errorf("iface[0] k/v/w flags wrong: k=%v v=%v bss=%v mfp=%v",
			ifaces[0].Dot11KEnabled, ifaces[0].Dot11VEnabled, ifaces[0].BSSTransition, ifaces[0].MFP)
	}

	// wifi5g → radio1 (5g/ch36).
	if ifaces[1].Section != "wifi5g" {
		t.Errorf("iface[1].Section = %q, want wifi5g", ifaces[1].Section)
	}
	if ifaces[1].Band != "5 GHz" {
		t.Errorf("iface[1].Band = %q, want 5 GHz", ifaces[1].Band)
	}
	if ifaces[1].Channel != 36 {
		t.Errorf("iface[1].Channel = %d, want 36", ifaces[1].Channel)
	}
	if !ifaces[1].Dot11REnabled {
		t.Errorf("iface[1].Dot11REnabled = false, want true")
	}
}

// TestParseUciWirelessVacio cubre entradas degeneradas: el parser nunca debe
// panicar y siempre devuelve un slice (vacío si no hay ifaces).
func TestParseUciWirelessVacio(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"no-wireless":   "unrelated line\nanother\n",
		"only-devices":  "wireless.radio0=wifi-device\nwireless.radio0.band='2g'\n",
		"iface-sin-ssid": "wireless.guest=wifi-iface\nwireless.guest.device='radio0'\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got := parseUciWireless(in)
			// Salvo "iface-sin-ssid", todos deben devolver 0 ifaces.
			if name == "iface-sin-ssid" {
				if len(got) != 1 {
					t.Fatalf("expected 1 iface (sin ssid), got %d", len(got))
				}
				if got[0].SSID != "" {
					t.Errorf("SSID = %q, want empty", got[0].SSID)
				}
				return
			}
			if len(got) != 0 {
				t.Fatalf("expected 0 ifaces for %q, got %d: %+v", name, len(got), got)
			}
		})
	}
}

// TestParseUciWirelessSin11r verifica que un iface SIN ieee80211r marque
// Dot11REnabled=false y los campos FT queden en cero.
func TestParseUciWirelessSin11r(t *testing.T) {
	out := `wireless.radio0=wifi-device
wireless.radio0.band='2g'
wireless.radio0.channel='6'
wireless.default=wifi-iface
wireless.default.device='radio0'
wireless.default.ssid='guest'
wireless.default.encryption='psk2'
`
	ifaces := parseUciWireless(out)
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 iface, got %d", len(ifaces))
	}
	if ifaces[0].Dot11REnabled {
		t.Errorf("Dot11REnabled = true, want false")
	}
	if ifaces[0].MobilityDomain != "" {
		t.Errorf("MobilityDomain = %q, want empty", ifaces[0].MobilityDomain)
	}
	if ifaces[0].FTOverDS || ifaces[0].FTPSKGenerateLocal {
		t.Errorf("FT flags set without ieee80211r")
	}
}

// TestParseUciWirelessGuestIgualSSID cubre la agregación por SSID en
// GetDot11r: dos ifaces con el mismo SSID en distintos routers → un único
// Dot11rSSID con IfaceCount=2. Lo verificamos indirectamente via la lista de
// ifaces (la agregación vive en Live.GetDot11r que necesita SSH pool mock).
func TestParseUciWirelessOrdenAparicion(t *testing.T) {
	out := `wireless.radio0=wifi-device
wireless.radio0.band='2g'
wireless.radio0.channel='1'
wireless.wifi2g=wifi-iface
wireless.wifi2g.device='radio0'
wireless.wifi2g.ssid='home'
wireless.radio1=wifi-device
wireless.radio1.band='5g'
wireless.radio1.channel='36'
wireless.wifi5g=wifi-iface
wireless.wifi5g.device='radio1'
wireless.wifi5g.ssid='home'
wireless.iot=wifi-iface
wireless.iot.device='radio0'
wireless.iot.ssid='iot'
wireless.iot.network='iot'
`
	ifaces := parseUciWireless(out)
	if len(ifaces) != 3 {
		t.Fatalf("expected 3 ifaces, got %d", len(ifaces))
	}
	want := []string{"wifi2g", "wifi5g", "iot"}
	for i, w := range want {
		if ifaces[i].Section != w {
			t.Errorf("iface[%d].Section = %q, want %q (orden de aparición)", i, ifaces[i].Section, w)
		}
	}
}

// TestUnquoteUci cubre los casos de comilla simple y escape \'.
func TestUnquoteUci(t *testing.T) {
	cases := map[string]string{
		`'temiscira'`:    "temiscira",
		`'it\'s here'`:   "it's here",
		"plain":          "plain",
		"":               "",
		`'2025'`:         "2025",
		`'multi word'`:   "multi word",
	}
	for in, want := range cases {
		got := unquoteUci(in)
		if got != want {
			t.Errorf("unquoteUci(%q) = %q, want %q", in, got, want)
		}
	}
}
