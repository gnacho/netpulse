// Package config — configuración por entorno, validada (fail-fast).
// Paridad con server/src/config.js: parser .env propio (no pisa el entorno),
// mismas variables, defaults y validaciones (SPEC §8.1).
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RouterSeed es una entrada de ROUTERS_JSON (semilla opcional de la tabla routers).
type RouterSeed struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
	Type string `json:"type"` // "glinet" | "openwrt" (default "openwrt")
}

// AdGuard configura el cliente estándar (env). Nil si no hay ADGUARD_URL.
type AdGuard struct {
	URL  string
	User string
	Pass string
}

// Config es la config normalizada (equivalente al objeto de loadConfig).
type Config struct {
	Port          int
	NodeEnv       string
	StaticDir     string // resuelta (absoluta)
	DataDir       string // resuelta (absoluta)
	SessionSecret string // "" si falta (autogenerado en kv)
	AuthUser      string
	AuthPass      string
	DemoMode      bool
	MaxSSEClients int
	Routers       []RouterSeed
	SSHKeyPath    string
	Adguard       *AdGuard
	WGInterface   string
	CookieSecure  string // "auto" | "always" | "never"
	GithubRepo    string
	GithubToken   string
	ServerRoot    string
	AutoRearm     bool // NETPULSE_AUTO_REARM=1: supervisor rearma agentes caídos
}

// LoadDotEnv parsea un .env: KEY=VALUE por línea, '#' comentarios (línea
// completa), comillas simples/dobles opcionales. NO pisa variables ya
// presentes en el entorno (paridad con config.js:13-27).
func LoadDotEnv(envPath string) {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq == -1 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
}

type issue struct {
	path, message string
}

// ValidationError es el error fail-fast de configuración (mismo formato que
// config.js:67-70).
type ValidationError struct{ issues []issue }

func (e *ValidationError) Error() string {
	var b strings.Builder
	b.WriteString("[netpulse] Configuración inválida (revisa server/.env):")
	for _, i := range e.issues {
		fmt.Fprintf(&b, "\n  - %s: %s", i.path, i.message)
	}
	return b.String()
}

