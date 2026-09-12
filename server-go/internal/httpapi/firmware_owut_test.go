// firmware_owut_test.go - contrato HTTP del ciclo owut completo (#761):
// lista de versiones, instalación, upgrade desatendido y recurrencia.
package httpapi_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// owutVersionsSample replica `owut versions` (salida real recortada).
const owutVersionsSample = `Available 'version-to' values from https://sysupgrade.openwrt.org:
  24.10 release branch
    24.10.0
    24.10.2 (latest)
  25.12 release branch
    25.12.5
    25.12.7 (latest)
`

func reqJSON(t *testing.T, method, base, path, payload, cookie string) *http.Response {
	t.Helper()
	var body io.Reader
	if payload != "" {
		body = bytes.NewBufferString(payload)
	}
	req, err := http.NewRequest(method, base+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return res
}

func getJSONPath(t *testing.T, base, path, cookie string) *http.Response {
	return reqJSON(t, http.MethodGet, base, path, "", cookie)
}

func putJSONPath(t *testing.T, base, path, payload, cookie string) *http.Response {
	return reqJSON(t, http.MethodPut, base, path, payload, cookie)
}

func deleteJSONPath(t *testing.T, base, path, cookie string) *http.Response {
	return reqJSON(t, http.MethodDelete, base, path, "", cookie)
}

func TestOwutVersionsEndpoint(t *testing.T) {
	ssh := &scriptedSSH{rules: []sshRule{
		{contains: "command -v owut", out: "__owut__\n"},
		{contains: "owut versions", out: owutVersionsSample},
	}}
	ts, rid := makeOwutTestServer(t, ssh)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := getJSONPath(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-versions", cookie)
	body := readJSON(t, res)
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %v", res.StatusCode, body)
	}
	if body["owutAvailable"] != true {
		t.Fatalf("owutAvailable: %v", body["owutAvailable"])
	}
	versions, _ := body["versions"].([]any)
	if len(versions) != 4 {
		t.Fatalf("esperaba 4 versiones, got %v", versions)
	}
}

func TestOwutVersionsEndpointWithoutOwut(t *testing.T) {
	ssh := &scriptedSSH{rules: []sshRule{
		{contains: "command -v owut", out: "\n"}, // PATH sin owut
	}}
	ts, rid := makeOwutTestServer(t, ssh)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := getJSONPath(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-versions", cookie)
	body := readJSON(t, res)
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %v", res.StatusCode, body)
	}
	if body["owutAvailable"] != false {
		t.Fatalf("owutAvailable: %v", body["owutAvailable"])
	}
	if versions, _ := body["versions"].([]any); len(versions) != 0 {
		t.Fatalf("sin owut no hay versiones: %v", versions)
	}
	if ssh.saw("owut versions") {
		t.Fatalf("no debe lanzarse owut versions sin owut: %v", ssh.cmdsSnapshot())
	}
}

func TestOwutInstallEndpoint(t *testing.T) {
	ssh := &scriptedSSH{rules: []sshRule{
		{contains: "apk add owut", out: "OK: 1 installed\n__owut_exit__=0\n"},
	}}
	ts, rid := makeOwutTestServer(t, ssh)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-install", "{}", cookie)
	body := readJSON(t, res)
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %v", res.StatusCode, body)
	}
	if body["ok"] != true {
		t.Fatalf("ok: %v", body)
	}
	if !ssh.saw("apk add owut") {
		t.Fatalf("no se lanzó la instalación: %v", ssh.cmdsSnapshot())
	}
}

func TestOwutInstallEndpointNoPackageManager(t *testing.T) {
	ssh := &scriptedSSH{rules: []sshRule{
		{contains: "apk update", out: "__no_pm__\n__owut_exit__=0\n"},
	}}
	ts, rid := makeOwutTestServer(t, ssh)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-install", "{}", cookie)
	if res.StatusCode != 500 {
		t.Fatalf("status %d, esperaba 500", res.StatusCode)
	}
}

