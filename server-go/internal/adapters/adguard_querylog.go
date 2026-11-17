// adguard_querylog.go — lectura del query log de AdGuard Home (#2078).
//
// El query log es la única fuente que atribuye dominios a dispositivos. Se lee
// de forma incremental (older_than) y se agrega en el servidor: nunca se guarda
// el log crudo. Este fichero solo normaliza la API a un contrato interno.

package adapters

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DNSQuery es una entrada del query log normalizada.
type DNSQuery struct {
	Time        time.Time
	ClientIP    string
	ClientName  string
	Domain      string
	QType       string
	Status      string
	Reason      string
	ServiceName string
	Rule        string
	Proto       string
}

// BlockedByFilter distingue un bloqueo real de una reescritura: Rewrite y
// FilteredSafeSearch reescriben la respuesta, no la deniegan.
func (q DNSQuery) BlockedByFilter() bool {
	switch q.Reason {
	case "FilteredBlackList", "FilteredSafeBrowsing", "FilteredParental", "FilteredBlockedService":
		return true
	}
	return false
}

// QueryLogPage es una página del log, de la más reciente a la más antigua.
type QueryLogPage struct {
	Oldest  time.Time
	Queries []DNSQuery
}

// QueryLogConfig es la configuración del log que cambia su utilidad:
// deshabilitado o con IPs anonimizadas no hay atribución posible.
type QueryLogConfig struct {
	Enabled           bool
	AnonymizeClientIP bool
	IntervalMS        float64
}

