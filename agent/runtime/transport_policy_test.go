package runtime

import "testing"

func TestPrivilegedEventAllowed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		server    string
		allow     bool
		wantError bool
	}{
		{"https", "https://netpulse.local:8443", false, false},
		{"https con path", "https://netpulse.local/netpulse", false, false},
		{"http denegado", "http://192.168.1.226:3000", false, true},
		{"http con valvula", "http://192.168.1.226:3000", true, false},
		{"sin esquema", "192.168.1.226:3000", false, true},
		{"esquema desconocido", "ftp://192.168.1.226", true, true},
		{"vacio", "   ", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := privilegedEventAllowed(tc.server, tc.allow)
			if tc.wantError && err == nil {
				t.Fatal("se esperaba error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("error inesperado: %v", err)
			}
		})
	}
}
