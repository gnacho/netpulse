package tlscert

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"sync"
	"time"
)

// reintentoTrasFallo: tras un intento de recarga fallido (p. ej. acme.sh a
// medio de escribir el par), no se vuelve a tocar disco hasta pasado este
// tiempo, para no martillear el filesystem en cada handshake.
const reintentoTrasFallo = 30 * time.Second

// Reloader sirve el par cert/clave de un usuario (#769) recargándolo del
// disco cuando cambia: GetCertificate repuebla la caché si el mtime de
// alguno de los dos ficheros avanza. Si la recarga falla, sigue sirviendo el
// último par válido (una renovación a medio escribir no debe tumbar el
// listener). El path on-box autofirmado NO usa esto: el agente fija el SPKI
// en el pairing y ahí la recarga en caliente rompería el pin.
type Reloader struct {
	certPath string
	keyPath  string

	mu          sync.Mutex
	cert        *tls.Certificate
	mtime       time.Time
	ultimoFallo time.Time
}

// NewReloader carga el par inicial; si no existe o no parsea, devuelve error
// (mismo contrato que Load: nunca genera un autofirmado en rutas del usuario).
func NewReloader(certPath, keyPath string) (*Reloader, error) {
	r := &Reloader{certPath: certPath, keyPath: keyPath}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Config devuelve el tls.Config del listener con GetCertificate (recarga en
// caliente por mtime) en lugar de una lista estática de Certificates.
func (r *Reloader) Config() *tls.Config {
	return &tls.Config{
		GetCertificate: r.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}
}

// GetCertificate implementa tls.Config.GetCertificate: sirve la caché y, si
// el mtime de algún fichero avanzó, repuebla desde disco antes.
func (r *Reloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mt, err := maxMtime(r.certPath, r.keyPath)
	if err != nil {
		return r.cert, nil // stat fallando: servir lo cacheado
	}
	if mt.After(r.mtime) && time.Since(r.ultimoFallo) > reintentoTrasFallo {
		if err := r.reload(); err != nil {
			r.ultimoFallo = time.Now() // par a medio escribir: reintentar luego
			return r.cert, nil
		}
	}
	return r.cert, nil
}

// Fingerprint is the SPKI fingerprint of the certificate being served now,
// after picking up a renewal as a handshake would. It is read live because
// renewals often change the key (certbot issues a new one unless told
// --reuse-key), and a value captured at start-up would hand agents a pin
// the server no longer matches. Empty if the served pair cannot be parsed.
func (r *Reloader) Fingerprint() string {
	cert, _ := r.GetCertificate(nil)
	if cert == nil || len(cert.Certificate) == 0 {
		return ""
	}
	leaf := cert.Leaf
	if leaf == nil {
		var err error
		if leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return ""
		}
	}
	return Fingerprint(leaf)
}

// reload repuebla la caché desde disco. Debe llamarse con r.mu tomado.
func (r *Reloader) reload() error {
	cert, err := tls.LoadX509KeyPair(r.certPath, r.keyPath)
	if err != nil {
		return err
	}
	mt, err := maxMtime(r.certPath, r.keyPath)
	if err != nil {
		return err
	}
	r.cert = &cert
	r.mtime = mt
	r.ultimoFallo = time.Time{}
	return nil
}

// maxMtime devuelve el mtime más reciente de los dos ficheros.
func maxMtime(certPath, keyPath string) (time.Time, error) {
	sc, err := os.Stat(certPath)
	if err != nil {
		return time.Time{}, err
	}
	sk, err := os.Stat(keyPath)
	if err != nil {
		return time.Time{}, err
	}
	if sk.ModTime().After(sc.ModTime()) {
		return sk.ModTime(), nil
	}
	return sc.ModTime(), nil
}
