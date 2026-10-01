// routeros_live.go — sondeo nativo de routers MikroTik RouterOS.
//
// A diferencia de OpenWrt (SSH + ubus) o SNMP (switches gestionados sin
// vitals), RouterOS 7+ expone una REST API HTTP directamente
// (https://<host>/rest/...): se sondea desde el servidor sin SSH ni agente.
// Router de primera clase igual que OpenWrt (Type "routeros" en vez de
// "openwrt"/"glinet"): tiene vitals reales (CPU/RAM/uptime), a diferencia de
// SNMP/external que van con VitalsAvailable=false (#441). Lo que SÍ queda
// ausente por ahora son las features específicas de ubus (radios, DHCP,
// LLDP, WireGuard...) — igual degradación amable que el resto de tipos no
// completos del contrato Router.
package adapters

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

type routerOSResource struct {
	CPULoad      string `json:"cpu-load"`
	FreeMemory   string `json:"free-memory"`
	TotalMemory  string `json:"total-memory"`
	Uptime       string `json:"uptime"`
	Version      string `json:"version"`
	BoardName    string `json:"board-name"`
	Architecture string `json:"architecture-name"`
}

type routerOSIdentity struct {
	Name string `json:"name"`
}

var routerOSDurationRe = regexp.MustCompile(`(\d+)([wdhms])`)

// parseRouterOSUptime convierte el uptime de RouterOS ("1w2d3h4m5s") a
// segundos. ponytail: mismo parser que agent/cmd/routeros-pusher, duplicado
// a propósito (módulos Go distintos, sin paquete compartido todavía) —
// factorizar si aparece un tercer consumidor.
func parseRouterOSUptime(s string) (float64, error) {
	matches := routerOSDurationRe.FindAllStringSubmatch(s, -1)
	if matches == nil {
		return 0, fmt.Errorf("routeros: unparseable uptime %q", s)
	}
	unitSeconds := map[string]float64{"w": 604800, "d": 86400, "h": 3600, "m": 60, "s": 1}
	var total float64
	for _, m := range matches {
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, fmt.Errorf("routeros: bad uptime component %q: %w", m[0], err)
		}
		total += n * unitSeconds[m[2]]
	}
	return total, nil
}

func routerOSFetch(ctx context.Context, client *http.Client, baseURL, user, pass, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return fmt.Errorf("routeros: GET %s: %s: %s", path, resp.Status, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// buildRouterOSPoll mapea resource+identity de RouterOS al routerPolled que
// consume buildRouter. Separado de pollRouterROS para poder testear el
// mapeo sin un servidor HTTP real.
func buildRouterOSPoll(cfg RouterConfig, res routerOSResource, id routerOSIdentity) (*routerPolled, error) {
	cpuLoad, err := strconv.Atoi(res.CPULoad)
	if err != nil {
		return nil, fmt.Errorf("routeros %s: bad cpu-load %q: %w", cfg.ID, res.CPULoad, err)
	}
	uptime, err := parseRouterOSUptime(res.Uptime)
	if err != nil {
		return nil, fmt.Errorf("routeros %s: %w", cfg.ID, err)
	}
	total, _ := strconv.ParseFloat(res.TotalMemory, 64)
	free, _ := strconv.ParseFloat(res.FreeMemory, 64)
	ramPct := 0
	if total > 0 {
		ramPct = int((total - free) / total * 100)
	}

	board := &BoardInfo{
		Model:     res.BoardName,
		Hostname:  id.Name,
		System:    "routeros",
		BoardName: res.BoardName,
	}
	board.Release.Version = res.Version
	board.Release.Target = res.Architecture

	return &routerPolled{
		cfg:       cfg,
		cpu:       cpuLoad,
		ram:       ramPct,
		uptimeSec: uptime,
		board:     board,
		polledAt:  time.Now().UnixMilli(),
	}, nil
}

// pollRouterROS sondea un router RouterOS. Prueba primero el API binario
// nativo (routeros_api.go, puerto 8728) — funciona aunque www/www-ssl estén
// cerrados, que es justo la postura de seguridad habitual en RouterOS (el
// admin expone `api`/`winbox` para gestión, no un panel web). Si el API
// falla (deshabilitado, IP no permitida en su ACL), cae a REST como segundo
// intento. ponytail: reintenta ambos caminos en CADA tick sin cachear cuál
// funcionó — ambos fallan rápido (dial rechazado, no timeout); cachear
// por-router si esto llega a pesar en la práctica.
func (l *Live) pollRouterROS(cfg RouterConfig) (*routerPolled, error) {
	if p, err := l.pollRouterROSAPI(cfg); err == nil {
		return p, nil
	} else if pRest, errRest := l.pollRouterROSRest(cfg); errRest == nil {
		return pRest, nil
	} else {
		return nil, fmt.Errorf("routeros api: %w; rest: %v", err, errRest)
	}
}

// pollRouterROSRest sondea un router RouterOS vía su REST API. Sin caché de
// intervalo (a diferencia de SNMP, #414): dos GET ligeros por tick, mismo
// coste que el sondeo SSH de OpenWrt.
//
// Prueba https primero y cae a http si la conexión es rechazada: muchos
// RouterOS domésticos solo tienen el servicio `www` (HTTP) activo, no
// `www-ssl`.
func (l *Live) pollRouterROSRest(cfg RouterConfig) (*routerPolled, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	if cfg.RouterOSInsecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // opt-in, RouterOS de fábrica trae certs autofirmados
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var res routerOSResource
	var id routerOSIdentity
	var lastErr error
	for _, scheme := range [...]string{"https", "http"} {
		base := scheme + "://" + cfg.Host
		if err := routerOSFetch(ctx, client, base, cfg.RouterOSUser, cfg.RouterOSPassword, "/rest/system/resource", &res); err != nil {
			lastErr = err
			continue
		}
		if err := routerOSFetch(ctx, client, base, cfg.RouterOSUser, cfg.RouterOSPassword, "/rest/system/identity", &id); err != nil {
			lastErr = err
			continue
		}
		return buildRouterOSPoll(cfg, res, id)
	}
	return nil, lastErr
}