// Load valida el entorno y devuelve la config normalizada.
// env es un mapa key→value (normalmente leído de os.Environ); serverRoot es
// el directorio base para resolver STATIC_DIR/DATA_DIR relativos (en Node es
// server/; en Go el working directory del proceso — ver cmd/netpulse).
// Fail-fast: devuelve error si falta algo crítico o ROUTERS_JSON es inválido.
func Load(env map[string]string, serverRoot string) (*Config, error) {
	var errs ValidationError

	// PORT: int 1..65535, default 3000
	port := 3000
	if v, ok := env["PORT"]; ok && v != "" {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 1 || n > 65535 {
			errs.issues = append(errs.issues, issue{"PORT", "Expected int 1..65535"})
		} else {
			port = n
		}
	}

	// NODE_ENV: development|production|test, default development
	nodeEnv := "development"
	if v, ok := env["NODE_ENV"]; ok && v != "" {
		switch v {
		case "development", "production", "test":
			nodeEnv = v
		default:
			errs.issues = append(errs.issues, issue{"NODE_ENV", "Invalid enum value. Expected 'development' | 'production' | 'test'"})
		}
	}

	staticDir := "../app/dist"
	if v, ok := env["STATIC_DIR"]; ok && v != "" {
		staticDir = v
	}
	dataDir := "./data"
	if v, ok := env["DATA_DIR"]; ok && v != "" {
		dataDir = v
	}

	// SESSION_SECRET: min 32, opcional
	sessionSecret := ""
	if v, ok := env["SESSION_SECRET"]; ok && v != "" {
		if len(v) < 32 {
			errs.issues = append(errs.issues, issue{"SESSION_SECRET", "String must contain at least 32 character(s)"})
		} else {
			sessionSecret = v
		}
	}

	authUser := "admin"
	if v, ok := env["AUTH_USER"]; ok && v != "" {
		authUser = v
	}

	// AUTH_PASS: obligatoria (fail-fast) aunque ya existan usuarios
	authPass := ""
	if v, ok := env["AUTH_PASS"]; ok && v != "" {
		authPass = v
	} else {
		errs.issues = append(errs.issues, issue{"AUTH_PASS", "Required"})
	}

	// DEMO_MODE: '0'|'1', opcional (demo solo si === '1')
	demoMode := false
	if v, ok := env["DEMO_MODE"]; ok && v != "" {
		switch v {
		case "0":
		case "1":
			demoMode = true
		default:
			errs.issues = append(errs.issues, issue{"DEMO_MODE", "Invalid enum value. Expected '0' | '1'"})
		}
	}

	// NETPULSE_AUTO_REARM: '0'|'1', opcional (solo rearma si === '1').
	// Opt-in explícito: nada autónomo sobre equipamiento de red sin
	// confirmación del usuario (regla Fase 8).
	autoRearm := false
	if v, ok := env["NETPULSE_AUTO_REARM"]; ok && v != "" {
		switch v {
		case "0":
		case "1":
			autoRearm = true
		default:
			errs.issues = append(errs.issues, issue{"NETPULSE_AUTO_REARM", "Invalid enum value. Expected '0' | '1'"})
		}
	}

	// MAX_SSE_CLIENTS: int 1..100, default 10
	maxSSE := 10
	if v, ok := env["MAX_SSE_CLIENTS"]; ok && v != "" {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 1 || n > 100 {
			errs.issues = append(errs.issues, issue{"MAX_SSE_CLIENTS", "Expected int 1..100"})
		} else {
			maxSSE = n
		}
	}

	// COOKIE_SECURE: auto|always|never, default auto
	cookieSecure := "auto"
	if v, ok := env["COOKIE_SECURE"]; ok && v != "" {
		switch v {
		case "auto", "always", "never":
			cookieSecure = v
		default:
			errs.issues = append(errs.issues, issue{"COOKIE_SECURE", "Invalid enum value. Expected 'auto' | 'always' | 'never'"})
		}
	}

	wgInterface := "wg0"
	if v, ok := env["WG_INTERFACE"]; ok && v != "" {
		wgInterface = v
	}

	githubRepo := "gnacho/netpulse"
	if v, ok := env["GITHUB_REPO"]; ok && v != "" {
		githubRepo = v
	}
	githubToken := env["GITHUB_TOKEN"]

	// ADGUARD_*: cliente estándar solo si hay URL válida
	var adguard *AdGuard
	if v, ok := env["ADGUARD_URL"]; ok && v != "" {
		if u, err := url.Parse(v); err != nil || u.Scheme == "" || u.Host == "" {
			errs.issues = append(errs.issues, issue{"ADGUARD_URL", "Invalid url"})
		} else {
			user := "admin"
			if w, ok := env["ADGUARD_USER"]; ok && w != "" {
				user = w
			}
			adguard = &AdGuard{URL: strings.TrimSuffix(v, "/"), User: user, Pass: env["ADGUARD_PASS"]}
		}
	}

	if len(errs.issues) > 0 {
		return nil, &errs
	}

	// ROUTERS_JSON: array de {id, name?, host, type?}; inválido → fail-fast
	var routers []RouterSeed
	if v, ok := env["ROUTERS_JSON"]; ok && v != "" {
		var raw []RouterSeed
		if err := json.Unmarshal([]byte(v), &raw); err != nil {
			return nil, fmt.Errorf("[netpulse] ROUTERS_JSON inválido: %v", err)
		}
		for i, r := range raw {
			if r.ID == "" || r.Host == "" {
				return nil, fmt.Errorf("[netpulse] ROUTERS_JSON inválido: entrada %d requiere id y host", i)
			}
			if r.Type == "" {
				raw[i].Type = "openwrt"
			} else if r.Type != "glinet" && r.Type != "openwrt" {
				return nil, fmt.Errorf("[netpulse] ROUTERS_JSON inválido: type debe ser glinet|openwrt")
			}
		}
		routers = raw
	}

	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Join(serverRoot, p)
	}

	sshKeyPath := ""
	if v, ok := env["SSH_KEY_PATH"]; ok && v != "" {
		home, _ := os.UserHomeDir()
		if strings.HasPrefix(v, "~") {
			v = home + v[1:]
		}
		sshKeyPath = v
	} else {
		sshKeyPath = filepath.Join(resolve(dataDir), ".ssh", "id_ed25519")
	}

	return &Config{
		Port:          port,
		NodeEnv:       nodeEnv,
		StaticDir:     resolve(staticDir),
		DataDir:       resolve(dataDir),
		SessionSecret: sessionSecret,
		AuthUser:      authUser,
		AuthPass:      authPass,
		DemoMode:      demoMode,
		MaxSSEClients: maxSSE,
		Routers:       routers,
		SSHKeyPath:    sshKeyPath,
		Adguard:       adguard,
		WGInterface:   wgInterface,
		CookieSecure:  cookieSecure,
		GithubRepo:    githubRepo,
		GithubToken:   githubToken,
		ServerRoot:    serverRoot,
		AutoRearm:     autoRearm,
	}, nil
}

// FromEnviron construye el mapa de entorno del proceso.
func FromEnviron() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if eq := strings.Index(kv, "="); eq > 0 {
			env[kv[:eq]] = kv[eq+1:]
		}
	}
	return env
}
