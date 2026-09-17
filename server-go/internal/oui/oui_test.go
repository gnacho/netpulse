package oui

import "testing"

func TestLookupKnownPrefix(t *testing.T) {
	cases := []struct {
		mac  string
		want string
	}{
		{"00:00:0C:00:00:00", "Cisco Systems, Inc"},
		{"00:03:93:00:00:00", "Apple, Inc."},
		{"24:0A:C4:00:00:00", "Espressif Inc."},
		{"B8:27:EB:00:00:00", "Raspberry Pi Foundation"},
		{"00:00:F0:00:00:00", "Samsung Electronics Co.,Ltd"},
	}
	for _, c := range cases {
		if got := Lookup(c.mac); got != c.want {
			t.Fatalf("Lookup(%q) = %q, want %q", c.mac, got, c.want)
		}
	}
}

func TestLookupNormalizesSeparatorsAndCase(t *testing.T) {
	if got := Lookup("B8-27-EB-12-34-56"); got != "Raspberry Pi Foundation" {
		t.Fatalf("dash: %q", got)
	}
	if got := Lookup("b827eb123456"); got != "Raspberry Pi Foundation" {
		t.Fatalf("bare lowercase: %q", got)
	}
	if got := Lookup("B827.EB12.3456"); got != "Raspberry Pi Foundation" {
		t.Fatalf("dot: %q", got)
	}
}

func TestLookupUnknownReturnsEmpty(t *testing.T) {
	if got := Lookup("FF:FF:FF:00:00:00"); got != "" {
		t.Fatalf("unknown prefix: %q", got)
	}
	if got := Lookup(""); got != "" {
		t.Fatalf("empty mac: %q", got)
	}
	if got := Lookup("not-a-mac"); got != "" {
		t.Fatalf("garbage: %q", got)
	}
}

func TestIsIoTVendor(t *testing.T) {
	iot := []string{"Espressif Inc.", "Tuya Smart Inc.", "Chengdu Meross Technology Co., Ltd.",
		"Shelly Europe LTD", "Lumi United Technology Co., Ltd", "IKEA of Sweden AB"}
	for _, m := range iot {
		if !IsIoTVendor(m) {
			t.Fatalf("IsIoTVendor(%q) = false, want true", m)
		}
	}
	nonIot := []string{"Apple, Inc.", "Cisco Systems, Inc", "Samsung Electronics Co.,Ltd", ""}
	for _, m := range nonIot {
		if IsIoTVendor(m) {
			t.Fatalf("IsIoTVendor(%q) = true, want false", m)
		}
	}
}

// A short vendor token must not swallow a longer unrelated name: a wrong
// vendor match hands the device a confident, wrong icon.
func TestVendorMatchingDoesNotOverreach(t *testing.T) {
	for _, tc := range []struct {
		manufacturer string
		iot          bool
	}{
		{"Gree Electric Appliances,Inc. of Zhuhai", true},
		{"WiZ", true},
		{"BSH Hausgeräte GmbH", true},
		{"Tuya Smart Inc.", true},
		{"Espressif Inc.", true},
		// Real vendors that merely contain a short token.
		{"Greenwave Systems", false},
		{"Greenliant Systems", false},
		{"WIZnet Co., Ltd.", false},
		{"Illuminati Instrument Corp", false}, // contains "lumi"
		{"Intel Corporate", false},
		{"", false},
	} {
		if got := IsIoTVendor(tc.manufacturer); got != tc.iot {
			t.Errorf("IsIoTVendor(%q) = %v, want %v", tc.manufacturer, got, tc.iot)
		}
	}
}

// Surveillance vendors get the camera type, which is more than "iot".
func TestCameraVendors(t *testing.T) {
	for _, m := range []string{"Reolink Innovation Limited", "Hangzhou Hikvision", "Dahua Technology", "EZVIZ Inc"} {
		if !IsCameraVendor(m) {
			t.Errorf("IsCameraVendor(%q) = false", m)
		}
	}
	for _, m := range []string{"Espressif Inc.", "Intel Corporate", ""} {
		if IsCameraVendor(m) {
			t.Errorf("IsCameraVendor(%q) = true", m)
		}
	}
}
