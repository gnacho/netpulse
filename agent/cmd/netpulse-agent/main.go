// netpulse-agent - agente nativo OpenWrt (SPEC-AGENTE-PILOTO §2): sondea
// LOCALMENTE el equipo (los mismos comandos que el servidor lanza por SSH,
// package probe compartido) y empuja el resultado a POST /api/ingest/agent
// con Bearer + backoff + buffer RAM acotado. Stateless: NADA en flash; logs
// a stderr (procd los manda a syslog). Todo el bucle vive en
// agent/runtime; este binario es un wrapper fino (flags + config + señales).
//
// Config (env o /etc/netpulse-agent.env; el env gana):
//
//	NETPULSE_SERVER        URL del servidor (https://... o http://...:3000) [obligatoria]
//	NETPULSE_TOKEN         token del equipo (64 hex; se muestra una vez al crearlo)
//	NETPULSE_SLUG          slug del equipo (agent.token.<slug> en el servidor)
//	NETPULSE_SERVER_FP     SHA-256 del SPKI del servidor en hex (obligatorio si la URL es https://)
//	NETPULSE_PAIRING_TOKEN token de pairing (modo bootstrap: contacta /api/agents/pair para
//	                       obtener el token real, lo escribe al env file y sale - procd reinicia)
//	NETPULSE_INTERVAL      intervalo de push (default 30s; "30", "15s", "1m")
//	NETPULSE_ENV_FILE      fichero env alternativo (default /etc/netpulse-agent.env)
//	NETPULSE_LOG_LEVEL     nivel de log: "info" (default) o "debug"
//	NETPULSE_WAN_TARGET    ping WAN con pérdida (gateway; p. ej. 1.1.1.1)
//	NETPULSE_GW_TARGET     ping corto al gateway (APs; p. ej. 192.168.8.1)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gnacho/netpulse/agent/runtime"
)

// Version del agente (la reporta cada push; se puede fijar con
// -ldflags "-X main.Version=x.y.z").
var Version = "0.1.0"

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

	opts, err := runtime.LoadConfigFromEnv("")
	if err != nil {
		slog.Error("[netpulse-agent] error fatal", "err", err)
		os.Exit(1)
	}
	opts.Version = Version
	// La delegación a NetGrip (si hay token local) se decide aquí; el bucle
	// cae al executor local cuando el delegado no está disponible.
	opts.ApplyDelegate = delegateToNetgrip

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := runtime.Run(ctx, opts); err != nil {
		slog.Error("[netpulse-agent] error fatal", "err", err)
		os.Exit(1)
	}
}
