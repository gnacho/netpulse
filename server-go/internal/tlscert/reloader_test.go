package tlscert

import (
	"crypto/x509"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// serial devuelve el serial del leaf del cert servido (para distinguir
// versiones del par en disco).
func serial(t *testing.T, r *Reloader) *big.Int {
	t.Helper()
	cert, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parsear leaf: %v", err)
	}
	return leaf.SerialNumber
}

func TestReloaderServesInitialCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if _, _, err := Ensure(certPath, keyPath); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	r, err := NewReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}
	conf := r.Config()
	if conf.GetCertificate == nil {
		t.Fatal("Config sin GetCertificate")
	}
	if len(conf.Certificates) != 0 {
		t.Fatal("Config con Certificates estático (debe ser GetCertificate)")
	}
	if serial(t, r).Sign() == 0 {
		t.Fatal("serial del cert inicial vacío")
	}
}

func TestReloaderMissingPairFails(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewReloader(filepath.Join(dir, "no.pem"), filepath.Join(dir, "no.key")); err == nil {
		t.Fatal("NewReloader con par inexistente debe fallar (no genera autofirmado)")
	}
}

func TestReloaderReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if _, _, err := Ensure(certPath, keyPath); err != nil {
		t.Fatalf("Ensure A: %v", err)
	}
	r, err := NewReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}
	serialA := serial(t, r)

	// Sustituir el par por uno nuevo (mismo escenario que una renovación
	// acme.sh/certbot sobre las mismas rutas).
	time.Sleep(10 * time.Millisecond) // asegurar mtime distinto
	dirB := t.TempDir()
	certB := filepath.Join(dirB, "cert.pem")
	keyB := filepath.Join(dirB, "key.pem")
	if _, _, err := Ensure(certB, keyB); err != nil {
		t.Fatalf("Ensure B: %v", err)
	}
	copyFile(t, certB, certPath)
	copyFile(t, keyB, keyPath)

	if s := serial(t, r); s.Cmp(serialA) == 0 {
		t.Fatal("el cert servido no cambió tras renovar los ficheros (issue #769)")
	}
}

func TestReloaderKeepsLastGoodOnBrokenFile(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if _, _, err := Ensure(certPath, keyPath); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	r, err := NewReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}
	good := serial(t, r)

	// Cert a medio escribir (renovación en curso): debe seguir sirviendo el
	// último par válido, sin error.
	if err := os.WriteFile(certPath, []byte("-----BEGIN CERTIFICATE-----\ngarbage\n"), 0o644); err != nil {
		t.Fatalf("escribir cert roto: %v", err)
	}
	if s := serial(t, r); s.Cmp(good) != 0 {
		t.Fatal("con el cert a medio escribir debe servirse el último par válido")
	}

	// Y al completarse la renovación (fichero válido + mtime nuevo), el
	// siguiente handshake ya sirve el par nuevo. Se limpia ultimoFallo para
	// no esperar el backoff de 30 s del fallo anterior (test en paquete).
	time.Sleep(10 * time.Millisecond)
	r.ultimoFallo = time.Time{}
	dirB := t.TempDir()
	certB := filepath.Join(dirB, "cert.pem")
	keyB := filepath.Join(dirB, "key.pem")
	if _, _, err := Ensure(certB, keyB); err != nil {
		t.Fatalf("Ensure B: %v", err)
	}
	copyFile(t, certB, certPath)
	copyFile(t, keyB, keyPath)
	if s := serial(t, r); s.Cmp(good) == 0 {
		t.Fatal("tras completar la renovación debe servirse el par nuevo")
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("leer %s: %v", src, err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatalf("escribir %s: %v", dst, err)
	}
}

// FORK: the fingerprint follows a renewal that changes the key, since agents
// are handed it at pairing.
func TestReloaderFingerprintFollowsARenewal(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	_, first, err := Ensure(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReloader(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Fingerprint(); got != first {
		t.Fatalf("fingerprint = %q, want the served key %q", got, first)
	}

	other := t.TempDir()
	_, second, err := Ensure(filepath.Join(other, "c.pem"), filepath.Join(other, "k.pem"))
	if err != nil {
		t.Fatal(err)
	}
	copyFile(t, filepath.Join(other, "c.pem"), certPath)
	copyFile(t, filepath.Join(other, "k.pem"), keyPath)
	future := time.Now().Add(time.Minute)
	for _, p := range []string{certPath, keyPath} {
		if err := os.Chtimes(p, future, future); err != nil {
			t.Fatal(err)
		}
	}
	if got := r.Fingerprint(); got != second || got == first {
		t.Fatalf("after renewal fingerprint = %q, want the new key %q", got, second)
	}
}
