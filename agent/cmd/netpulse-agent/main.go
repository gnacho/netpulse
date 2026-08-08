// netpulse-agent — agente nativo OpenWrt (SPEC-AGENTE-PILOTO §2): sondea
// LOCALMENTE el equipo (los mismos comandos que el servidor lanza por SSH,
// package probe compartido) y empuja el resultado a POST /api/ingest/agent
// con Bearer + backoff + buffer RAM acotado. Stateless: NADA en flash; logs
// a stderr (procd los manda a syslog).
//
// Fase 7.1: eventos ubus en tiempo real (hostapd assoc/disassoc) → push
// inmediato de wireless + DHCP, sin esperar al ciclo de sondeo (30s).
//
// Config (env o /etc/netpulse-agent.env; el env gana):
//
//	NETPULSE_SERVER        URL del servidor (https://... o http://...:3000) [obligatoria]
//	NETPULSE_TOKEN         token del equipo (64 hex; se muestra una vez al crearlo)
//	NETPULSE_SLUG          slug del equipo (agent.token.<slug> en el servidor)
//	NETPULSE_SERVER_FP     SHA-256 del SPKI del servidor en hex (obligatorio si la URL es https://)
//	NETPULSE_PAIRING_TOKEN token de pairing (modo bootstrap: contacta /api/agents/pair para
//	                       obtener el token real, lo escribe al env file y sale — procd reinicia)
//	NETPULSE_INTERVAL      intervalo de push (default 30s; "30", "15s", "1m")
//	NETPULSE_ENV_FILE      fichero env alternativo (default /etc/netpulse-agent.env)
//	NETPULSE_LOG_LEVEL     nivel de log: "info" (default) o "debug"
//	NETPULSE_WAN_TARGET    ping WAN con pérdida (gateway; p. ej. 1.1.1.1)
//	NETPULSE_GW_TARGET     ping corto al gateway (APs; p. ej. 192.168.8.1)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gnacho/netpulse/agent/internal/heartbeat"
	"github.com/gnacho/netpulse/agent/internal/iwevents"
	"github.com/gnacho/netpulse/agent/internal/push"
	"github.com/gnacho/netpulse/agent/internal/sseclient"
	"github.com/gnacho/netpulse/agent/internal/tlspin"
	"github.com/gnacho/netpulse/agent/probe"
)

// Version del agente (la reporta cada push; se puede fijar con
// -ldflags "-X main.Version=x.y.z").
var Version = "0.1.0"

const (
	// pollInterval: ciclo completo de sistema + wireless + DHCP + FDB (30s).
	pollInterval = 30 * time.Second
	// ubusMinGap: tiempo mínimo entre pushes wireless consecutivos disparados
	// por eventos ubus (evita martillear al servidor con ráfagas assoc/disassoc
	// — p. ej. un cliente que entra y sale repetidamente).
	ubusMinGap = 3 * time.Second
)

type config struct {
	server, token, slug string
	serverFP            string // SPKI hash hex (obligatorio si la URL es https://)
	pairingToken        string // si está puesto: modo bootstrap (pairing)
	interval            time.Duration
	wanTarget, gwTarget string
	heartbeatFile       string
	envFile             string // ruta del env file (para escribir el token tras pairing)
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

	// loadConfig: env del proceso > fichero env; fail-fast si falta lo obligatorio.
func loadConfig() (config, error) {
	file := os.Getenv("NETPULSE_ENV_FILE")
	if file == "" {
		file = "/etc/netpulse-agent.env"
	}
	fromFile := loadEnvFile(file)
	get := func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fromFile[key]
	}
	cfg := config{
		server:        strings.TrimRight(get("NETPULSE_SERVER"), "/"),
		token:         get("NETPULSE_TOKEN"),
		slug:          get("NETPULSE_SLUG"),
		serverFP:      tlspin.Normalize(get("NETPULSE_SERVER_FP")),
		pairingToken:  get("NETPULSE_PAIRING_TOKEN"),
		interval:      pollInterval,
		wanTarget:     get("NETPULSE_WAN_TARGET"),
		gwTarget:      get("NETPULSE_GW_TARGET"),
		heartbeatFile: get("NETPULSE_HEARTBEAT_FILE"),
		envFile:       file,
	}
	if v := get("NETPULSE_INTERVAL"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			cfg.interval = time.Duration(sec) * time.Second
		} else if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.interval = d
		} else {
			slog.Warn("[netpulse-agent] NETPULSE_INTERVAL inválido, usando 30s", "value", v)
		}
	}
	// Validación: SERVER y SLUG siempre obligatorios. TOKEN obligatorio salvo
	// en modo pairing (donde PAIRING_TOKEN sustituye a TOKEN).
	for _, kv := range [][2]string{
		{"NETPULSE_SERVER", cfg.server},
		{"NETPULSE_SLUG", cfg.slug},
	} {
		if kv[1] == "" {
			return config{}, &configError{kv[0]}
		}
	}
	if cfg.token == "" && cfg.pairingToken == "" {
		return config{}, &configError{"NETPULSE_TOKEN (o NETPULSE_PAIRING_TOKEN para bootstrap)"}
	}
	return cfg, nil
}

