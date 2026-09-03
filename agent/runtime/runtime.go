// Package runtime expone el bucle del agente NetPulse como paquete público
// para que otras aplicaciones (p. ej. NetGrip) lo embeban sin replicar
// lógica. Run hace todo lo que hacía cmd/netpulse-agent: sondeo local
// (probe), push con Bearer + backoff + buffer RAM acotado, eventos nl80211
// en tiempo real, SSE bidireccional (refresh/apply/upgrade) y modo pairing
// bootstrap. El logger y el estado del agente son observables via Options.
package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/agent/internal/heartbeat"
	"github.com/gnacho/netpulse/agent/internal/iwevents"
	"github.com/gnacho/netpulse/agent/internal/push"
	"github.com/gnacho/netpulse/agent/internal/sseclient"
	"github.com/gnacho/netpulse/agent/internal/tlspin"
	"github.com/gnacho/netpulse/agent/probe"
)

const (
	// DefaultInterval: ciclo completo de sistema + wireless + DHCP + FDB (30s).
	DefaultInterval = 30 * time.Second
	// ubusMinGap: tiempo mínimo entre pushes wireless consecutivos disparados
	// por eventos nl80211 (evita martillear al servidor con ráfagas).
	ubusMinGap = 3 * time.Second
)

// Options configura Run. Server/Slug y Token (o PairingToken) son
// obligatorios; Validate los comprueba antes de arrancar.
type Options struct {
	Server        string // URL del servidor ("https://..." o "http://...:3000")
	Token         string // token del equipo (64 hex)
	Slug          string // slug del equipo (agent.token.<slug> en el servidor)
	ServerFP      string // SHA-256 del SPKI del servidor en hex (https obligatorio)
	PairingToken  string // modo bootstrap: pairing una vez y salir
	Interval      time.Duration
	WanTarget     string
	GwTarget      string
	HeartbeatFile string
	EnvFile       string // ruta del env file (para escribir el token tras pairing)
	Version       string // la reporta cada push
	// Kind etiqueta el SABOR del pusher en el payload (#363): "" = agente
	// standalone netpulse-agent; "netgrip" = agente embebido en el panel
	// NetGrip. El servidor lo usa para la UI y para NO reinstalar el
	// binario standalone sobre un NetGrip.
	Kind string

	// OnStatus recibe el estado en cada cambio (arranque, cada push, parada).
	// Nunca rompe el bucle: un panic dentro del callback se recupera.
	OnStatus func(Status)

	// Logger opcional; nil usa slog.Default().
	Logger *slog.Logger

	// ApplyDelegate permite delegar la ejecución de Ops a otro proceso
	// (NetGrip via HTTP local). nil o (ok=false) ejecuta el executor local.
	ApplyDelegate func(ops []executor.Op) (executor.ApplyResult, bool)

	// OnUpgrade sustituye el self-upgrade integrado (evento SSE "upgrade").
	// nil usa el comportamiento estándar: descargar binario + swap + restart.
	OnUpgrade func(data string)
}

// Status es una foto del agente para que el embedder pinte estado.
type Status struct {
	Running   bool
	PushOk    bool
	LastPush  time.Time
	LastError string
	Slug      string
	Server    string
}

// Run ejecuta el bucle del agente hasta que ctx se cancele. Si
// Options.PairingToken está puesto, hace el pairing bootstrap y devuelve.
func Run(ctx context.Context, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	opts.normalize()

	if opts.PairingToken != "" {
		return pairWithServer(opts)
	}

	transport, err := tlspin.BuildTransport(opts.Server, opts.ServerFP)
	if err != nil {
		return err
	}
	client := push.New(opts.Server, opts.Token, &http.Client{Timeout: 10 * time.Second, Transport: transport})
	log := opts.logger()
	client.SetLogger(func(format string, args ...any) { log.Debug("[netpulse-agent] " + fmt.Sprintf(format, args)) })

	prober := probe.NewProber(probe.ShellRunner{}, probe.Options{
		WanPingTarget: opts.WanTarget,
		GwPingTarget:  opts.GwTarget,
	})

	a := &agent{opts: opts, log: log, client: client}
	hbFile := opts.HeartbeatFile
	if hbFile == "" {
		hbFile = heartbeat.DefaultFile
	}
	a.hbFile = hbFile

	log.Info("[netpulse-agent] agente iniciado", "version", opts.Version, "slug", opts.Slug, "server", opts.Server, "interval", opts.Interval)
	a.setRunning(true)

	// #453: si existe un upgrade de firmware pendiente de un reboot anterior,
	// reportamos el resultado final ahora que el agente ha vuelto.
	checkPendingFirmwareUpgrade(opts, transport)

	// Eventos nl80211 en tiempo real (new/del station): push inmediato de
	// wireless sin esperar al ciclo de sondeo.
	if iwevents.Available() {
		log.Info("[netpulse-agent] iw detectado: suscribiendo a eventos nl80211")
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
				log.Info("[netpulse-agent] iw evento", "action", action, "mac", ev.MAC, "iface", ev.Iface)
				payload := prober.BuildWireless(ctx, opts.Slug, opts.Version)
				payload.Kind = opts.Kind
				a.pushOnce(ctx, payload)
			}); err != nil {
				log.Warn("[netpulse-agent] iw event terminó", "err", err)
			}
		}()
	}

	ex := executor.New(opts.GwTarget, opts.WanTarget)

	// SSE bidireccional: el servidor envía comandos al agente. Reutiliza el
	// mismo transporte (mismo pinning SPKI que el push).
	refreshCh := make(chan struct{}, 1)
	go func() {
		sse := sseclient.New(opts.Server, opts.Slug, opts.Token,
			&http.Client{Timeout: 0, Transport: transport},
			func(ev sseclient.Event) {
				if ev.Name == "refresh" {
					select {
					case refreshCh <- struct{}{}:
					default:
					}
				}
				if ev.Name == "apply" && ev.Data != "" {
					go a.handleApply(ex, transport, ev.Data)
				}
				if ev.Name == "upgrade" {
					if opts.OnUpgrade != nil {
						go opts.OnUpgrade(ev.Data)
					} else {
						go handleUpgrade(opts, transport, ev.Data)
					}
				}
				if ev.Name == "firmware_upgrade" && ev.Data != "" {
					go handleFirmwareUpgrade(opts, transport, ev.Data)
				}
				log.Debug("[netpulse-agent] SSE evento", "event", ev.Name)
			})
		sse.SetLogger(func(format string, args ...any) { log.Debug("[netpulse-agent] " + fmt.Sprintf(format, args)) })
		sse.Run(ctx)
	}()

	// Ciclo principal: sondeo completo cada Interval o cuando el servidor
	// lo pide (refresh).
	for {
		payload := prober.Build(ctx, opts.Slug, opts.Version)
		payload.Kind = opts.Kind
		a.pushOnce(ctx, payload)
		select {
		case <-ctx.Done():
			log.Info("[netpulse-agent] saliendo")
			a.setRunning(false)
			return nil
		case <-refreshCh:
			// refresh inmediato pedido por el servidor
		case <-time.After(client.Delay(opts.Interval)):
		}
	}
}