// agQueryLogRaw es la forma JSON de /control/querylog.
type agQueryLogRaw struct {
	Oldest string `json:"oldest"`
	Data   []struct {
		Time        string `json:"time"`
		Client      string `json:"client"`
		ClientProto string `json:"client_proto"`
		ClientInfo  struct {
			Name string `json:"name"`
		} `json:"client_info"`
		Question struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"question"`
		Status      string `json:"status"`
		Reason      string `json:"reason"`
		ServiceName string `json:"service_name"`
		Rules       []struct {
			Text string `json:"text"`
		} `json:"rules"`
	} `json:"data"`
}

// parseQueryLog normaliza una respuesta cruda. Entradas sin dominio se
// descartan: no aportan nada a una vista por dominio.
func parseQueryLog(raw *agQueryLogRaw) *QueryLogPage {
	page := &QueryLogPage{Queries: []DNSQuery{}}
	if raw == nil {
		return page
	}
	if ts, err := time.Parse(time.RFC3339, raw.Oldest); err == nil {
		page.Oldest = ts
	}
	for _, e := range raw.Data {
		domain := normalizeDomain(e.Question.Name)
		if domain == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.Time)
		if err != nil {
			continue
		}
		q := DNSQuery{
			Time:        ts,
			ClientIP:    strings.TrimSpace(e.Client),
			ClientName:  e.ClientInfo.Name,
			Domain:      domain,
			QType:       e.Question.Type,
			Status:      e.Status,
			Reason:      e.Reason,
			ServiceName: e.ServiceName,
			Proto:       e.ClientProto,
		}
		if len(e.Rules) > 0 {
			q.Rule = e.Rules[0].Text
		}
		page.Queries = append(page.Queries, q)
	}
	return page
}

// normalizeDomain pasa a minúsculas y quita el punto final de la raíz.
func normalizeDomain(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// QueryLog lee una página del log, opcionalmente más antigua que olderThan.
func (c *AdGuardClient) QueryLog(ctx context.Context, olderThan string, limit int) (*QueryLogPage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	if olderThan != "" {
		q.Set("older_than", olderThan)
	}
	ctx, cancel := context.WithTimeout(ctx, adguardHTTPTimeout)
	defer cancel()
	var raw agQueryLogRaw
	if err := c.get(ctx, "/control/querylog?"+q.Encode(), &raw); err != nil {
		return nil, err
	}
	return parseQueryLog(&raw), nil
}

// QueryLogConfig lee la configuración del query log.
func (c *AdGuardClient) QueryLogConfig(ctx context.Context) (*QueryLogConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, adguardHTTPTimeout)
	defer cancel()
	var cfg struct {
		Enabled           bool    `json:"enabled"`
		AnonymizeClientIP bool    `json:"anonymize_client_ip"`
		Interval          float64 `json:"interval"`
	}
	if err := c.get(ctx, "/control/querylog/config", &cfg); err != nil {
		return nil, err
	}
	return &QueryLogConfig{Enabled: cfg.Enabled, AnonymizeClientIP: cfg.AnonymizeClientIP, IntervalMS: cfg.Interval}, nil
}

// QueryLog lee una página del log con login GL y re-login ante 401.
func (c *AdGuardGlinetClient) QueryLog(ctx context.Context, olderThan string, limit int) (*QueryLogPage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	if olderThan != "" {
		q.Set("older_than", olderThan)
	}
	ctx, cancel := context.WithTimeout(ctx, glHTTPTimeout+glSSHTimeout)
	defer cancel()
	var raw agQueryLogRaw
	if err := c.glGetWithLogin(ctx, "querylog?"+q.Encode(), &raw); err != nil {
		return nil, err
	}
	return parseQueryLog(&raw), nil
}

// QueryLogConfig lee la configuración del log con login GL. En firmwares GL
// antiguos el endpoint no existe: el caller lo trata como "desconocida".
func (c *AdGuardGlinetClient) QueryLogConfig(ctx context.Context) (*QueryLogConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, glHTTPTimeout+glSSHTimeout)
	defer cancel()
	var cfg struct {
		Enabled           bool    `json:"enabled"`
		AnonymizeClientIP bool    `json:"anonymize_client_ip"`
		Interval          float64 `json:"interval"`
	}
	if err := c.glGetWithLogin(ctx, "querylog/config", &cfg); err != nil {
		return nil, err
	}
	return &QueryLogConfig{Enabled: cfg.Enabled, AnonymizeClientIP: cfg.AnonymizeClientIP, IntervalMS: cfg.Interval}, nil
}

// glGetWithLogin ejecuta glGet con el sid actual y re-login ante 401 (mismo
// patrón que GetStats y QueryClients).
func (c *AdGuardGlinetClient) glGetWithLogin(ctx context.Context, path string, dst any) error {
	c.mu.Lock()
	hasSid := c.sid != ""
	c.mu.Unlock()
	if !hasSid {
		if _, err := c.Login(); err != nil {
			return err
		}
	}
	err := c.glGet(ctx, path, dst)
	if err != errGL401 {
		return err
	}
	c.mu.Lock()
	c.sid = ""
	c.mu.Unlock()
	if _, lerr := c.Login(); lerr != nil {
		return lerr
	}
	return c.glGet(ctx, path, dst)
}

// AdGuardSource implementa la lectura del query log sobre el cliente que haya
// configurado (estándar o GL), sin que el ingestor conozca las dos variantes.
type AdGuardSource struct {
	Std *AdGuardClient
	GL  *AdGuardGlinetClient
}

// QueryLog delega en el cliente configurado.
func (s AdGuardSource) QueryLog(ctx context.Context, olderThan string, limit int) (*QueryLogPage, error) {
	if s.Std != nil {
		return s.Std.QueryLog(ctx, olderThan, limit)
	}
	if s.GL != nil {
		return s.GL.QueryLog(ctx, olderThan, limit)
	}
	return nil, ErrNoAdguard
}

// QueryLogConfig delega en el cliente configurado.
func (s AdGuardSource) QueryLogConfig(ctx context.Context) (*QueryLogConfig, error) {
	if s.Std != nil {
		return s.Std.QueryLogConfig(ctx)
	}
	if s.GL != nil {
		return s.GL.QueryLogConfig(ctx)
	}
	return nil, ErrNoAdguard
}

// ErrNoAdguard: no hay integración configurada.
var ErrNoAdguard = errNoAdguard()

type errNoAdguardType struct{}

func (errNoAdguardType) Error() string { return "AdGuard no configurado" }

func errNoAdguard() error { return errNoAdguardType{} }

// parseQueryLogJSON se mantiene exportado para tests: decodifica un cuerpo
// crudo sin pasar por HTTP.
func parseQueryLogJSON(body []byte) (*QueryLogPage, error) {
	var raw agQueryLogRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return parseQueryLog(&raw), nil
}
