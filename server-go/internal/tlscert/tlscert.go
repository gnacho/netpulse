// Package tlscert — gestión del certificado autofirmado on-box (Fase 9 R2).
//
// En modo on-box el servidor escucha HTTPS con un cert autofirmado generado
// en el primer arranque. El agente valida la conexión por pinning del SPKI
// (SubjectPublicKeyInfo) hash, configurado durante el pairing — sin
// InsecureSkipVerify.
//
// El fingerprint expuesto por GET /fingerprint es SHA-256 del DER del SPKI en
// hex minúsculas (formato estándar de pinning, mismo que usa el agente en
// VerifyPeerCertificate).
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

// Ensure garantiza que existe un cert autofirmado en certPath/keyPath.
// Si ya existe, lo carga; si no, lo genera. Devuelve el *tls.Config para el
// listener y el fingerprint SPKI hex (SHA-256 del DER del SubjectPublicKeyInfo).
func Ensure(certPath, keyPath string) (*tls.Config, string, error) {
	if fileExists(certPath) && fileExists(keyPath) {
		return loadExisting(certPath, keyPath)
	}
	return generate(certPath, keyPath)
}

// Fingerprint calcula el SPKI hash hex de un certificado parsed.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

func loadExisting(certPath, keyPath string) (*tls.Config, string, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, "", fmt.Errorf("cargar cert existente: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, "", fmt.Errorf("parsear leaf: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, Fingerprint(leaf), nil
}

func generate(certPath, keyPath string) (*tls.Config, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("generar clave ECDSA: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, "", fmt.Errorf("serial: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "netpulse"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, "", fmt.Errorf("crear cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, "", fmt.Errorf("marshal clave: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, "", fmt.Errorf("escribir cert: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, "", fmt.Errorf("escribir key: %w", err)
	}

	leaf, _ := x509.ParseCertificate(der)
	tlsCert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS12,
	}, Fingerprint(leaf), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
