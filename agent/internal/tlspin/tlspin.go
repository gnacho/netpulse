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
	"fmt"
	"net/http"
	"strings"
)

// Normalize limpia un fingerprint: quita prefijo "sha256/", dos-puntos y
// espacios, y lo pasa a minúsculas. Formato esperado: 64 hex chars.
func Normalize(s string) string {
	s = strings.NewReplacer("sha256/", "", ":", "", " ", "").Replace(s)
	return strings.ToLower(strings.TrimSpace(s))
}

// BuildTransport crea un *http.Transport con la validación TLS adecuada:
//   - HTTP: transporte por defecto (sin TLS).
//   - HTTPS con fp: TLS con pinning SPKI (VerifyPeerCertificate).
//   - HTTPS sin fp: error (fail-closed).
func BuildTransport(serverURL, fp string) (*http.Transport, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if !strings.HasPrefix(serverURL, "https://") {
		return t, nil // HTTP plano: sin TLS
	}
	fp = Normalize(fp)
	if fp == "" {
		return nil, fmt.Errorf("HTTPS requiere NETPULSE_SERVER_FP (pinning SPKI, sin InsecureSkipVerify)")
	}
	want := fp
	t.TLSClientConfig = &tls.Config{
		// InsecureSkipVerify salta la verificación normal de CA; la decisión
		// final la toma VerifyPeerCertificate contra el SPKI pineado.
		InsecureSkipVerify: true, //nolint:gosec // pinning deliberado por SPKI
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return VerifySPKI(rawCerts, want)
		},
		MinVersion: tls.VersionTLS12,
	}
	return t, nil
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
