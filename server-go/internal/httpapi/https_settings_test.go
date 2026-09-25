package httpapi_test

// FORK: tests for Settings > HTTPS (https_settings.go). Addresses are from
// the private and documentation ranges, names invented.

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"github.com/gnacho/netpulse/server-go/internal/tlscert"
	"github.com/gnacho/netpulse/server-go/internal/tlsmode"
)

func makeHTTPSTestServer(t *testing.T) (*agentTestServer, *tlsmode.Manager) {
	t.Helper()
	auth.SetTrustProxy(true)
	t.Cleanup(func() { auth.SetTrustProxy(false) })
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
	}, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatal(err)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	mgr := tlsmode.New(tlsmode.Options{
		DB: d, DataDir: dataDir, Port: port,
		NewServer: func(addr string, h http.Handler) *http.Server { return &http.Server{Addr: addr, Handler: h} },
		CAOptions: func(o tlscert.CAOptions) tlscert.CAOptions {
			o.Addrs = func() ([]net.IP, error) { return []net.IP{net.ParseIP("192.168.50.2")}, nil }
			o.Hostname = func() (string, error) { return "monitor-box", nil }
			o.SearchDomains = func() []string { return nil }
			return o
		},
	})
	reg := adapters.NewAgentRegistry(90 * time.Second)
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Secret: secret,
		Hub:    sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil }),
		Agents: reg, Started: time.Now(), TLS: mgr,
		ServerFPFunc: mgr.Fingerprint,
	})
	if err := mgr.Start(handler); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mgr.PlainHandler(handler))
	status, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if status != 204 {
		t.Fatalf("login: %d", status)
	}
	t.Cleanup(func() {
		mgr.Close()
		srv.Close()
		d.Close()
	})
	return &agentTestServer{Server: srv, db: d, agents: reg, cookie: cookie}, mgr
}

// httpsCall makes an admin request; secure marks it as having arrived over
// HTTPS through a proxy the server trusts.
func httpsCall(t *testing.T, ts *agentTestServer, method, path, body string, secure bool) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+ts.cookie)
	if secure {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	data, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(data, &out)
	return res.StatusCode, out
}

func TestEveryHTTPSChangeAsksForThePassword(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	for _, pw := range []string{"", "not-the-password"} {
		if st, _ := httpsCall(t, ts, "POST", "/api/settings/https",
			fmt.Sprintf(`{"enabled":true,"password":%q}`, pw), true); st != http.StatusUnauthorized {
			t.Fatalf("password %q: status %d, want 401", pw, st)
		}
	}
	if mgr.Enabled() {
		t.Fatal("HTTPS was turned on without the password")
	}
	st, body := httpsCall(t, ts, "POST", "/api/settings/https", `{"enabled":true,"password":"test123456"}`, true)
	if st != http.StatusOK || !mgr.Enabled() {
		t.Fatalf("enable: %d %v", st, body)
	}
	_, status := httpsCall(t, ts, "GET", "/api/settings/https", "", true)
	if status["rootSha256"] == "" || status["fingerprint"] == "" || status["mode"] != "full" {
		t.Fatalf("status: %v", status)
	}
}

func TestTheRootIsServedToAnyone(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	res, _ := http.Get(ts.URL + "/netpulse-ca.crt")
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("with HTTPS off: %d, want 404", res.StatusCode)
	}
	if err := mgr.SetEnabled(true, "test"); err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(ts.URL + "/netpulse-ca.crt") // no session
	if err != nil {
		t.Fatal(err)
	}
	der, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.Header.Get("Content-Type") != "application/x-x509-ca-cert" {
		t.Fatalf("content type %q", res.Header.Get("Content-Type"))
	}
	root, err := x509.ParseCertificate(der)
	if err != nil || !root.IsCA || tlscert.Fingerprint(root) != mgr.Fingerprint() {
		t.Fatalf("served root is not the CA agents pin: %v", err)
	}
}

