package runtime

// FORK: learning the server's key at pairing without trusting whoever
// answers first.
//
// An agent given a pairing token but no pin used to have no way to reach an
// HTTPS server at all: the pin had to be supplied beforehand. ProveServerKey
// closes that gap without falling back to trust on first use. The pairing
// token is a secret the admin copies from the server, so the server can prove
// it knows the token - and bind that proof to the key it is serving - while
// the token itself never travels on the not-yet-authenticated connection:
//
//	agent -> POST /api/agents/pair/hello {nonce}
//	server -> {server_fp, mac = pairproof.MAC(token, nonce, server_fp)}
//
// The agent accepts server_fp only if the MAC is right AND server_fp is the
// key of a certificate in the chain it was actually shown. A man in the
// middle can relay the hello and obtain the real server's MAC, but for the
// real server's key, which is not in the chain the middle presented.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/agent/internal/tlspin"
	"github.com/gnacho/netpulse/agent/pairproof"
)

// ServerTransport is the transport an embedder should use for its own
// requests to the NetPulse server: the same pinning the agent uses. fps is
// NETPULSE_SERVER_FP - one or more comma-separated pins.
func ServerTransport(serverURL, fps string) (*http.Transport, error) {
	return tlspin.BuildTransport(serverURL, fps)
}

// ValidatePins checks a NETPULSE_SERVER_FP value - one or more
// comma-separated SHA-256 fingerprints - as the agent will read it, so a
// setting can be refused when it is entered rather than when the agent
// starts.
func ValidatePins(fps string) error {
	_, err := tlspin.ParsePins(fps)
	return err
}

// ProveServerKey learns which key the HTTPS server at serverURL serves and
// checks, with the pairing token, that it is the real server's. It returns
// the fingerprint to pin. It never sends the pairing token.
func ProveServerKey(serverURL, pairingToken string) (string, error) {
	if !strings.HasPrefix(serverURL, "https://") {
		return "", errors.New("the server's key can only be proven over https")
	}
	if pairingToken == "" {
		return "", errors.New("proving the server's key needs the pairing token")
	}
	var (
		mu   sync.Mutex
		seen []*x509.Certificate
	)
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	t.TLSClientConfig = &tls.Config{
		// Nothing is trusted on this connection: it only records the chain,
		// and what the server says over it is checked against that chain.
		InsecureSkipVerify: true, //nolint:gosec // the chain is checked below, against the MAC
		MinVersion:         tls.VersionTLS12,
		VerifyConnection: func(cs tls.ConnectionState) error {
			mu.Lock()
			seen = cs.PeerCertificates
			mu.Unlock()
			return nil
		},
	}
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(nonceBytes)
	body := fmt.Sprintf(`{"nonce":%q}`, nonce)
	req, err := http.NewRequest("POST", strings.TrimRight(serverURL, "/")+"/api/agents/pair/hello", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := &http.Client{
		Transport: t,
		Timeout:   15 * time.Second,
		// A redirect would move the request to another connection, whose
		// chain is not the one recorded.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("pair hello: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return "", fmt.Errorf("pair hello: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var hello struct {
		ServerFP string `json:"server_fp"`
		MAC      string `json:"mac"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&hello); err != nil {
		return "", fmt.Errorf("pair hello: %w", err)
	}
	fp := tlspin.Normalize(hello.ServerFP)
	want := pairproof.MAC(pairingToken, nonce, fp)
	if !hmac.Equal([]byte(strings.ToLower(hello.MAC)), []byte(want)) {
		return "", errors.New("pair hello: the server could not prove it holds the pairing token; " +
			"this is not the server the token came from, or the token is wrong")
	}
	mu.Lock()
	chain := seen
	mu.Unlock()
	// The proven key must be the one this connection authenticated with: the
	// leaf's own key, or a CA key that signed the leaf for this host. Merely
	// appearing in the chain is not enough - a relay can attach the real
	// server's certificate to its own.
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", err
	}
	if err := tlspin.Verify(chain, u.Hostname(), []string{fp}, time.Now()); err != nil {
		return "", fmt.Errorf("pair hello: the proven key is not the one this connection authenticated with; "+
			"something between this device and the server is intercepting the connection (%v)", err)
	}
	return fp, nil
}
