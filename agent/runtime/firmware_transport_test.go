package runtime

// FORK: tests for firmwareTransport. Addresses are from the documentation
// ranges.

import (
	"net/http"
	"testing"
)

// The pinned transport accepts only the server's own key, so it is kept for
// an image on the server itself and never used for a mirror.
func TestFirmwareTransportKeepsThePinOnlyForTheServer(t *testing.T) {
	pinned := &http.Transport{}
	for _, tc := range []struct {
		server  string
		target  string
		wantPin bool
	}{
		{"https://192.0.2.10:3443", "https://192.0.2.10:3443/firmware/image.bin", true},
		{"https://192.0.2.10:3443", "https://downloads.example.org/releases/image.bin", false},
		{"https://192.0.2.10:3443", "https://192.0.2.10:8443/image.bin", false},
		{"https://192.0.2.10:3443", "http://192.0.2.10:3443/image.bin", false},
		// The default port, written out or not, is the same server.
		{"https://192.0.2.10", "https://192.0.2.10:443/image.bin", true},
		{"https://192.0.2.10:443", "https://192.0.2.10/image.bin", true},
	} {
		got := firmwareTransport(tc.server, tc.target, pinned)
		if (got == http.RoundTripper(pinned)) != tc.wantPin {
			t.Errorf("server %s, image %s: pinned transport used = %v, want %v",
				tc.server, tc.target, got == http.RoundTripper(pinned), tc.wantPin)
		}
	}
}
