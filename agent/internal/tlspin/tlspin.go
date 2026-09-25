// Package tlspin — validación TLS del agente por pinning SPKI (Fase 9 R2).
//
// Sustituye a NETPULSE_INSECURE_TLS: en vez de saltar la verificación del
// certificado, el agente pinea el hash SHA-256 del SubjectPublicKeyInfo del
// servidor. Fail-closed: HTTPS sin fingerprint → error (nunca degrada a
// insecure).
package tlspin

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Normalize limpia un fingerprint: quita prefijo "sha256/", dos-puntos y
// espacios, y lo pasa a minúsculas. Formato esperado: 64 hex chars.
func Normalize(s string) string {
	// Lower-cased first, so "SHA256/" is stripped as well as "sha256/".
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer("sha256/", "", ":", "", " ", "").Replace(s)
}

// BuildTransport crea un *http.Transport con la validación TLS adecuada:
//   - HTTP: transporte por defecto (sin TLS).
//   - HTTPS con fp: TLS con pinning SPKI (VerifyConnection).
//   - HTTPS sin fp: error (fail-closed).
//
// FORK: fps may hold several comma-separated pins, and a pin may name either
// the server's own key (as before) or a CA in the chain the server presents;
// see Verify. More than one pin is what lets the server move to a new CA
// without a window in which its agents cannot reach it.
func BuildTransport(serverURL, fps string) (*http.Transport, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if !strings.HasPrefix(serverURL, "https://") {
		return t, nil // HTTP plano: sin TLS
	}
	pins, err := ParsePins(fps)
	if err != nil {
		return nil, err
	}
	if len(pins) == 0 {
		host := serverURL
		if u, err := url.Parse(serverURL); err == nil && u.Host != "" {
			host = u.Host
		}
		return nil, fmt.Errorf("HTTPS requiere NETPULSE_SERVER_FP (pinning SPKI, sin InsecureSkipVerify); consíguelo con install-agent.sh (lo deriva del certificado automáticamente) o con: openssl s_client -connect %s </dev/null 2>/dev/null | openssl x509 -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum", host)
	}
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("server URL: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		// Without a host there is nothing to check the certificate's names
		// against, and a CA pin would accept any name the CA signed.
		return nil, fmt.Errorf("server URL %q has no host", serverURL)
	}
	t.TLSClientConfig = &tls.Config{
		// InsecureSkipVerify salta la verificación normal de CA; la decisión
		// final la toma VerifyConnection contra el SPKI pineado.
		InsecureSkipVerify: true, //nolint:gosec // pinning deliberado por SPKI
		// VerifyConnection rather than VerifyPeerCertificate: it also runs on
		// resumed sessions, so a session cache added later cannot let a
		// connection skip the pin.
		VerifyConnection: func(cs tls.ConnectionState) error {
			return Verify(cs.PeerCertificates, host, pins, time.Now())
		},
		MinVersion: tls.VersionTLS12,
	}
	return t, nil
}

// ParsePins splits a comma-separated list of fingerprints, normalizing each.
// A pin that is not a SHA-256 in hex is an error rather than a pin that can
// never match: a typo in the env file should say so, not look like an
// impostor server.
func ParsePins(fps string) ([]string, error) {
	var pins []string
	for _, p := range strings.Split(Normalize(fps), ",") {
		if p == "" {
			continue
		}
		if b, err := hex.DecodeString(p); err != nil || len(b) != sha256.Size {
			return nil, fmt.Errorf("NETPULSE_SERVER_FP: %q is not a SHA-256 fingerprint (64 hex characters)", p)
		}
		pins = append(pins, p)
	}
	return pins, nil
}

// Verify decides whether the chain a server presented matches one of pins.
//
// A pin on the leaf's own key accepts the connection outright - the original
// behaviour, which checks neither name nor dates: the key is the identity.
//
// A pin on a CA accepts it only if the pinned key signed a leaf that is valid
// now, for server authentication, for host. Pinning a CA hands it the power
// to vouch for other keys, so those checks are what keeps "signed by our CA"
// from meaning "any certificate our CA ever signed, for any name, at any
// time".
//
// What is enforced is the leaf's signature by the pinned key. The CA
// certificate itself, as the server sends it, is not signed by anything this
// check trusts - anyone can present a copy of its key with other fields - so
// its own dates and name constraints cannot be relied on here, and its CA
// flags are only checked to catch a pin pointing at the wrong certificate.
// The server must send the CA certificate in its chain: a hash cannot stand
// in for it.
func Verify(chain []*x509.Certificate, host string, pins []string, now time.Time) error {
	if len(chain) == 0 {
		return fmt.Errorf("sin certificados del peer")
	}
	leaf := chain[0]
	// The handshake has already proved the peer holds the leaf's private key,
	// so a pinned key presented as the leaf is that key's holder - even when
	// it is a CA's key, which only its owner can present this way.
	if slices.Contains(pins, spki(leaf)) {
		return nil
	}
	var lastErr error
	for i, c := range chain[1:] {
		if !slices.Contains(pins, spki(c)) {
			continue
		}
		// A CA without a key usage extension (OpenSSL's default for one) may
		// sign; one that has the extension must include certificate signing.
		if !c.IsCA || !c.BasicConstraintsValid || (c.KeyUsage != 0 && c.KeyUsage&x509.KeyUsageCertSign == 0) {
			lastErr = fmt.Errorf("the pinned key belongs to a certificate that is not a CA")
			continue
		}
		roots := x509.NewCertPool()
		roots.AddCert(c)
		inter := x509.NewCertPool()
		for j, o := range chain[1:] {
			if j != i {
				inter.AddCert(o)
			}
		}
		_, err := leaf.Verify(x509.VerifyOptions{
			DNSName:       host,
			Roots:         roots,
			Intermediates: inter,
			CurrentTime:   now,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		})
		if err == nil {
			return nil
		}
		// Another pinned certificate further up the chain may still vouch
		// for the leaf, as during a move to a new CA.
		lastErr = explain(err, host)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("SPKI mismatch: the server's key %s matches no pinned key", spki(leaf))
}

// explain turns a chain verification error into one that says what to check.
func explain(err error, host string) error {
	var hostErr x509.HostnameError
	var invalid x509.CertificateInvalidError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &hostErr):
		return fmt.Errorf("the server's certificate does not name %s: %w", host, err)
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return fmt.Errorf("the server's certificate is expired or not yet valid - "+
			"if this device's clock is not set yet (no NTP), this clears once it is: %w", err)
	default:
		return fmt.Errorf("the server's certificate was not issued by the pinned CA: %w", err)
	}
}

func spki(c *x509.Certificate) string {
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

// VerifySPKI comprueba que el SPKI hash del leaf cert coincida con el
// fingerprint esperado (SHA-256 hex del DER del SubjectPublicKeyInfo).
func VerifySPKI(rawCerts [][]byte, wantHex string) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("sin certificados del peer")
	}
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("parsear leaf: %w", err)
	}
	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	got := hex.EncodeToString(sum[:])
	if got != wantHex {
		return fmt.Errorf("SPKI mismatch: got %s, want %s", got, wantHex)
	}
	return nil
}