func TestAStricterModeIsConfirmedOverHTTPSOnly(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	_ = mgr.SetEnabled(true, "test")
	st, body := httpsCall(t, ts, "POST", "/api/settings/https", `{"mode":"migrate","password":"test123456"}`, false)
	if st != http.StatusOK || body["pending"] != "migrate" || mgr.Mode() != tlsmode.Full {
		t.Fatalf("request: %d %v, mode %s", st, body, mgr.Mode())
	}
	confirmURL, _ := body["confirmUrl"].(string)
	if !strings.HasPrefix(confirmURL, "https://") || !strings.Contains(confirmURL, "confirm=") {
		t.Fatalf("confirmUrl %q", confirmURL)
	}
	code := confirmURL[strings.Index(confirmURL, "confirm=")+len("confirm="):]
	if i := strings.IndexByte(code, '&'); i >= 0 {
		code = code[:i]
	}
	// Over plain HTTP the confirmation is refused - and in fact the request
	// is already refused as a session call, even before the mode applies.
	if st, _ := httpsCall(t, ts, "POST", "/api/settings/https/confirm", fmt.Sprintf(`{"code":%q}`, code), false); st == http.StatusOK {
		t.Fatal("confirmed over plain HTTP")
	}
	if st, body := httpsCall(t, ts, "POST", "/api/settings/https/confirm", fmt.Sprintf(`{"code":%q}`, code), true); st != http.StatusOK || mgr.Mode() != tlsmode.Migrate {
		t.Fatalf("confirm over HTTPS: %d %v, mode %s", st, body, mgr.Mode())
	}
	// Now plain HTTP refuses the session call outright.
	if st, _ := httpsCall(t, ts, "GET", "/api/settings/https", "", false); st != http.StatusForbidden {
		t.Fatalf("a session call over plain HTTP in migrate: %d, want 403", st)
	}
}

// Redirect would cut off agents still on plain HTTP; the page says which, and
// only an explicit force goes ahead.
func TestRedirectWaitsForAgentsOnHTTP(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	_ = mgr.SetEnabled(true, "test")
	_, token, _ := createAgentToken(t, ts, "test-router")
	res := ingest(t, ts, token, "192.0.2.50", validPayloadFor("test-router"))
	res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest: %d", res.StatusCode)
	}
	st, body := httpsCall(t, ts, "POST", "/api/settings/https", `{"mode":"redirect","password":"test123456"}`, true)
	if st != http.StatusConflict || body["error"] != "agents_on_http" || !strings.Contains(fmt.Sprint(body["agents"]), "test-router") {
		t.Fatalf("redirect with an agent on HTTP: %d %v", st, body)
	}
	st, body = httpsCall(t, ts, "POST", "/api/settings/https", `{"mode":"redirect","force":true,"password":"test123456"}`, true)
	if st != http.StatusOK || body["pending"] != "redirect" {
		t.Fatalf("forced: %d %v", st, body)
	}
}

func validPayloadFor(slug string) string {
	return fmt.Sprintf(`{"router":%q,"ts":%d,"version":"test","data":{}}`, slug, time.Now().Unix())
}

