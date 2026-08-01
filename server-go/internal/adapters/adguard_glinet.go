// adguard_glinet.go — Cliente AdGuard Home en firmware GL.iNet (port EXACTO
// de src/adapters/adguard-glinet.js, SPEC §7.4 — algoritmo gl-ngx-session):
//
//   - La API (puerto 3000) exige cookie `Admin-Token` = sesión de la UI GL.
//   - Login GL: challenge (nonce+salt+alg) → hash calculado EN el router vía
//     SSH root (`openssl passwd -<alg> -salt <salt> <pass>` →
//     sha256("<user>:<pw>:<nonce>") → ubus gl-session login) → sid.
//   - La contraseña viaja embebida con escaping de comillas simples (BusyBox
//     del GL NO tiene base64); NUNCA se incluye el comando en los errores.
//   - Cooldown de 15 min tras fallo de login (el GL bloquea tras N intentos).
//   - El sid se cachea y se renueva al primer 401.
package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	glSSHTimeout    = 8 * time.Second
	glHTTPTimeout   = 4 * time.Second
	glLoginCooldown = 15 * time.Minute // el GL bloquea logins tras N fallos
)

// AdGuardGlinetClient es el cliente GL.iNet (host, user, pass + pool SSH).
type AdGuardGlinetClient struct {
	Host string
	User string
	Pass string
	pool *SSHPool

	mu             sync.Mutex
	sid            string
	loginFailUntil time.Time
	hc             *http.Client
}

// NewAdGuardGlinetClient crea el cliente GL (host, user/pass de la UI GL).
func NewAdGuardGlinetClient(host, user, pass string, pool *SSHPool) *AdGuardGlinetClient {
	return &AdGuardGlinetClient{
		Host: host, User: user, Pass: pass, pool: pool,
		hc: &http.Client{Timeout: glHTTPTimeout},
	}
}

// Login ejecuta el flujo gl-ngx-session completo EN el router → sid.
// Literal del JS (adguard-glinet.js:49-81).
func (c *AdGuardGlinetClient) Login() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Cooldown tras fallo: el GL bloquea logins tras N intentos (600 s)
	if time.Now().Before(c.loginFailUntil) {
		return "", fmt.Errorf("login GL en cooldown (%d s)",
			int(math.Ceil(time.Until(c.loginFailUntil).Seconds())))
	}
	// Contraseña embebida con escaping de comillas simples (BusyBox del GL
	// NO tiene base64). El resto del flujo va entre comillas dobles seguro.
	esc := strings.ReplaceAll(c.Pass, `'`, `'\''`)
	script := strings.Join([]string{
		`PASS='` + esc + `'`,
		`RESP=$(ubus call gl-session challenge '{"username":"` + c.User + `"}')`,
		`NONCE=$(jsonfilter -s "$RESP" -e '@.data.nonce')`,
		`SALT=$(jsonfilter -s "$RESP" -e '@.data.salt')`,
		`ALG=$(jsonfilter -s "$RESP" -e '@.data.alg')`,
		`PW=$(openssl passwd -$ALG -salt "$SALT" "$PASS")`,
		`HASH=$(echo -n "` + c.User + `:$PW:$NONCE" | sha256sum | cut -d' ' -f1)`,
		`RESP2=$(ubus call gl-session login '{"username":"` + c.User + `","hash":"'$HASH'"}')`,
		`SID=$(jsonfilter -s "$RESP2" -e '@.data.sid')`,
		`echo "SID:$SID"`,
	}, " && ")
	out, err := c.pool.Run(c.Host, script, glSSHTimeout)
	if err != nil {
		// NUNCA incluir el comando (lleva la contraseña embebida)
		c.loginFailUntil = time.Now().Add(glLoginCooldown)
		return "", err
	}
	m := regexp.MustCompile(`(?m)^SID:(\S+)$`).FindStringSubmatch(out)
	if m == nil {
		c.loginFailUntil = time.Now().Add(glLoginCooldown)
		return "", fmt.Errorf("login GL falló (revisa usuario/contraseña de la UI)")
	}
	c.loginFailUntil = time.Time{}
	c.sid = m[1]
	return c.sid, nil
}

var errGL401 = fmt.Errorf("401 no autorizado")