type configError struct{ key string }

func (e *configError) Error() string {
	return "falta " + e.key + " (env o /etc/netpulse-agent.env)"
}

func main() {
	// Niveles de log (P7): Info por defecto, Debug con NETPULSE_LOG_LEVEL=debug.
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("NETPULSE_LOG_LEVEL"), "debug") {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *versionFlag {
		fmt.Println(Version)
		os.Exit(0)
	}

	if err := run(); err != nil {
		slog.Error("[netpulse-agent] error fatal", "err", err)
		os.Exit(1)
	}
}

// pushOnce envía el payload y toca el heartbeat; si falla, lo loguea.
func pushOnce(ctx context.Context, client *push.Client, payload *probe.Payload, hbFile string) {
	if err := client.Push(ctx, payload); err != nil {
		slog.Warn("[netpulse-agent] push falló", "err", err, "buffered", client.Buffered(), "dropped", client.Dropped())
		return
	}
	if err := heartbeat.Touch(hbFile, time.Now()); err != nil {
		slog.Warn("[netpulse-agent] heartbeat error", "err", err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Fase 9 R3: modo pairing (bootstrap). Si hay pairing_token en vez de
	// token, contactar al servidor una vez, obtener el token real, escribirlo
	// al env file y salir. procd reinicia el agente, que arranca en modo normal.
	if cfg.pairingToken != "" {
		return pairWithServer(cfg)
	}

	transport, err := tlspin.BuildTransport(cfg.server, cfg.serverFP)
	if err != nil {
		return err
	}
	client := push.New(cfg.server, cfg.token, &http.Client{Timeout: 10 * time.Second, Transport: transport})
	client.SetLogger(func(format string, args ...any) { slog.Debug("[netpulse-agent] "+fmt.Sprintf(format, args...)) })

	prober := probe.NewProber(probe.ShellRunner{}, probe.Options{
		WanPingTarget: cfg.wanTarget,
		GwPingTarget:  cfg.gwTarget,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	slog.Info("[netpulse-agent] agente iniciado", "version", Version, "slug", cfg.slug, "server", cfg.server, "interval", cfg.interval)

	hbFile := cfg.heartbeatFile
	if hbFile == "" {
		hbFile = heartbeat.DefaultFile
	}

	// Fase 7.1: eventos nl80211 en tiempo real (new/del station).
	if iwevents.Available() {
		slog.Info("[netpulse-agent] iw detectado: suscribiendo a eventos nl80211")
		var lastEventPush time.Time
		var evMu sync.Mutex
		go func() {
			if err := iwevents.Listen(ctx, func(ev iwevents.Event) {
				evMu.Lock()
				since := time.Since(lastEventPush)
				if since < ubusMinGap {
					evMu.Unlock()
					return
				}
				lastEventPush = time.Now()
				evMu.Unlock()
				action := "assoc"
				if !ev.Connected {
					action = "disassoc"
				}
				slog.Info("[netpulse-agent] iw evento", "action", action, "mac", ev.MAC, "iface", ev.Iface)
				payload := prober.BuildWireless(ctx, cfg.slug, Version)
				pushOnce(ctx, client, payload, hbFile)
			}); err != nil {
				slog.Warn("[netpulse-agent] iw event terminó", "err", err)
			}
		}()
	}

	// Fase 7.3: SSE bidireccional — el servidor envía comandos al agente.
	// El SSE reutiliza el mismo transporte (mismo pinning SPKI que el push).
	refreshCh := make(chan struct{}, 1)
	go func() {
		sse := sseclient.New(cfg.server, cfg.slug, cfg.token,
			&http.Client{Timeout: 0, Transport: transport},
			func(ev sseclient.Event) {
			if ev.Name == "refresh" {
				select {
				case refreshCh <- struct{}{}:
				default:
				}
			}
			slog.Debug("[netpulse-agent] SSE evento", "event", ev.Name)
		})
		sse.SetLogger(func(format string, args ...any) { slog.Debug("[netpulse-agent] "+fmt.Sprintf(format, args...)) })
		sse.Run(ctx)
	}()

	// Ciclo principal: sondeo completo cada 30s o cuando el servidor lo pide.
	for {
		payload := prober.Build(ctx, cfg.slug, Version)
		pushOnce(ctx, client, payload, hbFile)
		select {
		case <-ctx.Done():
			slog.Info("[netpulse-agent] saliendo")
			return nil
		case <-refreshCh:
			// refresh inmediato pedido por el servidor
		case <-time.After(client.Delay(cfg.interval)):
		}
	}
}

// pairWithServer contacta POST /api/agents/pair con el pairing token, recibe
// el token real del agente, lo escribe al env file y sale (procd reinicia).
// Usa el server_fp para validar TLS (el admin lo proporciona junto con el
// pairing token).
func pairWithServer(cfg config) error {
	transport, err := tlspin.BuildTransport(cfg.server, cfg.serverFP)
	if err != nil {
		return err
	}

	body := fmt.Sprintf(`{"pairing_token":%q,"slug":%q}`, cfg.pairingToken, cfg.slug)
	req, err := http.NewRequest("POST", cfg.server+"/api/agents/pair", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	hc := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	slog.Info("[netpulse-agent] pairing", "server", cfg.server, "slug", cfg.slug)
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("pairing: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 201 {
		respBody, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("pairing fallido (HTTP %d): %s", res.StatusCode, respBody)
	}

	var pr struct {
		Slug     string `json:"slug"`
		Token    string `json:"token"`
		ServerFP string `json:"server_fp"`
	}
	if err := json.NewDecoder(res.Body).Decode(&pr); err != nil {
		return fmt.Errorf("parsear respuesta pairing: %w", err)
	}
	if pr.Token == "" {
		return fmt.Errorf("pairing: token vacío en respuesta")
	}

	// Escribir el token real al env file (reemplaza PAIRING_TOKEN por TOKEN).
	if err := writePairedToken(cfg.envFile, pr.Token); err != nil {
		return fmt.Errorf("escribir token al env file: %w", err)
	}

	slog.Info("[netpulse-agent] pairing OK", "slug", pr.Slug, "env_file", cfg.envFile)
	// Salir limpiamente: procd reinicia el agente, que ahora lee NETPULSE_TOKEN
	// del env file y arranca en modo normal.
	return nil
}

// writePairedToken reescribe el env file: quita NETPULSE_PAIRING_TOKEN y
// añade/actualiza NETPULSE_TOKEN. Conserva el resto de líneas.
func writePairedToken(path, token string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		// Si no existe, crear uno nuevo con el token.
		return os.WriteFile(path, []byte("NETPULSE_TOKEN="+token+"\n"), 0o600)
	}
	var out []string
	haveToken := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			out = append(out, line)
			continue
		}
		key := strings.TrimSpace(k)
		if key == "NETPULSE_PAIRING_TOKEN" {
			continue // eliminar
		}
		if key == "NETPULSE_TOKEN" {
			out = append(out, "NETPULSE_TOKEN="+token)
			haveToken = true
			continue
		}
		out = append(out, line)
	}
	if !haveToken {
		out = append(out, "NETPULSE_TOKEN="+token)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o600)
}
