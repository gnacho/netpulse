// config.go: carga de config del agente desde el entorno del proceso y el
// env file (KEY=VALUE), extraída de cmd/netpulse-agent para que los
// embedders reutilicen el mismo formato y validación.
package runtime

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/internal/tlspin"
)

// DefaultEnvFile: env file por defecto del agente standalone.
const DefaultEnvFile = "/etc/netpulse-agent.env"

// ConfigError: falta una key obligatoria (env o env file).
type ConfigError struct{ Key string }

func (e *ConfigError) Error() string {
	return "falta " + e.Key + " (env o " + DefaultEnvFile + ")"
}

// LoadConfigFromEnv lee la config del entorno del proceso y del env file
// (el env del proceso gana). envFile vacío respeta NETPULSE_ENV_FILE y, en
// su defecto, DefaultEnvFile. Fail-fast si falta lo obligatorio.
func LoadConfigFromEnv(envFile string) (Options, error) {
	if envFile == "" {
		envFile = os.Getenv("NETPULSE_ENV_FILE")
		if envFile == "" {
			envFile = DefaultEnvFile
		}
	}
	fromFile := loadEnvFile(envFile)
	get := func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fromFile[key]
	}
	opts := Options{
		Server:        strings.TrimRight(get("NETPULSE_SERVER"), "/"),
		Token:         get("NETPULSE_TOKEN"),
		Slug:          get("NETPULSE_SLUG"),
		ServerFP:      tlspin.Normalize(get("NETPULSE_SERVER_FP")),
		PairingToken:  get("NETPULSE_PAIRING_TOKEN"),
		Interval:      DefaultInterval,
		WanTarget:     get("NETPULSE_WAN_TARGET"),
		GwTarget:      get("NETPULSE_GW_TARGET"),
		HeartbeatFile: get("NETPULSE_HEARTBEAT_FILE"),
		EnvFile:       envFile,
	}
	if v := get("NETPULSE_INTERVAL"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			opts.Interval = time.Duration(sec) * time.Second
		} else if d, err := time.ParseDuration(v); err == nil && d > 0 {
			opts.Interval = d
		} else {
			slog.Warn("[netpulse-agent] NETPULSE_INTERVAL inválido, usando 30s", "value", v)
		}
	}
	if err := opts.Validate(); err != nil {
		return Options{}, err
	}
	return opts, nil
}

// Validate comprueba lo obligatorio: SERVER y SLUG siempre; TOKEN salvo en
// modo pairing (donde PAIRING_TOKEN lo sustituye).
func (o Options) Validate() error {
	for _, kv := range [][2]string{
		{"NETPULSE_SERVER", o.Server},
		{"NETPULSE_SLUG", o.Slug},
	} {
		if strings.TrimSpace(kv[1]) == "" {
			return &ConfigError{kv[0]}
		}
	}
	if o.Token == "" && o.PairingToken == "" {
		return &ConfigError{"NETPULSE_TOKEN (o NETPULSE_PAIRING_TOKEN para bootstrap)"}
	}
	return nil
}

// loadEnvFile lee KEY=VALUE de un fichero env (líneas # = comentario).
func loadEnvFile(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out
}