// agent acumula el estado observable del bucle.
type agent struct {
	opts   Options
	log    *slog.Logger
	client *push.Client
	hbFile string

	mu     sync.Mutex
	status Status
}

// pushOnce envía el payload, toca el heartbeat y actualiza el estado.
func (a *agent) pushOnce(ctx context.Context, payload *probe.Payload) {
	if err := a.client.Push(ctx, payload); err != nil {
		a.log.Warn("[netpulse-agent] push falló", "err", err, "buffered", a.client.Buffered(), "dropped", a.client.Dropped())
		a.setPush(false, err.Error())
		return
	}
	if err := heartbeat.Touch(a.hbFile, time.Now()); err != nil {
		a.log.Warn("[netpulse-agent] heartbeat error", "err", err)
	}
	a.setPush(true, "")
}

func (a *agent) setRunning(running bool) {
	a.mu.Lock()
	a.status.Running = running
	a.status.Slug = a.opts.Slug
	a.status.Server = a.opts.Server
	a.mu.Unlock()
	a.emit()
}

func (a *agent) setPush(ok bool, errMsg string) {
	a.mu.Lock()
	a.status.PushOk = ok
	if ok {
		a.status.LastPush = time.Now()
		a.status.LastError = ""
	} else {
		a.status.LastError = errMsg
	}
	a.mu.Unlock()
	a.emit()
}

// emit notifica OnStatus fuera del lock; un panic del callback se recupera
// para que nunca tumbe el bucle del agente.
func (a *agent) emit() {
	if a.opts.OnStatus == nil {
		return
	}
	a.mu.Lock()
	st := a.status
	a.mu.Unlock()
	defer func() { _ = recover() }()
	a.opts.OnStatus(st)
}

// handleApply procesa un comando apply del servidor: parsea las Ops, las
// ejecuta con snapshot+healthcheck+rollback (o delega en NetGrip si
// ApplyDelegate está puesto) y POSTea el resultado.
func (a *agent) handleApply(ex *executor.Executor, transport http.RoundTripper, data string) {
	var payload struct {
		PlanID string        `json:"plan_id"`
		Ops    []executor.Op `json:"ops"`
	}
	if err := jsonUnmarshal(data, &payload); err != nil {
		a.log.Warn("[netpulse-agent] apply: parse error", "err", err)
		return
	}
	if len(payload.Ops) == 0 {
		a.log.Warn("[netpulse-agent] apply: sin ops", "plan_id", payload.PlanID)
		return
	}

	a.log.Info("[netpulse-agent] apply iniciado", "plan_id", payload.PlanID, "ops", len(payload.Ops))

	var result executor.ApplyResult
	delegated := false
	if a.opts.ApplyDelegate != nil {
		if r, ok := a.opts.ApplyDelegate(payload.Ops); ok {
			result = r
			delegated = true
			a.log.Info("[netpulse-agent] apply delegado a NetGrip", "plan_id", payload.PlanID, "status", result.Status, "ms", result.DurationMs)
		}
	}
	if !delegated {
		result = ex.Apply(payload.Ops)
		a.log.Info("[netpulse-agent] apply ejecutado por agente", "plan_id", payload.PlanID, "status", result.Status, "ms", result.DurationMs)
	}

	postJSONWithRetry(a.opts, transport, "/api/agents/"+a.opts.Slug+"/apply-result",
		map[string]any{"planId": payload.PlanID, "result": result}, "apply-result")
}

func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

// normalize sanea Server (sin / final) y ServerFP (hex normalizado).
func (o *Options) normalize() {
	o.Server = strings.TrimRight(o.Server, "/")
	o.ServerFP = tlspin.Normalize(o.ServerFP)
	if o.Interval <= 0 {
		o.Interval = DefaultInterval
	}
}
