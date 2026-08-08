// uci.go — modo on-box (Fase 9, SPEC R4/R5): puente entre la config UCI del
// router (/etc/config/netpulse, sección `server`) y el loader de entorno.
//
//   - LoadUCIEnv traduce las opciones UCI a variables de entorno con las
//     mismas claves que .env, SIN pisar las ya presentes. Precedencia final:
//     entorno real > .env > UCI > defaults (misma semántica que LoadDotEnv).
//   - PersistUCIAuthPass guarda la contraseña generada en el bootstrap R4
//     vía la CLI `uci` (transaccional; nunca sed sobre ficheros — regla de
//     diseño de la Fase 10 aplicada ya aquí).
//
// Todo es best-effort/fail-soft: en un entorno sin `uci` (el CT) las
// funciones devuelven error y el caller decide (log y seguir). El binario
// nunca deja de arrancar por culpa de UCI.
package config

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// uciConfigFile es el fichero UCI del servidor on-box (convención OpenWrt;
// no se soporta `uci -c` con otro directorio).
const uciConfigFile = "/etc/config/netpulse"

// uciExecTimeout acota cada llamada a la CLI de uci.
const uciExecTimeout = 5 * time.Second

// uciEnvMap es la allowlist opción UCI → variable de entorno (SPEC R5: "las
// mismas claves"). Solo lo listado se inyecta: nada de mapear opciones
// arbitrarias a entorno. Orden estable para logs/tests deterministas.
var uciEnvMap = []struct{ uci, env string }{
	{"port", "PORT"},
	{"static_dir", "STATIC_DIR"},
	{"data_dir", "DATA_DIR"},
	{"session_secret", "SESSION_SECRET"},
	{"auth_user", "AUTH_USER"},
	{"auth_pass", "AUTH_PASS"},
	{"demo_mode", "DEMO_MODE"},
	{"max_sse_clients", "MAX_SSE_CLIENTS"},
	{"cookie_secure", "COOKIE_SECURE"},
	{"trust_proxy", "TRUST_PROXY"},
	{"wg_interface", "WG_INTERFACE"},
	{"github_repo", "GITHUB_REPO"},
	{"github_token", "GITHUB_TOKEN"},
	{"adguard_url", "ADGUARD_URL"},
	{"adguard_user", "ADGUARD_USER"},
	{"adguard_pass", "ADGUARD_PASS"},
	{"webhook_url", "WEBHOOK_URL"},
	{"webhook_secret", "WEBHOOK_SECRET"},
	{"agent_max_ts_drift_s", "AGENT_MAX_TS_DRIFT_S"},
	{"auto_rearm", "NETPULSE_AUTO_REARM"},
	{"agent_ttl_s", "NETPULSE_AGENT_TTL_S"},
}

// LoadUCIEnv lee `uci -q show netpulse` y aplica la sección server al
// entorno del proceso (solo claves de la allowlist ausentes del entorno).
// Error si uci no existe o el paquete netpulse no está configurado; el
// caller debe tratarlo como aviso, no como fatal.
func LoadUCIEnv() error {
	ctx, cancel := context.WithTimeout(context.Background(), uciExecTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "uci", "-q", "show", "netpulse").Output()
	if err != nil {
		return fmt.Errorf("uci -q show netpulse: %w", err)
	}
	applyUCIEnv(parseUCIShow(string(out)), os.LookupEnv, os.Setenv)
	return nil
}

// applyUCIEnv inyecta en el "entorno" (lookup/set inyectables para test) las
// opciones de la allowlist que NO estén ya presentes. Los errores de set se
// ignoran (os.Setenv solo falla con claves inválidas, que aquí son fijas).
func applyUCIEnv(vals map[string]string, lookup func(string) (string, bool), set func(string, string) error) {
	for _, m := range uciEnvMap {
		v, ok := vals[m.uci]
		if !ok || v == "" {
			continue
		}
		if _, exists := lookup(m.env); exists {
			continue
		}
		_ = set(m.env, v)
	}
}

