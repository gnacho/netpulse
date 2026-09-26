// fp.go — derivación del SPKI pin (NETPULSE_SERVER_FP) del server cuando el
// TLS lo termina otro frente (proxy, NPM, Caddy): el server se hace un TLS
// dial a su propia URL pública y pinea el hash SHA-256 del
// SubjectPublicKeyInfo del leaf (#851). El modelo de confianza del agente es
// pinning SPKI (tlspin), así que aquí tampoco hace falta validar la cadena:
// solo se extrae la clave pública tal cual la vería el agente.

package reinstall

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// fpPositiveTTL: un cert renovado rota el SPKI con días de antelación
	// (notBefore), no en segundos; 24 h de cache positivo es seguro.
	fpPositiveTTL = 24 * time.Hour
	// fpNegativeTTL corto: si el dial falla (red caída, arranque), reintentar
	// pronto en el siguiente reinstall sin martillear.
	fpNegativeTTL = time.Minute
	fpDialTimeout = 8 * time.Second
)

type fpEntry struct {
	fp     string
	err    bool
	filled time.Time
}

var (
	fpMu    sync.Mutex
	fpCache = map[string]fpEntry{}
	fpNow   = time.Now // inyectable en tests
)

// ServerFP devuelve el SPKI pin (SHA-256 hex, formato de tlspin.Normalize)
// del leaf cert servido en serverURL. Solo para URLs https; para http o si
// el handshake falla devuelve "" (el script se genera entonces sin FP, el
// comportamiento previo a #851). Con cache por URL: positivo 24 h, negativo
// 1 min. Nunca propaga error: el FP es mejora, no requisito del reinstall.
func ServerFP(serverURL string) string {
	u, err := url.Parse(serverURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return ""
	}
	cacheKey := u.Host

	fpMu.Lock()
	if e, ok := fpCache[cacheKey]; ok {
		ttl := fpPositiveTTL
		if e.err {
			ttl = fpNegativeTTL
		}
		if fpNow().Sub(e.filled) < ttl {
			fp := e.fp
			fpMu.Unlock()
			return fp
		}
	}
	fpMu.Unlock()

	fp, dialErr := dialFP(u.Host)

	fpMu.Lock()
	fpCache[cacheKey] = fpEntry{fp: fp, err: dialErr != nil, filled: fpNow()}
	fpMu.Unlock()
	return fp
}

// dialFP hace el TLS dial y calcula el pin. Separada de ServerFP para poder
// stubbearla en tests.
var dialFP = func(host string) (string, error) {
	d := &net.Dialer{Timeout: fpDialTimeout}
	conn, err := tls.DialWithDialer(d, "tcp", host, &tls.Config{
		// InsecureSkipVerify deliberado: el modelo de confianza es el pin SPKI
		// (el mismo que usa el agente en tlspin), no la cadena de CAs.
		InsecureSkipVerify: true, //nolint:gosec // ver comentario
		ServerName:         serverName(host),
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		return "", err
	}
	defer conn.Close()
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return "", nil
	}
	sum := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:]), nil
}

// serverName extrae el SNI del host:port.
func serverName(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
