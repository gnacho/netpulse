package httpapi

import "testing"

func TestHealPusherPinAddsPinForHTTPS(t *testing.T) {
	env := map[string]string{
		"NETPULSE_SERVER":   "https://np.example:3000",
		"NETPULSE_INTERVAL": "45s",
	}
	out := healPusherPin(env, "https://np.example:3000")
	if out["NETPULSE_SERVER_FP"] == "" {
		t.Fatal("pin not merged for an https server URL")
	}
	if out["NETPULSE_INTERVAL"] != "45s" {
		t.Fatalf("existing keys must be preserved, got %q", out["NETPULSE_INTERVAL"])
	}
}

func TestHealPusherPinNoopForHTTP(t *testing.T) {
	env := map[string]string{"NETPULSE_SERVER": "http://127.0.0.1:3000"}
	out := healPusherPin(env, "http://127.0.0.1:3000")
	if _, ok := out["NETPULSE_SERVER_FP"]; ok {
		t.Fatal("pin must not be added for a plain http server URL")
	}
}
