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
//	NETPULSE_SERVER    URL del servidor (https://... o http://...:3000) [obligatoria]
//	NETPULSE_TOKEN     token del equipo (64 hex; se muestra una vez al crearlo)
//	NETPULSE_SLUG      slug del equipo (agent.token.<slug> en el servidor)
//	NETPULSE_INTERVAL  intervalo de push (default 30s; "30", "15s", "1m")
//	NETPULSE_ENV_FILE  fichero env alternativo (default /etc/netpulse-agent.env)
//	NETPULSE_WAN_TARGET    ping WAN con pérdida (gateway; p. ej. 1.1.1.1)
//	NETPULSE_GW_TARGET     ping corto al gateway (APs; p. ej. 192.168.8.1)
//	NETPULSE_INSECURE_TLS  "1" → no verificar el certificado (LAN autofirmado)
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
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
	interval            time.Duration
	wanTarget, gwTarget string
	insecureTLS         bool
	heartbeatFile       string
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
		interval:      pollInterval,
		wanTarget:     get("NETPULSE_WAN_TARGET"),
		gwTarget:      get("NETPULSE_GW_TARGET"),
		insecureTLS:   get("NETPULSE_INSECURE_TLS") == "1",
		heartbeatFile: get("NETPULSE_HEARTBEAT_FILE"),
	}
	if v := get("NETPULSE_INTERVAL"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			cfg.interval = time.Duration(sec) * time.Second
		} else if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.interval = d
		} else {
			log.Printf("[netpulse-agent] NETPULSE_INTERVAL %q inválido; usando 30s", v)
		}
	}
	for _, kv := range [][2]string{
		{"NETPULSE_SERVER", cfg.server},
		{"NETPULSE_TOKEN", cfg.token},
		{"NETPULSE_SLUG", cfg.slug},
	} {
		if kv[1] == "" {
			return config{}, &configError{kv[0]}
		}
	}
	return cfg, nil
}

type configError struct{ key string }

func (e *configError) Error() string {
	return "falta " + e.key + " (env o /etc/netpulse-agent.env)"
}

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("")

	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *versionFlag {
		fmt.Println(Version)
		os.Exit(0)
	}

	if err := run(); err != nil {
		log.Printf("[netpulse-agent] error fatal: %v", err)
		os.Exit(1)
	}
}

// pushOnce envía el payload y toca el heartbeat; si falla, lo loguea.
func pushOnce(ctx context.Context, client *push.Client, payload *probe.Payload, hbFile string) {
	if err := client.Push(ctx, payload); err != nil {
		log.Printf("[netpulse-agent] push falló (%v); buffered=%d descartados=%d",
			err, client.Buffered(), client.Dropped())
		return
	}
	if err := heartbeat.Touch(hbFile, time.Now()); err != nil {
		log.Printf("[netpulse-agent] heartbeat: %v", err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.insecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in documentado (LAN autofirmada)
	}
	client := push.New(cfg.server, cfg.token, &http.Client{Timeout: 10 * time.Second, Transport: transport})
	client.SetLogger(func(format string, args ...any) { log.Printf(format, args...) })

	prober := probe.NewProber(probe.ShellRunner{}, probe.Options{
		WanPingTarget: cfg.wanTarget,
		GwPingTarget:  cfg.gwTarget,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	log.Printf("[netpulse-agent] v%s · %s → %s cada %s", Version, cfg.slug, cfg.server, cfg.interval)

	hbFile := cfg.heartbeatFile
	if hbFile == "" {
		hbFile = heartbeat.DefaultFile
	}

	// Fase 7.1: eventos nl80211 en tiempo real (new/del station).
	if iwevents.Available() {
		log.Printf("[netpulse-agent] iw detectado: suscribiendo a eventos nl80211")
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
				log.Printf("[netpulse-agent] iw: %s %s (%s)", action, ev.MAC, ev.Iface)
				payload := prober.BuildWireless(ctx, cfg.slug, Version)
				pushOnce(ctx, client, payload, hbFile)
			}); err != nil {
				log.Printf("[netpulse-agent] iw event terminó: %v", err)
			}
		}()
	}

	// Fase 7.3: SSE bidireccional — el servidor envía comandos al agente.
	refreshCh := make(chan struct{}, 1)
	go func() {
	sse := sseclient.New(cfg.server, cfg.slug, cfg.token, func(ev sseclient.Event) {
		if ev.Name == "refresh" {
			select {
			case refreshCh <- struct{}{}:
			default:
			}
		}
		log.Printf("[netpulse-agent] SSE: %s", ev.Name)
	})
		sse.SetLogger(func(format string, args ...any) { log.Printf(format, args...) })
		sse.Run(ctx)
	}()

	// Ciclo principal: sondeo completo cada 30s o cuando el servidor lo pide.
	for {
		payload := prober.Build(ctx, cfg.slug, Version)
		pushOnce(ctx, client, payload, hbFile)
		select {
		case <-ctx.Done():
			log.Printf("[netpulse-agent] saliendo")
			return nil
		case <-refreshCh:
			// refresh inmediato pedido por el servidor
		case <-time.After(client.Delay(cfg.interval)):
		}
	}
}