// parseUCIShow parsea la salida de `uci show netpulse`:
//
//	netpulse.server=server
//	netpulse.server.port='3000'
//	netpulse.@server[0].data_dir='/etc/netpulse'   (sección anónima)
//
// Se aceptan la sección nombrada `server` y la anónima `@server[0]`; las
// líneas de declaración de sección (2 segmentos) y los paquetes ajenos se
// ignoran. Valores vacíos se descartan (opción sin configurar → defaults).
// No hay opciones de tipo lista en la allowlist; un valor de lista llegaría
// con comillas internas y simplemente no coincidiría con ningún enum/parse
// del loader (fail-fast aguas arriba).
func parseUCIShow(out string) map[string]string {
	vals := map[string]string{}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		parts := strings.Split(line[:eq], ".")
		if len(parts) != 3 || parts[0] != "netpulse" {
			continue
		}
		if parts[1] != "server" && parts[1] != "@server[0]" {
			continue
		}
		opt, val := parts[2], uciUnquote(line[eq+1:])
		if val == "" {
			continue
		}
		if _, dup := vals[opt]; !dup {
			vals[opt] = val
		}
	}
	return vals
}

// uciUnquote deshace el entrecomillado de `uci show`: valor entre comillas
// simples, con la comilla interna escapada al estilo shell (`'\”`).
func uciUnquote(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		s = s[1 : len(s)-1]
		s = strings.ReplaceAll(s, `'\''`, `'`)
	}
	return s
}

// PersistUCIAuthPass guarda la contraseña generada por el bootstrap on-box
// (R4) en netpulse.server.auth_pass y deja /etc/config/netpulse en 0600
// (contiene un secreto, como hace dropbear con sus claves). La sección la
// trae el paquete netpulse-server; si falta (instalación manual) se crea.
func PersistUCIAuthPass(pass string) error {
	if _, err := exec.LookPath("uci"); err != nil {
		return fmt.Errorf("uci no disponible: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), uciExecTimeout)
	defer cancel()

	// `uci set` falla si el paquete/sección no existen todavía: asegurar el
	// fichero y la sección nombrada antes de escribir la opción.
	if err := exec.CommandContext(ctx, "uci", "-q", "get", "netpulse.server").Run(); err != nil {
		f, err := os.OpenFile(uciConfigFile, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("creando %s: %w", uciConfigFile, err)
		}
		_ = f.Close()
		if err := uciRun(ctx, "set", "netpulse.server=server"); err != nil {
			return err
		}
	}
	if err := uciRun(ctx, "set", "netpulse.server.auth_pass="+pass); err != nil {
		return err
	}
	if err := uciRun(ctx, "commit", "netpulse"); err != nil {
		return err
	}
	// Best-effort: uci commit reescribe el fichero (puede resetear permisos).
	_ = os.Chmod(uciConfigFile, 0o600)
	return nil
}

// uciRun ejecuta un subcomando de uci con vector de argumentos (sin shell:
// inyección imposible) y envuelve el error con la salida para el log.
func uciRun(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "uci", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("uci %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Bootstrap R4 — contraseña inicial del admin on-box
// ---------------------------------------------------------------------------

// InitialPasswordLen es la longitud de la contraseña generada en el primer
// arranque on-box (SPEC Fase 9 R4: 26 chars ≈ 154 bits con charset de 62).
const InitialPasswordLen = 26

// passwordCharset: alfanumérico. Sin símbolos: el valor viaja por UCI, logread
// e input de login sin necesitar escapado en ninguna de las tres superficies.
const passwordCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateInitialPassword genera la contraseña del bootstrap con
// crypto/rand y muestreo por rechazo (248 = 62*4) para evitar el sesgo del
// módulo sobre el charset de 62.
func GenerateInitialPassword() (string, error) {
	const limit = byte(len(passwordCharset) * (256 / len(passwordCharset))) // 248
	out := make([]byte, InitialPasswordLen)
	buf := make([]byte, 64)
	i := 0
	for i < len(out) {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("crypto/rand: %w", err)
		}
		for _, b := range buf {
			if b >= limit {
				continue
			}
			out[i] = passwordCharset[int(b)%len(passwordCharset)]
			i++
			if i == len(out) {
				break
			}
		}
	}
	return string(out), nil
}
