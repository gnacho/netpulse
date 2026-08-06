// NetPulse backend (Go) — entry point (paridad src/index.js):
//
//	loadDotEnv → loadConfig (fail-fast) → openDb (+ migración Node→Go) →
//	ensureSessionSecret → ensureUsers (seed admin) → adapter (stub demo;
//	el live llega con el agente B) → hub SSE → poller → HTTP → graceful
//	shutdown SIGTERM/SIGINT (salvavidas 3 s).
//
// Base de rutas relativas (STATIC_DIR/DATA_DIR/.env): el WORKING DIRECTORY
// del proceso (equivalente al SERVER_ROOT de Node; systemd usa
// WorkingDirectory=/opt/netpulse/server).
package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/poller"
	"github.com/gnacho/netpulse/server-go/internal/push"
	"github.com/gnacho/netpulse/server-go/internal/rearmer"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"github.com/gnacho/netpulse/server-go/internal/sshkey"
	"github.com/gnacho/netpulse/server-go/internal/staticspa"
	"github.com/gnacho/netpulse/server-go/internal/updater"
	"github.com/gnacho/netpulse/server-go/internal/webhook"
)

func main() {
	if err := run(); err != nil {
		log.Printf("[netpulse] error fatal en arranque: %v", err)
		os.Exit(1)
	}
}

