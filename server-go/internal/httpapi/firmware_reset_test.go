// firmware_reset_test.go - contrato HTTP del reset del estado de upgrade
// (#1776): un intento atascado se limpia desde la UI sin tocar la API a mano.
package httpapi_test

import (
	"net/http"
	"testing"
)

// upgradeOf busca la fila del router en la lista y devuelve su objeto
// "upgrade", o nil cuando no hay ninguno.
func upgradeOf(t *testing.T, ts *testServer, rid, cookie string) map[string]any {
	t.Helper()
	res := getJSONPath(t, ts.URL, "/api/firmware-upgrades", cookie)
	for _, it := range readJSONArray(t, res) {
		m, _ := it.(map[string]any)
		if m["routerId"] != rid {
			continue
		}
		up, _ := m["upgrade"].(map[string]any)
		return up
	}
	t.Fatalf("%s no aparece en la lista", rid)
	return nil
}

// #1776: un intento terminado en failed se limpia con el reset y la fila deja
// de anunciarlo. Es el caso que antes exigía borrar la fila a mano.
func TestFirmwareResetClearsFailedUpgrade(t *testing.T) {
	ssh := &scriptedSSH{rules: []sshRule{
		{contains: "command -v owut", out: "__owut__\n"},
		{contains: "owut upgrade", out: "Update checks reveal errors, can't proceed\n__owut_exit__=1\n"},
	}}
	ts, rid := makeOwutTestServer(t, ssh)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	if res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-upgrade", `{"targetVersion":"25.12.7"}`, cookie); res.StatusCode != 202 {
		t.Fatalf("status del upgrade %d", res.StatusCode)
	}
	waitForUpgradeStatus(t, ts, rid, "failed", cookie)

	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/reset", "{}", cookie)
	body := readJSON(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %v", res.StatusCode, body)
	}
	if body["ok"] != true || body["cleared"] != true {
		t.Fatalf("respuesta del reset: %v", body)
	}
	if up := upgradeOf(t, ts, rid, cookie); up != nil {
		t.Fatalf("tras el reset no debería quedar upgrade: %v", up)
	}
}

// #1776: sin nada que limpiar el endpoint es idempotente y lo dice, en vez de
// inventar un borrado.
func TestFirmwareResetWithoutUpgrade(t *testing.T) {
	ts, rid := makeOwutTestServer(t, nil)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/reset", "{}", cookie)
	body := readJSON(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %v", res.StatusCode, body)
	}
	if body["cleared"] != false {
		t.Fatalf("cleared: %v, quería false sin upgrade", body["cleared"])
	}
}