// The confirmation arrives over the real HTTPS listener - the one it is
// meant to prove works - and applies the mode.
func TestAModeIsConfirmedThroughTheRealHTTPSListener(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	if err := mgr.SetEnabled(true, "test"); err != nil {
		t.Fatal(err)
	}
	code, _, err := mgr.RequestMode(tlsmode.Migrate, "test")
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(mgr.CA().RootPEM())
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}}
	url := fmt.Sprintf("https://127.0.0.1:%d/api/settings/https/confirm", mgr.Status().Port)
	var res *http.Response
	for i := 0; i < 50; i++ { // the listener starts in a goroutine
		req, _ := http.NewRequest("POST", url, strings.NewReader(fmt.Sprintf(`{"code":%q}`, code)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "session="+ts.cookie)
		if res, err = client.Do(req); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || mgr.Mode() != tlsmode.Migrate {
		t.Fatalf("confirm over the HTTPS listener: %d, mode %s", res.StatusCode, mgr.Mode())
	}
}

// Agents already on HTTPS would be cut off by turning it off; the page says
// which, and only an explicit force goes ahead. Turning it off in a stricter
// mode is refused outright.
func TestTurningHTTPSOffWarnsAboutAgentsOnIt(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	_ = mgr.SetEnabled(true, "test")
	_, token, _ := createAgentToken(t, ts, "test-router")
	req, _ := http.NewRequest("POST", ts.URL+"/api/ingest/agent", strings.NewReader(validPayloadFor("test-router")))
	res := ingestSecure(t, req, token)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest over https: %d", res.StatusCode)
	}
	st, body := httpsCall(t, ts, "POST", "/api/settings/https", `{"enabled":false,"password":"test123456"}`, true)
	if st != http.StatusConflict || body["error"] != "agents_on_https" {
		t.Fatalf("turning off with an agent on HTTPS: %d %v", st, body)
	}
	code, _, _ := mgr.RequestMode(tlsmode.Migrate, "test")
	_, _ = mgr.Confirm(code, true, "test")
	st, body = httpsCall(t, ts, "POST", "/api/settings/https", `{"enabled":false,"force":true,"password":"test123456"}`, true)
	if st != http.StatusConflict || body["error"] != "not_full" {
		t.Fatalf("turning off in migrate: %d %v", st, body)
	}
	_, _, _ = mgr.RequestMode(tlsmode.Full, "test")
	st, _ = httpsCall(t, ts, "POST", "/api/settings/https", `{"enabled":false,"force":true,"password":"test123456"}`, true)
	if st != http.StatusOK || mgr.Enabled() {
		t.Fatalf("forced off from full: %d, enabled %v", st, mgr.Enabled())
	}
}

