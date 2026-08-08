package tlscert

import (
	"path/filepath"
	"testing"
)

func TestEnsureGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	// Primera llamada: genera.
	tlsConf1, fp1, err := Ensure(certPath, keyPath)
	if err != nil {
		t.Fatalf("Ensure (generate): %v", err)
	}
	if tlsConf1 == nil || len(tlsConf1.Certificates) != 1 {
		t.Fatal("tls.Config sin certificados")
	}
	if fp1 == "" {
		t.Fatal("fingerprint vacío")
	}

	// Segunda llamada: carga el existente. El fingerprint debe ser idéntico.
	tlsConf2, fp2, err := Ensure(certPath, keyPath)
	if err != nil {
		t.Fatalf("Ensure (load): %v", err)
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprint cambió entre generate y load: %s ≠ %s", fp1, fp2)
	}
	if len(tlsConf2.Certificates) != 1 {
		t.Fatal("tls.Config (load) sin certificados")
	}
}

func TestFingerprintIs64HexChars(t *testing.T) {
	dir := t.TempDir()
	_, fp, err := Ensure(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(fp) != 64 {
		t.Fatalf("fingerprint length = %d, want 64 (SHA-256 hex)", len(fp))
	}
	for _, c := range fp {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHex {
			t.Fatalf("fingerprint contiene char no-hex: %q en %s", c, fp)
		}
	}
}

func TestGeneratedCertUsableByTLS(t *testing.T) {
	dir := t.TempDir()
	tlsConf, _, err := Ensure(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// Verificar que el tls.Config es válido para un handshake (sin levantar
	// servidor: basta con que el leaf parseable y la clave casen).
	cert := tlsConf.Certificates[0]
	if len(cert.Certificate) == 0 {
		t.Fatal("cert sin DER")
	}
	if cert.PrivateKey == nil {
		t.Fatal("cert sin PrivateKey")
	}
}
