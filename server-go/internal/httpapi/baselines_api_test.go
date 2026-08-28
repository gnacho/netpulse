package httpapi_test

import (
	"net/http"
	"testing"
)

func TestBaselinesEndpointEmpty(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := get(t, ts.URL, "/api/baselines", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body := readJSON(t, res)
	if len(body) != 0 {
		t.Fatalf("expected empty baselines, got %v", body)
	}
}
