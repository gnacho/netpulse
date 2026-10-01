// proxmox_detect.go - POST /api/config/proxmox/detect (#967): descubrimiento
// de hosts Proxmox VE en la /24 de los hosts configurados en `routers`.
//
// DOS FASES, y el orden importa: pveproxy LIMITA las conexiones simultaneas
// por IP de origen, así que un barrido TLS paralelo mata TODAS las conexiones
// (context deadline exceeded incluso contra hosts sanos). Por eso:
//   - Fase 1: TCP connect masivo al :8006 (64 workers, 400ms) para filtrar
//     los que aceptan el puerto (RST instantaneo o timeout ARP de 400ms).
//   - Fase 2: confirmacion TLS GET /api2/json/version EN SERIE (5s), valida
//     si el header Server es "pve-api-daemon" o el body contiene "pve".
package httpapi

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// proxmoxDetectResponse es la forma JSON del detect: la lista de hosts
// confirmados como PVE (puede estar vacia).
type proxmoxDetectResponse struct {
	Found bool     `json:"found"`
	Hosts []string `json:"hosts"`
}

// Constantes del barrido (#967): los valores vienen de la validacion en vivo
// (fase 1 ~1,6s por /24; fase 2 una conexion cada vez, siempre).
const (
	pveDetectPort        = 8006
	pveDetectTCPWorkers  = 64
	pveDetectTCPTimeout  = 400 * time.Millisecond
	pveDetectTLSTimeout  = 5 * time.Second
	pveDetectOverallCap  = 3 * time.Minute
	pveDetectTLSMaxBytes = 8192
)

// Sondas inyectables en tests.
var (
	pveTCPProbe   = tcpAcceptsPVE
	pveTLSConfirm = confirmPVEHost
)

// tcpAcceptsPVE: el host acepta TCP en el puerto (fase 1, barata y paralela).
func tcpAcceptsPVE(host string, port int, timeout time.Duration) bool {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// confirmPVEHost: GET TLS /api2/json/version (fase 2, EN SERIE). Valido si
// el header Server es "pve-api-daemon" o el body contiene "pve". El cert es
// autofirmado en cualquier PVE real (mismo criterio que pve.NewClient).
func confirmPVEHost(host string, port int, timeout time.Duration) bool {
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // #561
	client := &http.Client{Transport: tr, Timeout: timeout}
	res, err := client.Get(fmt.Sprintf("https://%s/api2/json/version", net.JoinHostPort(host, fmt.Sprintf("%d", port))))
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if strings.Contains(res.Header.Get("Server"), "pve-api-daemon") {
		return true
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, pveDetectTLSMaxBytes))
	if err != nil {
		return false
	}
	return strings.Contains(string(body), "pve")
}

// scanSubnetPVE: fase 1 - TCP connect masivo a toda la /24 en el puerto PVE.
func scanSubnetPVE(subnet string) []string {
	jobs := make(chan string)
	var mu sync.Mutex
	found := []string{}
	var wg sync.WaitGroup
	for w := 0; w < pveDetectTCPWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				if pveTCPProbe(ip, pveDetectPort, pveDetectTCPTimeout) {
					mu.Lock()
					found = append(found, ip)
					mu.Unlock()
				}
			}
		}()
	}
	for i := 1; i <= 254; i++ {
		jobs <- fmt.Sprintf("%s.%d", subnet, i)
	}
	close(jobs)
	wg.Wait()
	sort.Strings(found)
	return found
}

// handleDetectProxmox: escanea la /24 de cada host configurado en `routers`
// y devuelve los hosts confirmados como Proxmox VE.
func (s *server) handleDetectProxmox(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), pveDetectOverallCap)
	defer cancel()
	subnets := map[string]bool{}
	for _, rt := range routerstore.ListRouters(s.db.DB) {
		ip := net.ParseIP(strings.TrimSpace(rt.Host))
		if ip == nil || ip.To4() == nil {
			continue
		}
		v4 := ip.To4()
		subnets[fmt.Sprintf("%d.%d.%d", v4[0], v4[1], v4[2])] = true
	}
	confirmed := []string{}
	for subnet := range subnets {
		if ctx.Err() != nil {
			break
		}
		for _, ip := range scanSubnetPVE(subnet) {
			if ctx.Err() != nil {
				break
			}
			// Fase 2 EN SERIE: pveproxy muere con TLS paralelo (ver cabecera).
			if pveTLSConfirm(ip, pveDetectPort, pveDetectTLSTimeout) {
				confirmed = append(confirmed, ip)
			}
		}
	}
	sort.Strings(confirmed)
	writeJSON(w, http.StatusOK, proxmoxDetectResponse{Found: len(confirmed) > 0, Hosts: confirmed})
}