// glGet: GET http://<host>:3000/control/<path> con cookie Admin-Token.
func (c *AdGuardGlinetClient) glGet(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+c.Host+":3000/control/"+path, nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	sid := c.sid
	c.mu.Unlock()
	req.Header.Set("Cookie", "Admin-Token="+sid)
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == 401 {
		return errGL401
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(dst)
}

// queryStats: /control/stats con el sid actual.
func (c *AdGuardGlinetClient) queryStats(ctx context.Context) (*AdGuardStats, error) {
	var data struct {
		NumDNSQueries       int64           `json:"num_dns_queries"`
		NumBlockedFiltering int64           `json:"num_blocked_filtering"`
		AvgProcessingTime   float64         `json:"avg_processing_time"`
		TopBlockedDomains   json.RawMessage `json:"top_blocked_domains"`
		TopClients          json.RawMessage `json:"top_clients"`
	}
	if err := c.glGet(ctx, "stats", &data); err != nil {
		return nil, err
	}
	total := data.NumDNSQueries
	blocked := data.NumBlockedFiltering
	blockedPct := 0.0
	if total > 0 {
		blockedPct = math.Round(float64(blocked)/float64(total)*100*10) / 10
	}
	clients := 0
	if len(data.TopClients) > 0 {
		var tc []any
		if json.Unmarshal(data.TopClients, &tc) == nil {
			clients = len(tc)
		}
	}
	// GL: top_blocked_domains siempre [{domain: count}] (como el JS)
	top := []TopBlocked{}
	var arr []json.RawMessage
	if json.Unmarshal(data.TopBlockedDomains, &arr) == nil {
		for i, item := range arr {
			if i >= 5 {
				break
			}
			var m map[string]int64
			if json.Unmarshal(item, &m) == nil {
				for k, v := range m {
					top = append(top, TopBlocked{Domain: k, Count: v})
					break
				}
			}
		}
	}
	return &AdGuardStats{
		Host: c.Host, Port: 3000, Status: "active",
		Queries24h: total, Blocked24h: blocked, BlockedPct: blockedPct,
		TrackersBlocked: 0,
		DNSLatencyMs:    int(math.Round(data.AvgProcessingTime * 1000)),
		ClientsUsing:    clients, ClientsTotal: clients,
		TopBlocked:      top,
		FilterLists:     0, Rules: 0,
	}, nil
}

// GetStats: stats con login/relogin automático (401 → re-login una vez).
func (c *AdGuardGlinetClient) GetStats(ctx context.Context) (*AdGuardStats, error) {
	ctx, cancel := context.WithTimeout(ctx, glHTTPTimeout+glSSHTimeout)
	defer cancel()
	c.mu.Lock()
	hasSid := c.sid != ""
	c.mu.Unlock()
	if !hasSid {
		if _, err := c.Login(); err != nil {
			return nil, err
		}
	}
	stats, err := c.queryStats(ctx)
	if err == nil {
		return stats, nil
	}
	if err != errGL401 {
		return nil, err
	}
	c.mu.Lock()
	c.sid = ""
	c.mu.Unlock()
	if _, lerr := c.Login(); lerr != nil {
		return nil, lerr
	}
	return c.queryStats(ctx)
}

// QueryClients: /control/clients con login automático (paridad del JS:
// ante 401 re-login y reintento).
func (c *AdGuardGlinetClient) QueryClients(ctx context.Context) ([]AdguardClient, error) {
	ctx, cancel := context.WithTimeout(ctx, glHTTPTimeout+glSSHTimeout)
	defer cancel()
	c.mu.Lock()
	hasSid := c.sid != ""
	c.mu.Unlock()
	if !hasSid {
		if _, err := c.Login(); err != nil {
			return nil, err
		}
	}
	var data struct {
		Clients []struct {
			Name              string   `json:"name"`
			IDs               []string `json:"ids"`
			UseGlobalSettings bool     `json:"use_global_settings"`
			BlockedServices   []string `json:"blocked_services"`
		} `json:"clients"`
	}
	err := c.glGet(ctx, "clients", &data)
	if err == errGL401 {
		c.mu.Lock()
		c.sid = ""
		c.mu.Unlock()
		if _, lerr := c.Login(); lerr != nil {
			return nil, lerr
		}
		err = c.glGet(ctx, "clients", &data)
	}
	if err != nil {
		return nil, err
	}
	out := make([]AdguardClient, 0, len(data.Clients))
	for _, cl := range data.Clients {
		name := cl.Name
		ip := ""
		if len(cl.IDs) > 0 {
			if name == "" {
				name = cl.IDs[0]
			}
			ip = cl.IDs[0]
		}
		out = append(out, AdguardClient{
			Name: name, IP: ip,
			UseGlobalSettings: cl.UseGlobalSettings,
			BlockedServices:   len(cl.BlockedServices),
		})
	}
	return out, nil
}
