// fp_test.go — tests de ServerFP con dial y reloj inyectados (paquete
// interno: necesita tocar dialFP/fpNow/fpCache, no exportables).
package reinstall

import (
	"errors"
	"testing"
	"time"
)

type fpTestHooks struct {
	dials   []string
	fp      string
	dialErr error
	now     time.Time
}

// installFPHooks stubba dialFP y fpNow y vacía el cache. Devuelve un reset.
func installFPHooks(t *testing.T, h *fpTestHooks) {
	t.Helper()
	origDial, origNow := dialFP, fpNow
	dialFP = func(host string) (string, error) {
		h.dials = append(h.dials, host)
		return h.fp, h.dialErr
	}
	fpNow = func() time.Time { return h.now }
	fpMu.Lock()
	fpCache = map[string]fpEntry{}
	fpMu.Unlock()
	t.Cleanup(func() {
		dialFP, fpNow = origDial, origNow
		fpMu.Lock()
		fpCache = map[string]fpEntry{}
		fpMu.Unlock()
	})
}

func TestServerFPHttpsDerivesAndCaches(t *testing.T) {
	h := &fpTestHooks{fp: "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"}
	installFPHooks(t, h)

	got := ServerFP("https://netpulse.example.org:3443")
	if got != h.fp {
		t.Fatalf("ServerFP = %q, quiero %q", got, h.fp)
	}
	if len(h.dials) != 1 || h.dials[0] != "netpulse.example.org:3443" {
		t.Fatalf("dial a %v, quiero [netpulse.example.org:3443]", h.dials)
	}

	// Dentro del TTL positivo NO re-diala.
	if again := ServerFP("https://netpulse.example.org:3443"); again != h.fp {
		t.Fatalf("cache positivo no devolvió el FP: %q", again)
	}
	if len(h.dials) != 1 {
		t.Fatalf("re-dial dentro del TTL positivo: %v", h.dials)
	}

	// Pasado el TTL re-dial.
	h.now = h.now.Add(fpPositiveTTL + time.Second)
	ServerFP("https://netpulse.example.org:3443")
	if len(h.dials) != 2 {
		t.Fatalf("sin re-dial tras expirar el cache positivo: %v", h.dials)
	}
}

func TestServerFPHttpsDefaultPort(t *testing.T) {
	h := &fpTestHooks{fp: "aa"}
	installFPHooks(t, h)
	ServerFP("https://netpulse.example.org")
	if len(h.dials) != 1 || h.dials[0] != "netpulse.example.org" {
		t.Fatalf("dial a %v, quiero el host tal cual (url.Host)", h.dials)
	}
}

func TestServerFPHttpSkipsDial(t *testing.T) {
	h := &fpTestHooks{fp: "aa"}
	installFPHooks(t, h)
	for _, u := range []string{"http://192.168.1.226:3000", "http://s:8080/x", "no-es-una-url", ""} {
		if got := ServerFP(u); got != "" {
			t.Errorf("ServerFP(%q) = %q, quiero \"\" para no-https", u, got)
		}
	}
	if len(h.dials) != 0 {
		t.Fatalf("dial para URL no-https: %v", h.dials)
	}
}

func TestServerFPNegativeCache(t *testing.T) {
	h := &fpTestHooks{dialErr: errors.New("red caida")}
	installFPHooks(t, h)

	if got := ServerFP("https://np.example.org"); got != "" {
		t.Fatalf("con dial fallido quiero \"\", tuve %q", got)
	}
	// Dentro del TTL negativo NO re-diala (anti-martilleo).
	ServerFP("https://np.example.org")
	if len(h.dials) != 1 {
		t.Fatalf("re-dial dentro del TTL negativo: %v", h.dials)
	}
	// Pasado el TTL negativo reintenta.
	h.now = h.now.Add(fpNegativeTTL + time.Second)
	h.dialErr = nil
	h.fp = "recuperado"
	if got := ServerFP("https://np.example.org"); got != "recuperado" {
		t.Fatalf("tras expirar el negativo quiero el FP nuevo, tuve %q", got)
	}
}

func TestServerFPEmptyPeerCerts(t *testing.T) {
	h := &fpTestHooks{fp: ""} // handshake sin leaf (dialFP devuelve "", nil)
	installFPHooks(t, h)
	if got := ServerFP("https://np.example.org"); got != "" {
		t.Fatalf("sin leaf quiero \"\", tuve %q", got)
	}
}
