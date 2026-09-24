// mqttpub_config.go: configuración del publisher en kv (#838). El entorno
// manda como base y la base de datos la pisa clave a clave, así una
// instalación configurada por `NETPULSE_MQTT_*` sigue igual hasta que alguien
// guarda desde Ajustes.
package mqttpub

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// kvStore es el contrato mínimo con la kv del servidor (mismo patrón que
// ntfy/telegram).
type kvStore interface {
	Get(key string) (string, bool)
	Set(key, value string) error
}

const (
	kvKeyEnabled  = "mqtt.enabled"
	kvKeyHost     = "mqtt.host"
	kvKeyPort     = "mqtt.port"
	kvKeyUser     = "mqtt.user"
	kvKeyPass     = "mqtt.pass"
	kvKeyInstance = "mqtt.instance"
	kvKeyInterval = "mqtt.interval"
)

// LoadConfig lee la configuración: el entorno como base y la kv por encima.
func LoadConfig(kv kvStore) Config {
	cfg := ConfigFromProcessEnv()
	if v, ok := kv.Get(kvKeyEnabled); ok {
		cfg.Enabled = v == "true"
	}
	if v, ok := kv.Get(kvKeyHost); ok {
		cfg.Host = strings.TrimSpace(v)
	}
	if v, ok := kv.Get(kvKeyPort); ok {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 65535 {
			cfg.Port = p
		}
	}
	if v, ok := kv.Get(kvKeyUser); ok {
		cfg.User = v
	}
	if v, ok := kv.Get(kvKeyPass); ok {
		cfg.Pass = v
	}
	if v, ok := kv.Get(kvKeyInstance); ok && strings.TrimSpace(v) != "" {
		cfg.Instance = strings.TrimSpace(v)
	}
	if v, ok := kv.Get(kvKeyInterval); ok {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			cfg.Interval = time.Duration(sec) * time.Second
		}
	}
	return cfg
}

// SaveConfig persiste la configuración en kv. La contraseña vacía conserva la
// guardada.
func SaveConfig(kv kvStore, cfg Config) error {
	if cfg.Enabled && strings.TrimSpace(cfg.Host) == "" {
		return fmt.Errorf("host is required to enable MQTT")
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return fmt.Errorf("invalid port %d", cfg.Port)
	}
	if strings.TrimSpace(cfg.Instance) == "" {
		cfg.Instance = "default"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.Pass == "" {
		if prev, ok := kv.Get(kvKeyPass); ok {
			cfg.Pass = prev
		}
	}
	pairs := []struct{ key, value string }{
		{kvKeyEnabled, strconv.FormatBool(cfg.Enabled)},
		{kvKeyHost, strings.TrimSpace(cfg.Host)},
		{kvKeyPort, strconv.Itoa(cfg.Port)},
		{kvKeyUser, cfg.User},
		{kvKeyPass, cfg.Pass},
		{kvKeyInstance, cfg.Instance},
		{kvKeyInterval, strconv.Itoa(int(cfg.Interval.Seconds()))},
	}
	for _, p := range pairs {
		if err := kv.Set(p.key, p.value); err != nil {
			return fmt.Errorf("save %s: %w", p.key, err)
		}
	}
	return nil
}

// SQLKV adapta la tabla kv del servidor al kvStore de este paquete.
type SQLKV struct{ DB *sql.DB }

// Get lee una clave (false si no existe).
func (s SQLKV) Get(key string) (string, bool) {
	var v string
	if err := s.DB.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

// Set escribe una clave (upsert).
func (s SQLKV) Set(key, value string) error {
	_, err := s.DB.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}
