// update_readiness_test.go — readiness en /api/update/status (issue #160).
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/updater"
)

func TestUpdateStatusExposesReadiness(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	// #160: el check de conectividad apunta a un servidor local. Antes dependia
	// de que el runner alcanzase github.com y un corte de red ponia en rojo el
	// workflow sin que hubiese nada roto.
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer probe.Close()
	defer updater.SetProbeURL(probe.URL)()
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	ts := makeTestServerWithUpdater(t, root)
	cookie := adminCookie(t, ts)
	res := get(t, ts.URL, "/api/update/status", cookie)
	if res.StatusCode != 200 {
		t.Fatalf("status: got %d want 200", res.StatusCode)
	}
	body := readJSON(t, res)
	r, ok := body["readiness"].(map[string]any)
	if !ok {
		t.Fatalf("readiness ausente: %+v", body)
	}
	ready, _ := r["ready"].(bool)
	if !ready {
		t.Errorf("ready false: %+v", r)
	}
	for _, k := range []string{"disk", "git", "network", "concurrent"} {
		ck, ok := r[k].(map[string]any)
		if !ok {
			t.Errorf("check %s ausente: %+v", k, r)
			continue
		}
		okv, _ := ck["ok"].(bool)
		if !okv {
			t.Errorf("check %s = %+v, want ok", k, ck)
		}
	}
}