// ingestSecure pushes as an agent would through a trusted TLS proxy.
func ingestSecure(t *testing.T, req *http.Request, token string) *http.Response {
	t.Helper()
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-For", "192.0.2.51")
	req.Header.Set("Authorization", "Bearer "+token)
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(body)
	req.Header.Set("X-Agent-Signature", hex.EncodeToString(mac.Sum(nil)))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

// In migrate, the admin pairing token is refused over plain HTTP, and
// accepted over HTTPS.
func TestMigrateRefusesThePairingTokenOverPlainHTTP(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	_ = mgr.SetEnabled(true, "test")
	code, _, _ := mgr.RequestMode(tlsmode.Migrate, "test")
	_, _ = mgr.Confirm(code, true, "test")
	var tok string
	if err := ts.db.QueryRow("SELECT value FROM kv WHERE key = 'pairing.token'").Scan(&tok); err != nil {
		// Created on first use.
		tok = "11111111-2222-4333-8444-555555555555"
		if _, err := ts.db.Exec("INSERT INTO kv (key, value) VALUES ('pairing.token', ?)", tok); err != nil {
			t.Fatal(err)
		}
	}
	pair := func(secure bool) int {
		req, _ := http.NewRequest("POST", ts.URL+"/api/agents/pair",
			strings.NewReader(fmt.Sprintf(`{"pairing_token":%q,"slug":"new-router"}`, tok)))
		req.Header.Set("Content-Type", "application/json")
		if secure {
			req.Header.Set("X-Forwarded-Proto", "https")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if st := pair(false); st != http.StatusForbidden {
		t.Fatalf("over plain HTTP: %d, want 403", st)
	}
	if st := pair(true); st != http.StatusCreated {
		t.Fatalf("over HTTPS: %d, want 201", st)
	}
}

// Wrong passwords here count towards the sign-in lockout.
func TestThePasswordCheckIsRateLimited(t *testing.T) {
	ts, _ := makeHTTPSTestServer(t)
	last := 0
	for i := 0; i < 6; i++ {
		last, _ = httpsCall(t, ts, "POST", "/api/settings/https", `{"enabled":true,"password":"wrong"}`, true)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after six wrong passwords: %d, want 429", last)
	}
}

// A sign-in over HTTPS gets its own Secure cookie, so it does not replace -
// and cannot lock out - a sign-in over plain HTTP on the same host.
func TestHTTPSAndHTTPSignInsKeepTheirOwnCookies(t *testing.T) {
	ts, _ := makeHTTPSTestServer(t)
	_, _, plainSet := loginCookie(t, ts.URL, "admin", "test123456")
	if !strings.HasPrefix(plainSet, "session=") || strings.Contains(plainSet, "Secure") {
		t.Fatalf("plain sign-in: %q", plainSet)
	}
	_, _, secureSet := loginCookie(t, ts.URL, "admin", "test123456", "X-Forwarded-Proto", "https")
	if !strings.HasPrefix(secureSet, "__Host-session=") || !strings.Contains(secureSet, "; Secure") {
		t.Fatalf("secure sign-in: %q", secureSet)
	}
	secureVal := strings.SplitN(strings.TrimPrefix(secureSet, "__Host-session="), ";", 2)[0]

	// Both cookies at once, as a browser sends them over HTTPS.
	req, _ := http.NewRequest("GET", ts.URL+"/api/auth/me", nil)
	req.Header.Set("Cookie", "session=stale.value; __Host-session="+secureVal)
	req.Header.Set("X-Forwarded-Proto", "https")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the Secure session was not accepted: %d", res.StatusCode)
	}

	// Signing out over HTTPS clears both, and ends the session.
	req, _ = http.NewRequest("POST", ts.URL+"/api/auth/logout", nil)
	req.Header.Set("Cookie", "__Host-session="+secureVal)
	req.Header.Set("X-Forwarded-Proto", "https")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	cleared := strings.Join(res.Header.Values("Set-Cookie"), " | ")
	if !strings.Contains(cleared, "session=; Path=/") || !strings.Contains(cleared, "__Host-session=; Path=/; HttpOnly; Secure") {
		t.Fatalf("logout cleared: %s", cleared)
	}
	req, _ = http.NewRequest("GET", ts.URL+"/api/auth/me", nil)
	req.Header.Set("Cookie", "__Host-session="+secureVal)
	req.Header.Set("X-Forwarded-Proto", "https")
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the session outlived logout: %d", res.StatusCode)
	}
}

// The install line carries the root and the pin once HTTPS is on, and the
// printf that writes the root reproduces it exactly.
func TestTheInstallLineCarriesTheRoot(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	_, _, before := createAgentToken(t, ts, "test-router")
	if strings.Contains(before, "NPCA") || strings.Contains(before, "--server-fp") {
		t.Fatalf("with HTTPS off the line changed: %s", before)
	}
	if err := mgr.SetEnabled(true, "test"); err != nil {
		t.Fatal(err)
	}
	_, _, line := createAgentToken(t, ts, "test-router-2")
	wantServer := fmt.Sprintf("--server=https://192.168.50.2:%d", mgr.Status().Port)
	for _, want := range []string{`--cacert "$NPCA"`, "NPCA=$(mktemp)", "--server-fp=" + mgr.Fingerprint(), wantServer} {
		if !strings.Contains(line, want) {
			t.Fatalf("install line lacks %q:\n%s", want, line)
		}
	}
	dir := t.TempDir()
	// Up to the download: create the file, write the root, show where.
	upTo := line[:strings.Index(line, " && curl")]
	upTo = strings.Replace(upTo, "$(mktemp)", dir+"/ca.pem", 1)
	if out, err := exec.Command("sh", "-c", upTo).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	got, _ := os.ReadFile(dir + "/ca.pem")
	if string(got) != string(mgr.CA().RootPEM()) {
		t.Fatalf("the line writes:\n%s\nwant:\n%s", got, mgr.CA().RootPEM())
	}
}

// With no agent reporting, the list is empty, never null: the page reads its
// length, and a null took the whole Settings page down.
func TestTheAgentListIsNeverNull(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	_ = mgr.SetEnabled(true, "test")
	req, _ := http.NewRequest("GET", ts.URL+"/api/settings/https", nil)
	req.Header.Set("Cookie", "session="+ts.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), `"agentsOnHttp":[]`) {
		t.Fatalf("status: %s", body)
	}
}