func run() error {
	serverRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	config.LoadDotEnv(serverRoot + "/.env")
	envMap := config.FromEnviron()
	cfg, err := config.Load(envMap, serverRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	// Auditoría de seguridad #1: la IP confiable (XFF) solo si TRUST_PROXY.
	auth.SetTrustProxy(cfg.TrustProxy)

	dbHandle, err := db.Open(cfg.DataDir)
	if err != nil {
		return err
	}

	// Modo mantenimiento: `-rollup` ejecuta el rollup nocturno de la escalera
	// de retención y sale. Con `-rollup-repair` primero purga metrics_buckets
	// y metrics_daily (útil tras un rollup previo mal formado, issue #54).
	if os.Getenv("NETPULSE_ROLLUP") == "1" {
		if os.Getenv("NETPULSE_ROLLUP_REPAIR") == "1" {
			log.Print("[netpulse] rollup: purgando metrics_buckets y metrics_daily")
			if _, err := dbHandle.Exec("DELETE FROM metrics_buckets"); err != nil {
				return err
			}
			if _, err := dbHandle.Exec("DELETE FROM metrics_daily"); err != nil {
				return err
			}
		}
		log.Print("[netpulse] rollup: ejecutando NightlyJob (buckets 5 min + daily)")
		dbHandle.NightlyJob()
		_ = dbHandle.Close()
		log.Print("[netpulse] rollup: OK")
		os.Exit(0)
	}

	secret, err := auth.EnsureSessionSecret(dbHandle, cfg)
	if err != nil {
		return err
	}
	if err := auth.EnsureUsers(dbHandle, cfg); err != nil {
		return err
	}

	// Clave SSH propia para sondear routers (se genera la primera vez)
	if err := sshkey.EnsureKeypair(cfg.SSHKeyPath); err != nil {
		log.Printf("[netpulse] aviso: no se pudo generar la clave SSH: %v", err)
	}
	// Bootstrap de routers: tabla vacía → ROUTERS_JSON o autodetección
	routers := routerstore.EnsureInitialRouters(dbHandle.DB, cfg)

	// Adapter: DEMO_MODE=1 → demo (dataset canónico + random walk);
	// live → sondeo real (SSH/ubus) de los routers configurados.
	// Registry de agentes nativos (Fase 3): en live alimenta el adapter
	// live-agent (Tier 2 con degrade a SSH); en demo solo registra pushes
	// (last_seen/versión para GET /api/agents) sin tocar el dataset canónico.
	agentTTL := adapters.AgentTTLDefault
	if v := os.Getenv("NETPULSE_AGENT_TTL_S"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			agentTTL = time.Duration(sec) * time.Second
		}
	}
	agentReg := adapters.NewAgentRegistry(agentTTL)
	var adapter adapters.Snapshotter
	var sshPool *adapters.SSHPool
	if cfg.DemoMode {
		adapter = adapters.NewDemo(alerts.New(dbHandle, nil))
	} else {
		// Fase 8.2 (R8): restaurar el último push persistido de cada agente
		// (kv agent.state.<slug>) para que lastSeen/versión sobrevivan a un
		// reinicio. El siguiente push del agente refresca el estado igual.
		restore := httpapi.NewStateRestorer(dbHandle)
		restore(agentReg)
		pool, err := adapters.NewSSHPool(cfg.SSHKeyPath)
		if err != nil {
			log.Printf("[netpulse] aviso: pool SSH no disponible (%v); sirviendo dataset demo", err)
			adapter = adapters.NewDemo(alerts.New(dbHandle, nil))
		} else {
			sshPool = pool
			live := adapters.NewLive(cfg, dbHandle, routers, pool)
			live.SetAgents(agentReg)
			adapter = live
		}
	}

	// Web Push (Bloque C): par VAPID en kv (primer arranque) + Notifier
	// asíncrono conectado al motor de alertas del adapter (solo eventos
	// urgentes que pasan config). Si falla, el resto del servidor sigue.
	var pushNotifier *push.Notifier
	if pub, priv, err := push.EnsureVAPIDKeys(dbHandle); err != nil {
		log.Printf("[netpulse] aviso: Web Push desactivado (%v)", err)
	} else {
		pushNotifier = push.NewNotifier(dbHandle, pub, priv)
	}

	// Webhook saliente (Fase 8.7b): si WEBHOOK_URL está configurado, las
	// alertas urgentes también se envían a esa URL (HMAC + retry + DLQ).
	var webhookNotifier *webhook.Notifier
	if cfg.Webhook != nil && cfg.Webhook.Enabled {
		webhookNotifier = webhook.NewNotifier(*cfg.Webhook, dbHandle)
		log.Printf("[netpulse] webhook saliente activo: %s", cfg.Webhook.URL)
	}

	// Notifier compuesto: push + webhook (SetNotifier solo admite UNO).
	if pushNotifier != nil || webhookNotifier != nil {
		adapter.AlertsEngine().SetNotifier(notifierChain{pushNotifier, webhookNotifier})
	}

	// Dependencia sse↔poller resuelta con un holder (como index.js:40-45).
	var p *poller.Poller
	hub := sse.NewHub(dbHandle, cfg.MaxSSEClients, func() any {
		if p == nil {
			return nil
		}
		return p.LastOverview()
	})
	p = poller.New(adapter, dbHandle, hub)

	// Estáticos: STATIC_DIR explícito → disco; si no → dist embebido.
	staticDir := ""
	if _, set := envMap["STATIC_DIR"]; set {
		staticDir = cfg.StaticDir
	}
	static := staticspa.New(staticDir)

	// Actualizador: repoRoot = padre de serverRoot (paridad index.js:49-53)
	upd := updater.New(filepath.Clean(filepath.Join(cfg.ServerRoot, "..")), cfg.GithubRepo, cfg.GithubToken)

	// Rearmer compartido (endpoint manual + supervisor de auto-rearme).
	// El supervisor solo arranca con NETPULSE_AUTO_REARM=1 y en modo live
	// con pool SSH: nada autónomo sobre equipamiento de red sin opt-in
	// explícito (regla Fase 10).
	rearmEngine := adapter.AlertsEngine()
	arm := rearmer.New(dbHandle.DB, agentReg, sshPool, rearmEngine, 0)
	var rearmSup *rearmer.Supervisor
	if cfg.AutoRearm && sshPool != nil {
		var cooldown time.Duration
		if v := os.Getenv("NETPULSE_AUTO_REARM_COOLDOWN_S"); v != "" {
			if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
				cooldown = time.Duration(sec) * time.Second
			}
		}
		rearmSup = rearmer.NewSupervisor(arm, agentReg, dbHandle.DB, rearmEngine, 0, cooldown)
		rearmSup.Start()
		if cooldown <= 0 {
			cooldown = rearmer.AutoCooldownDefault
		}
		log.Printf("[netpulse] supervisor de auto-rearme activo (cooldown %d s)", int(cooldown.Seconds()))
	}

	// Fase 7.3: SSE bidireccional agente↔servidor (AgentHub permite al
	// servidor enviar comandos al agente vía SSE).
	agentHub := sse.NewAgentHub(func(slug, token string) bool {
		return checkAgentToken(dbHandle, slug, token)
	})

	handler := httpapi.NewHandler(httpapi.Deps{
		Config:  cfg,
		DB:      dbHandle,
		Adapter: adapter,
		Hub:     hub,
		Secret:  secret,
		Static:  static,
		Updater: upd,
		Agents:  agentReg,
		Pool:    sshPool,
		Rearmer: arm,
		AgentHub: agentHub,
		LastOverview: func() *adapters.Overview {
			return p.LastOverview()
		},
		PollNow: p.PollNow,
		Started: time.Now(),
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: handler,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("[netpulse] v%s · modo %s · http://localhost:%d", httpapi.Version, adapter.Mode(), cfg.Port)
		staticDesc := staticDir
		if staticDesc == "" {
			staticDesc = "(embed)"
		}
		log.Printf("[netpulse] datos: %s · estáticos: %s", cfg.DataDir, staticDesc)
		p.Start()
		upd.Start()
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case sig := <-sigCh:
		name := "SIGINT"
		if sig == syscall.SIGTERM {
			name = "SIGTERM"
		}
		log.Printf("[netpulse] %s recibido, cerrando...", name)
		if rearmSup != nil {
			rearmSup.Stop()
		}
		p.Stop()
		upd.Stop()
		hub.NotifyShutdown()
		_ = adapter.Close()
		// Tras parar poller y adapter ya nadie emite alertas: se puede
		// cerrar el worker de push sin riesgo de Notify sobre canal cerrado.
		if pushNotifier != nil {
			pushNotifier.Close()
		}
		if webhookNotifier != nil {
			webhookNotifier.Close()
		}
		// Salvavidas de 3 s: salir igualmente aunque el server no cierre.
		lifeline := time.AfterFunc(3*time.Second, func() {
			_ = dbHandle.Close()
			os.Exit(0)
		})
		defer lifeline.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = dbHandle.Close()
	}
	return nil
}

// checkAgentToken valida el token de un agente contra su hash sha256 en kv.
// Usado por AgentHub y por el endpoint de binario.
func checkAgentToken(d *db.DB, slug, token string) bool {
	if token == "" || d == nil {
		return false
	}
	var stored string
	if err := d.QueryRow("SELECT value FROM kv WHERE key = ?", "agent.token."+slug).Scan(&stored); err != nil {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	got := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(stored)) == 1
}

// notifierChain implementa alerts.Notifier encadenando varios notifiers
// (push + webhook). El motor de alertas solo admite UNO (SetNotifier), así
// que este compuesto reparte cada evento a todos los miembros no nulos.
type notifierChain []alerts.Notifier

func (c notifierChain) Notify(ev alerts.AlertEvent) {
	for _, n := range c {
		if n != nil {
			n.Notify(ev)
		}
	}
}
