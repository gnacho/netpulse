// update_apply_test.go — regresión del gate de apply (layout estable).
package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestApplyUnavailableOnStableLayout(t *testing.T) {
	// Sin deploy/update.sh → modo estable: POST /api/update/apply → 409.
	root := t.TempDir()
	ts := makeTestServerWithUpdater(t, root)
	cookie := adminCookie(t, ts)
	req, _ := http.NewRequest("POST", ts.URL+"/api/update/apply", nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatalf("apply estable: got %d want 409", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "update_unavailable" {
		t.Errorf("error: %+v", body)
	}
}
