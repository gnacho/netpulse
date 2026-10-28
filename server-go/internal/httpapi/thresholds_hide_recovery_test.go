// thresholds_hide_recovery_test.go - campo hideRecoveryEvents del endpoint de
// umbrales (#1518): round-trip GET/PUT. El efecto del filtro sobre el feed lo
// cubre TestHideRecoveryFiltersFeedAndUnread (paleta alerts, engine con BD
// real): el adapter demo del server de test monta su engine SIN BD (dataset
// sintético), así que aquí solo se valida el contrato del endpoint.
package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func thresholdsBody(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("json: %v", err)
	}
	return body
}

func TestThresholdsHideRecoveryRoundTrip(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	// Default: off.
	body := thresholdsBody(t, get(t, ts.URL, "/api/settings/thresholds", cookie))
	if body["hideRecoveryEvents"] != false {
		t.Fatalf("default hideRecoveryEvents = %v, esperaba false", body["hideRecoveryEvents"])
	}

	// PUT on: persiste y el GET lo devuelve.
	res := doJSON(t, "PUT", ts.URL, "/api/settings/thresholds", cookie, `{"hideRecoveryEvents":true}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT hideRecoveryEvents: status %d", res.StatusCode)
	}
	body = thresholdsBody(t, res)
	if body["hideRecoveryEvents"] != true {
		t.Fatalf("tras PUT, hideRecoveryEvents = %v, esperaba true", body["hideRecoveryEvents"])
	}
	body = thresholdsBody(t, get(t, ts.URL, "/api/settings/thresholds", cookie))
	if body["hideRecoveryEvents"] != true {
		t.Fatalf("GET tras PUT: hideRecoveryEvents = %v, esperaba true", body["hideRecoveryEvents"])
	}

	// PUT off: vuelve el comportamiento por defecto.
	res = doJSON(t, "PUT", ts.URL, "/api/settings/thresholds", cookie, `{"hideRecoveryEvents":false}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT hideRecoveryEvents=false: status %d", res.StatusCode)
	}
	body = thresholdsBody(t, get(t, ts.URL, "/api/settings/thresholds", cookie))
	if body["hideRecoveryEvents"] != false {
		t.Fatalf("GET tras PUT off: hideRecoveryEvents = %v, esperaba false", body["hideRecoveryEvents"])
	}
}
