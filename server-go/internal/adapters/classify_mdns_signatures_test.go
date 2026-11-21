package adapters

import "testing"

func TestGuessFromMdnsSignatures(t *testing.T) {
	tests := []struct {
		name         string
		services     []string
		model        string
		manufacturer string
		want         string
	}{
		{
			name:         "Marantz real fixture is a speaker",
			services:     []string{"_airplay._tcp"},
			model:        "Marantz NR1608",
			manufacturer: "Marantz",
			want:         "altavoz",
		},
		{
			name:         "LG real fixture is a TV",
			services:     []string{"_airplay._tcp"},
			model:        "OLED77CX6LA",
			manufacturer: "LG Electronics",
			want:         "tv",
		},
		{
			name:         "Apple TV simulated candidate stays unclassified",
			services:     []string{"_airplay._tcp"},
			model:        "AppleTV14,1",
			manufacturer: "Apple",
			want:         "",
		},
		{
			name:         "HomePod simulated candidate stays unclassified",
			services:     []string{"_airplay._tcp"},
			model:        "AudioAccessory5,1",
			manufacturer: "Apple",
			want:         "",
		},
		{
			name:         "missing model gives no signature",
			services:     []string{"_airplay._tcp"},
			manufacturer: "Marantz",
			want:         "",
		},
		{
			name:         "unknown model gives no signature",
			services:     []string{"_airplay._tcp"},
			model:        "Unknown 2000",
			manufacturer: "Unknown",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := guessFromMdnsSignatures(tt.services, tt.model, tt.manufacturer); got != tt.want {
				t.Errorf("guessFromMdnsSignatures(%v, %q, %q) = %q, want %q", tt.services, tt.model, tt.manufacturer, got, tt.want)
			}
		})
	}
}

func TestMdnsSignatureCatalogIsValid(t *testing.T) {
	for _, signature := range mdnsProductSignatures {
		if signature.fixture != "real" && signature.fixture != "simulated" {
			t.Errorf("signature %s has invalid fixture %q", signature.id, signature.fixture)
		}
		if !ValidDeviceTypes[signature.deviceType] {
			t.Errorf("signature %s returns unknown type %q", signature.id, signature.deviceType)
		}
	}
}
