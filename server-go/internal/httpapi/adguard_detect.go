// adguard_detect.go - POST /api/config/adguard/detect (#964): autodetección
// de PRESENCIA de AdGuard Home (no credenciales; el usuario confirma la
// password después). Sondea :3000/control/status primero en el gateway
// (mode=glinet, source=gateway) y luego en la flota (mode=standard).
package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// adguardDetectResponse es la forma JSON del detect: found=false si ningún
// host de la flota sirve la API de AdGuard Home.
type adguardDetectResponse struct {
	Found  bool   `json:"found"`
	Mode   string `json:"mode,omitempty"`   // "glinet" | "standard"
	Host   string `json:"host,omitempty"`
	Port   int    `json:"port,omitempty"`
	Source string `json:"source,omitempty"` // "gateway" | "fleet"
}

// adguardStatusProbe pregunta a /control/status de un host (timeout 2s).
// Variable de paquete para inyectar un fake en tests.
var adguardStatusProbe = probeAdguardStatus

// probeAdguardStatus detecta PRESENCIA: /control/status responde 200 sin
// credenciales con un JSON que lleva dns_port/version. Cualquier otra cosa
// (connection refused, timeout, 404 de otro servidor en :3000) = no está.
func probeAdguardStatus(ctx context.Context, host string, port int) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://%s:%d/control/status", host, port), nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 8192))
	if err != nil {
		return false
	}
	return strings.Contains(string(body), `"dns_port"`) || strings.Contains(string(body), `"version"`)
}

// handleDetectAdguard: gateway primero (GL.iNet lleva AdGuard Home propio en
// :3000), luego el resto de la flota en orden. El primer hallazgo gana.
func (s *server) handleDetectAdguard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	routers := routerstore.ListRouters(s.db.DB)
	const port = 3000
	for _, pass := range []struct {
		mode, source string
		gateway      bool
	}{
		{"glinet", "gateway", true},
		{"standard", "fleet", false},
	} {
		for _, rt := range routers {
			if rt.IsGateway != pass.gateway || rt.Host == "" {
				continue
			}
			if adguardStatusProbe(ctx, rt.Host, port) {
				writeJSON(w, http.StatusOK, adguardDetectResponse{
					Found: true, Mode: pass.mode, Host: rt.Host, Port: port, Source: pass.source,
				})
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, adguardDetectResponse{Found: false})
}
