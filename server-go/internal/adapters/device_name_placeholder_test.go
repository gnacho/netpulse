package adapters

import "testing"

func TestIsGeneratedDeviceName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "android random suffix", in: "android-7F3A9C2D", want: true},
		{name: "android random suffix with underscore", in: "android_7f3a9c2d", want: true},
		{name: "shelly technical suffix", in: "shelly1-8CAAB50000A2", want: true},
		{name: "esphome technical suffix", in: "esphome-abcdef12", want: true},
		{name: "meaningful iphone name", in: "iPhone-de-Nacho", want: false},
		{name: "meaningful vacuum name", in: "roomba-cocina", want: false},
		{name: "android version is not a random suffix", in: "android-13", want: false},
		{name: "ordinary hostname", in: "pixel-8-pro", want: false},
		{name: "shelly location is not a technical suffix", in: "shelly1-kitchen", want: false},
		{name: "unrelated technical looking hostname", in: "server-deadbeef", want: false},
		{name: "empty", in: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGeneratedDeviceName(tt.in); got != tt.want {
				t.Errorf("isGeneratedDeviceName(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