// waitForUpgradeStatus polea la lista hasta que el upgrade llegue al estado
// terminal (la goroutine del server corre en background).
func waitForUpgradeStatus(t *testing.T, ts *testServer, rid, want, cookie string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res := getJSONPath(t, ts.URL, "/api/firmware-upgrades", cookie)
		for _, it := range readJSONArray(t, res) {
			m, _ := it.(map[string]any)
			if m["routerId"] != rid {
				continue
			}
			if up, ok := m["upgrade"].(map[string]any); ok && up["status"] == want {
				return up
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("el upgrade no llegó a %s", want)
	return nil
}

func TestOwutUpgradeEndpointNoChanges(t *testing.T) {
	ssh := &scriptedSSH{rules: []sshRule{
		{contains: "command -v owut", out: "__owut__\n"},
		{contains: "owut upgrade", out: "There are no changes to upgrade (see '--force')\n__owut_exit__=0\n"},
	}}
	ts, rid := makeOwutTestServer(t, ssh)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-upgrade", `{"targetVersion":"25.12.7"}`, cookie)
	body := readJSON(t, res)
	if res.StatusCode != 202 {
		t.Fatalf("status %d: %v", res.StatusCode, body)
	}
	if body["engine"] != "owut" {
		t.Fatalf("engine: %v", body["engine"])
	}
	up := waitForUpgradeStatus(t, ts, rid, "done", cookie)
	if up["engine"] != "owut" {
		t.Fatalf("engine del upgrade: %v", up["engine"])
	}
	if !ssh.saw("owut upgrade -q") {
		t.Fatalf("comando lanzado: %v", ssh.cmdsSnapshot())
	}
}

func TestOwutUpgradeEndpointFailure(t *testing.T) {
	ssh := &scriptedSSH{rules: []sshRule{
		{contains: "command -v owut", out: "__owut__\n"},
		{contains: "owut upgrade", out: "Update checks reveal errors, can't proceed\n__owut_exit__=1\n"},
	}}
	ts, rid := makeOwutTestServer(t, ssh)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-upgrade", `{"targetVersion":"25.12.7"}`, cookie)
	if res.StatusCode != 202 {
		t.Fatalf("status %d", res.StatusCode)
	}
	up := waitForUpgradeStatus(t, ts, rid, "failed", cookie)
	if msg, _ := up["error"].(string); !strings.Contains(msg, "can't proceed") {
		t.Fatalf("error del upgrade: %v", up["error"])
	}
}

func TestOwutUpgradeRequiresTarget(t *testing.T) {
	ts, rid := makeOwutTestServer(t, nil)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-upgrade", `{}`, cookie)
	if res.StatusCode != 400 {
		t.Fatalf("status %d, esperaba 400 sin targetVersion", res.StatusCode)
	}
}

func TestRecurrencePutGetDelete(t *testing.T) {
	ts, rid := makeOwutTestServer(t, nil)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	// weekly válido.
	res := putJSONPath(t, ts.URL, "/api/firmware-upgrades/"+rid+"/recurrence",
		`{"enabled":true,"kind":"weekly","dayOfWeek":5,"time":"04:00"}`, cookie)
	if res.StatusCode != 200 {
		t.Fatalf("PUT weekly: %d %v", res.StatusCode, readJSON(t, res))
	}
	res = getJSONPath(t, ts.URL, "/api/firmware-upgrades/"+rid+"/recurrence", cookie)
	body := readJSON(t, res)
	rc, _ := body["recurrence"].(map[string]any)
	if rc["kind"] != "weekly" || rc["enabled"] != true {
		t.Fatalf("GET recurrence: %v", body)
	}
	if _, ok := body["nextRunMs"]; !ok {
		t.Fatalf("GET recurrence debe traer nextRunMs: %v", body)
	}

	// weekly sin time: 400.
	res = putJSONPath(t, ts.URL, "/api/firmware-upgrades/"+rid+"/recurrence",
		`{"enabled":true,"kind":"weekly","dayOfWeek":5}`, cookie)
	if res.StatusCode != 400 {
		t.Fatalf("PUT inválido: %d, esperaba 400", res.StatusCode)
	}

	// monthly y once válidos.
	res = putJSONPath(t, ts.URL, "/api/firmware-upgrades/"+rid+"/recurrence",
		`{"enabled":true,"kind":"monthly","dayOfMonth":1,"time":"05:30"}`, cookie)
	if res.StatusCode != 200 {
		t.Fatalf("PUT monthly: %d", res.StatusCode)
	}
	future := time.Now().Add(48 * time.Hour).UnixMilli()
	res = putJSONPath(t, ts.URL, "/api/firmware-upgrades/"+rid+"/recurrence",
		fmt.Sprintf(`{"enabled":true,"kind":"once","atMs":%d}`, future), cookie)
	if res.StatusCode != 200 {
		t.Fatalf("PUT once: %d", res.StatusCode)
	}

	// DELETE desactiva.
	res = deleteJSONPath(t, ts.URL, "/api/firmware-upgrades/"+rid+"/recurrence", cookie)
	if res.StatusCode != 200 {
		t.Fatalf("DELETE: %d", res.StatusCode)
	}
	res = getJSONPath(t, ts.URL, "/api/firmware-upgrades/"+rid+"/recurrence", cookie)
	body = readJSON(t, res)
	rc, _ = body["recurrence"].(map[string]any)
	if rc["enabled"] != false {
		t.Fatalf("tras DELETE debe quedar disabled: %v", body)
	}
}

func TestOwutEndpointsBlockedForVendorFirmware(t *testing.T) {
	// GL.iNet: la plataforma devuelve __gl__ -> install y upgrade 422, y el
	// listado de versiones llega con vendorFirmware y sin sondear ASU.
	ssh := &scriptedSSH{rules: []sshRule{
		{contains: "fw.gl-inet.com", out: "__gl__\n"},
	}}
	ts, rid := makeOwutTestServer(t, ssh)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-install", "{}", cookie)
	if res.StatusCode != 422 {
		t.Fatalf("install status %d, esperaba 422 vendor_firmware", res.StatusCode)
	}
	res = postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-upgrade", `{"targetVersion":"25.12.7"}`, cookie)
	if res.StatusCode != 422 {
		t.Fatalf("upgrade status %d, esperaba 422 vendor_firmware", res.StatusCode)
	}
	res = getJSONPath(t, ts.URL, "/api/firmware-upgrades/"+rid+"/owut-versions", cookie)
	body := readJSON(t, res)
	if body["vendorFirmware"] != "gl-inet" {
		t.Fatalf("vendorFirmware: %v", body["vendorFirmware"])
	}
	if versions, _ := body["versions"].([]any); len(versions) != 0 {
		t.Fatalf("vendor no debe listar versiones: %v", versions)
	}
	if ssh.saw("owut versions") {
		t.Fatalf("vendor no debe sondear versiones ASU: %v", ssh.cmdsSnapshot())
	}
}

func TestTargetSaveWithoutURL(t *testing.T) {
	// #761: con owut la imagen la construye ASU; el target se guarda sin URL.
	ts, rid := makeOwutTestServer(t, nil)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := postJSON(t, ts.URL, "/api/firmware-upgrades/"+rid+"/target",
		`{"model":"redmi_ax6","currentVersion":"25.12.5","targetVersion":"25.12.5"}`, cookie)
	if res.StatusCode != 200 {
		t.Fatalf("guardar sin targetUrl: %d %v", res.StatusCode, readJSON(t, res))
	}
	res = getJSONPath(t, ts.URL, "/api/firmware-upgrades", cookie)
	for _, it := range readJSONArray(t, res) {
		m, _ := it.(map[string]any)
		if m["routerId"] == rid && m["targetVersion"] != "25.12.5" {
			t.Fatalf("target no persistió: %v", m)
		}
	}
}
