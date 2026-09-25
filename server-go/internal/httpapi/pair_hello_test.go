package httpapi_test

// FORK: end to end tests for proving the server's key at pairing - the real
// handler on a real TLS listener, and the agent's runtime.ProveServerKey on
// the other end.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	agentruntime "github.com/gnacho/netpulse/agent/runtime"
	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

const testPairingToken = "11111111-2222-4333-8444-555555555555"

// ownCert is a certificate of its own for a test server: httptest's servers
// all share one, which would make a "different key" test vacuous.
func ownCert(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}, hex.EncodeToString(sum[:])
}

// pairServer is the real API on a TLS listener with a key of its own, which
// is also the key it hands to agents. bodies records every request body.
type pairServer struct {
	*httptest.Server
	fp     string
	mu     sync.Mutex
	bodies []string
}

func newPairServer(t *testing.T) *pairServer {
	t.Helper()
	auth.SetTrustProxy(true)
	t.Cleanup(func() { auth.SetTrustProxy(false) })
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DATA_DIR": dataDir, "NODE_ENV": "test",
	}, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec("INSERT INTO kv (key, value) VALUES ('pairing.token', ?)", testPairingToken); err != nil {
		t.Fatal(err)
	}
	cert, fp := ownCert(t)
	ps := &pairServer{fp: fp}
	api := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Secret: secret,
		Hub:      sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil }),
		Agents:   adapters.NewAgentRegistry(0),
		Started:  time.Now(),
		ServerFP: fp,
	})
	ps.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		ps.mu.Lock()
		ps.bodies = append(ps.bodies, string(b))
		ps.mu.Unlock()
		r.Body = io.NopCloser(strings.NewReader(string(b)))
		api.ServeHTTP(w, r)
	}))
	ps.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	ps.StartTLS()
	t.Cleanup(ps.Close)
	return ps
}

func TestProveServerKeyLearnsTheServedKey(t *testing.T) {
	ps := newPairServer(t)
	fp, err := agentruntime.ProveServerKey(ps.URL, testPairingToken)
	if err != nil {
		t.Fatalf("ProveServerKey: %v", err)
	}
	if fp != ps.fp {
		t.Fatalf("learned %s, the server serves %s", fp, ps.fp)
	}
	// The pairing token never went to the server on this connection.
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for _, b := range ps.bodies {
		if strings.Contains(b, testPairingToken) {
			t.Fatalf("the pairing token was sent: %s", b)
		}
	}
}

func TestProveServerKeyRefusesAServerWithoutTheToken(t *testing.T) {
	ps := newPairServer(t)
	if _, err := agentruntime.ProveServerKey(ps.URL, "not-the-token"); err == nil {
		t.Fatal("a server that does not know the agent's token was accepted")
	}
}

// A man in the middle relays the hello to the real server and returns its
// genuine answer - over a connection that presents the middle's own key,
// with or without the real server's certificate attached to its chain.
func TestProveServerKeyRefusesARelayedProof(t *testing.T) {
	for _, attachReal := range []bool{false, true} {
		t.Run(fmt.Sprintf("real certificate attached=%v", attachReal), func(t *testing.T) {
			relayedProof(t, attachReal)
		})
	}
}

func relayedProof(t *testing.T, attachReal bool) {
	ps := newPairServer(t)
	relay := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // the attacker's own client
	}}
	middleCert, _ := ownCert(t)
	middle := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequest(r.Method, ps.URL+r.URL.Path, r.Body)
		req.Header = r.Header.Clone()
		res, err := relay.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer res.Body.Close()
		w.WriteHeader(res.StatusCode)
		io.Copy(w, res.Body)
	}))
	if attachReal {
		middleCert.Certificate = append(middleCert.Certificate, ps.TLS.Certificates[0].Certificate[0])
	}
	middle.TLS = &tls.Config{Certificates: []tls.Certificate{middleCert}}
	middle.StartTLS()
	defer middle.Close()

	_, err := agentruntime.ProveServerKey(middle.URL, testPairingToken)
	if err == nil || !strings.Contains(err.Error(), "intercepting") {
		t.Fatalf("a relayed proof was accepted or refused for the wrong reason: %v", err)
	}
}

// Over plain HTTP there is no chain to bind the proof to.
func TestPairHelloIsRefusedOverPlainHTTP(t *testing.T) {
	ts := makeTestServer(t)
	res, err := http.Post(ts.URL+"/api/agents/pair/hello", "application/json",
		strings.NewReader(`{"nonce":"`+strings.Repeat("ab", 32)+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.StatusCode)
	}
	if _, err := agentruntime.ProveServerKey(ts.URL, testPairingToken); err == nil {
		t.Fatal("the agent tried to prove a key over plain http")
	}
}

// The whole bootstrap: an agent with a pairing token and no pin pairs with an
// HTTPS server, and leaves an env file that pins the proven key next to its
// new token.
func TestAnAgentPairsOverHTTPSWithoutAPin(t *testing.T) {
	ps := newPairServer(t)
	env := t.TempDir() + "/netpulse-agent.env"
	if err := os.WriteFile(env, []byte("NETPULSE_SERVER="+ps.URL+"\nNETPULSE_SLUG=test-agent\n"+
		"NETPULSE_PAIRING_TOKEN="+testPairingToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := agentruntime.Run(context.Background(), agentruntime.Options{
		Server: ps.URL, Slug: "test-agent", PairingToken: testPairingToken, EnvFile: env,
	})
	if err != nil {
		t.Fatalf("pairing: %v", err)
	}
	got, err := os.ReadFile(env)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "NETPULSE_SERVER_FP="+ps.fp+"\n") {
		t.Fatalf("the proven pin was not written:\n%s", s)
	}
	if !strings.Contains(s, "NETPULSE_TOKEN=") || strings.Contains(s, "NETPULSE_PAIRING_TOKEN") {
		t.Fatalf("the env was not switched to the agent token:\n%s", s)
	}
}
