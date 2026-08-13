// update_readiness_test.go — readiness en /api/update/status (issue #160).
package httpapi_test

import (
	"testing"
)

func TestUpdateStatusExposesReadiness(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
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
