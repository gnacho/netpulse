package adapters

import (
	"math"
	"testing"
)

// TestFreqToChannel cubre la conversión freq MHz → número de canal.
func TestFreqToChannel(t *testing.T) {
	cases := map[int]int{
		2412: 1,  // 2.4 GHz ch1
		2417: 2,  // ch2
		2437: 6,  // ch6
		2462: 11, // ch11
		2472: 13, // ch13
		2484: 14, // ch14 (Japón)
		5180: 36, // 5 GHz UNII-1 ch36
		5200: 40,
		5240: 48,
		5745: 149, // UNII-3
		5825: 165,
	}
	for freq, want := range cases {
		got := freqToChannel(freq)
		if got != want {
			t.Errorf("freqToChannel(%d) = %d, want %d", freq, got, want)
		}
	}
}

func TestFreqToBand(t *testing.T) {
	cases := map[int]string{
		2412: "2.4 GHz",
		2484: "2.4 GHz",
		5180: "5 GHz",
		5825: "5 GHz",
		6135: "6 GHz",
		9999: "",
	}
	for freq, want := range cases {
		got := freqToBand(freq)
		if got != want {
			t.Errorf("freqToBand(%d) = %q, want %q", freq, got, want)
		}
	}
}

// TestParseIwSurveyFlint2 usa la salida real de `iw dev wlan0 survey dump`
// y `iw dev wlan1 survey dump` del Flint2 como fixture.
func TestParseIwSurveyFlint2(t *testing.T) {
	out := `Survey data from wlan0
	frequency:			2412 MHz [in use]
	noise:				-90 dBm
	channel active time:		3632796925 ms
	channel busy time:		878259766 ms
	channel receive time:		440957725 ms
	channel BSS receive time:	173056055 ms
	channel transmit time:		340995043 ms
Survey data from wlan0
	frequency:			2417 MHz
	noise:				-92 dBm
	channel active time:		72 ms
	channel busy time:		20 ms
	channel receive time:		10 ms
	channel transmit time:		0 ms
Survey data from wlan1
	frequency:			5180 MHz [in use]
	noise:				-92 dBm
	channel active time:		3632802379 ms
	channel busy time:		146150367 ms
	channel receive time:		67413467 ms
	channel transmit time:		76785952 ms
Survey data from wlan1
	frequency:			5200 MHz
	noise:				-92 dBm
	channel active time:		191 ms
	channel busy time:		0 ms
	channel receive time:		0 ms
	channel transmit time:		0 ms
`
	result := parseIwSurvey(out)

	if len(result) != 2 {
		t.Fatalf("expected 2 devices, got %d: %v", len(result), result)
	}
	if len(result["wlan0"]) != 2 {
		t.Errorf("wlan0 channels: %d, want 2", len(result["wlan0"]))
	}
	if len(result["wlan1"]) != 2 {
		t.Errorf("wlan1 channels: %d, want 2", len(result["wlan1"]))
	}

	// Canal in-use wlan0: 2412 MHz = ch1, noise -90.
	ch := result["wlan0"][0]
	if ch.Freq != 2412 || ch.Channel != 1 || !ch.InUse || ch.NoiseDbm != -90 {
		t.Errorf("wlan0[0] wrong: %+v", ch)
	}
	// busyPct = 878259766 / 3632796925 * 100 ≈ 24.18%
	expectedBusy := 878259766.0 / 3632796925.0 * 100
	if math.Abs(ch.BusyPct-expectedBusy) > 0.01 {
		t.Errorf("wlan0[0] BusyPct = %.4f, want %.4f", ch.BusyPct, expectedBusy)
	}

	// Canal NO in-use wlan0: 2417 = ch2, noise -92.
	ch = result["wlan0"][1]
	if ch.Freq != 2417 || ch.Channel != 2 || ch.InUse || ch.NoiseDbm != -92 {
		t.Errorf("wlan0[1] wrong: %+v", ch)
	}
	// busyPct = 20 / 72 * 100 ≈ 27.78%
	expectedBusy = 20.0 / 72.0 * 100
	if math.Abs(ch.BusyPct-expectedBusy) > 0.01 {
		t.Errorf("wlan0[1] BusyPct = %.4f, want %.4f", ch.BusyPct, expectedBusy)
	}

	// Canal in-use wlan1: 5180 MHz = ch36.
	ch = result["wlan1"][0]
	if ch.Freq != 5180 || ch.Channel != 36 || !ch.InUse {
		t.Errorf("wlan1[0] wrong: %+v", ch)
	}
}

// TestParseIwSurveyEdge cubre entradas degeneradas.
func TestParseIwSurveyEdge(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"no-survey":   "unrelated line\nanother\n",
		"only-header": "Survey data from wlan0\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got := parseIwSurvey(in)
			if name == "only-header" {
				// 1 device con 0 channels (no se hace flush sin campos).
				if len(got["wlan0"]) != 0 {
					t.Fatalf("wlan0 channels: %d, want 0", len(got["wlan0"]))
				}
				return
			}
			if len(got) != 0 {
				t.Fatalf("expected empty map for %q, got %v", name, got)
			}
		})
	}
}

// TestParseIwSurveyChannelNoActive cubre un canal sin "channel active time"
// (no debe panicar, busyPct=0).
func TestParseIwSurveyChannelNoActive(t *testing.T) {
	out := `Survey data from wlan0
	frequency:			2412 MHz
	noise:				-85 dBm
`
	result := parseIwSurvey(out)
	if len(result["wlan0"]) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(result["wlan0"]))
	}
	ch := result["wlan0"][0]
	if ch.BusyPct != 0 || ch.RxPct != 0 || ch.TxPct != 0 {
		t.Errorf("busyPct should be 0 without active time, got busy=%.2f rx=%.2f tx=%.2f",
			ch.BusyPct, ch.RxPct, ch.TxPct)
	}
	if ch.NoiseDbm != -85 {
		t.Errorf("noise = %d, want -85", ch.NoiseDbm)
	}
}
