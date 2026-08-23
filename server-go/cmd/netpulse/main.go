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
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
	"github.com/gnacho/netpulse/server-go/internal/poller"
	"github.com/gnacho/netpulse/server-go/internal/push"
	"github.com/gnacho/netpulse/server-go/internal/rearmer"
	"github.com/gnacho/netpulse/server-go/internal/roamevents"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"github.com/gnacho/netpulse/server-go/internal/sshkey"
	"github.com/gnacho/netpulse/server-go/internal/staticspa"
	"github.com/gnacho/netpulse/server-go/internal/tlscert"
	"github.com/gnacho/netpulse/server-go/internal/updater"
	"github.com/gnacho/netpulse/server-go/internal/webhook"
)

// Timeouts de http.Server (issue #210): ReadTimeout/WriteTimeout acotan una
// conexión lenta (slowloris) sin cuerpo. Los endpoints SSE (/api/stream y
// /api/agents/{slug}/stream) son conexiones largas que escriben de forma
// continua: su write deadline se extiende vía http.ResponseController antes
// de delegar en el handler (con WriteTimeout plano, la conexión moriría a los
// 15 s aunque hubiera heartbeats).
const (
	serverReadTimeout  = 15 * time.Second
	serverWriteTimeout = 15 * time.Second
	sseWriteTimeout    = 24 * time.Hour
)

// isSSEStreamPath devuelve true para los endpoints SSE de larga duración.
func isSSEStreamPath(p string) bool {
	if p == "/api/stream" {
		return true
	}
	return strings.HasPrefix(p, "/api/agents/") && strings.HasSuffix(p, "/stream")
}

// webhookHostForLog devuelve solo el host del webhook para el log de arranque:
// la URL completa puede incrustar credenciales (https://user:pass@host) y
// acabarían en el log (issue #217).
func webhookHostForLog(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "(sin host)"
	}
	return u.Host
}

// withSSEWriteTimeout extiende el write deadline para los endpoints SSE: el
// server lo fija a WriteTimeout al leer la petición y, sin este override, una
// conexión SSE se cerraría a los 15 s.
func withSSEWriteTimeout(next http.Handler, long time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSSEStreamPath(r.URL.Path) {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(long))
		}
		next.ServeHTTP(w, r)
	})
}

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
	// Fase 9 (R5): en on-box la config vive en /etc/config/netpulse. El
	// loader la traduce a entorno SIN pisar lo ya presente — precedencia:
	// entorno real > .env > UCI > defaults. Fail-soft: sin uci se sigue.
	if os.Getenv("NETPULSE_ONBOX") == "1" {
		if err := config.LoadUCIEnv(); err != nil {
			log.Printf("[netpulse] aviso: config UCI no cargada (%v); siguiendo con entorno/defaults", err)
		}
	}
	envMap := config.FromEnviron()
	cfg, err := config.Load(envMap, serverRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	// Auditoría de seguridad #1: la IP confiable (XFF) solo si TRUST_PROXY.
	auth.SetTrustProxy(cfg.TrustProxy)

	// Fase 9 (R6): on-box la DB va en journal DELETE (sin -wal en flash).
	var dbOpts []db.OpenOption
	if cfg.Onbox {
		dbOpts = append(dbOpts, db.WithRollbackJournal())
	}
	dbHandle, err := db.Open(cfg.DataDir, dbOpts...)
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
	// Fase 9 (R4): bootstrap on-box sin AUTH_PASS. Solo en el PRIMER
	// arranque (sin usuarios): se genera una contraseña aleatoria, se
	// intenta persistir en UCI (netpulse.server.auth_pass) y se imprime UNA
	// vez en el log (logread). Con usuarios ya creados no se toca nada:
	// EnsureUsers es no-op y el login usa el hash de la DB.
	if cfg.Onbox && cfg.AuthPass == "" {
		n, err := auth.CountUsers(dbHandle)
		if err != nil {
			return fmt.Errorf("onbox: contando usuarios: %w", err)
		}
		if n == 0 {
			pass, err := config.GenerateInitialPassword()
			if err != nil {
				return fmt.Errorf("onbox: generando contraseña inicial: %w", err)
			}
			cfg.AuthPass = pass
			if err := config.PersistUCIAuthPass(pass); err != nil {
				log.Printf("[netpulse] onbox: auth_pass NO persistida en UCI (%v): apúntala del log AHORA", err)
			} else {
				log.Print("[netpulse] onbox: auth_pass guardada en UCI (uci get netpulse.server.auth_pass)")
			}
			log.Print("[netpulse] ==================================================")
			log.Print("[netpulse] ONBOX primer arranque — credenciales de la webapp")
			log.Printf("[netpulse]   usuario:    %s", cfg.AuthUser)
			log.Printf("[netpulse]   contraseña: %s", pass)
			log.Print("[netpulse] ==================================================")
		}
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
	var eventsCollector *roamevents.Collector
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
			// Dead Man's Switch (P6): periodo de confirmación antes de
			// alertar caída de agente (default 3 min, config por env).
			if v := os.Getenv("NETPULSE_AGENT_DOWN_CONFIRM_S"); v != "" {
				if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
					live.SetAgentDownConfirm(time.Duration(sec) * time.Second)
				}
			}
			adapter = live
			// Fase 14.5: collector continuo de eventos hostapd/DAWN.
			// Goroutine dedicada cada 60s, lee logread por router y
			// persiste en roam_events con dedup. Solo en modo live.
			eventsCollector = roamevents.NewCollector(dbHandle.DB, sshPool, func() []roamevents.RouterHost {
				out := []roamevents.RouterHost{}
				for _, r := range routerstore.ListRouters(dbHandle.DB) {
					out = append(out, roamevents.RouterHost{
						ID: r.ID, Host: r.Host, Name: r.Name, AgentOnly: r.AgentOnly,
					})
				}
				return out
			})
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
		log.Printf("[netpulse] webhook saliente activo: %s", webhookHostForLog(cfg.Webhook.URL))
	}

	// Notifier compuesto: push + webhook (SetNotifier solo admite UNO).
	// OJO nil-encapsulado (issue #57): meter un `(*webhook.Notifier)(nil)` en
	// la cadena lo empaqueta en la interfaz con tipo pero valor nil → la
	// interfaz NO es nil y `if n != nil` de notifierChain no lo filtra →
	// panic al Notify. Filtrar ANTES de empaquetar.
	if pushNotifier != nil || webhookNotifier != nil {
		chain := notifierChain{}
		if pushNotifier != nil {
			chain = append(chain, pushNotifier)
		}
		if webhookNotifier != nil {
			chain = append(chain, webhookNotifier)
		}
		adapter.AlertsEngine().SetNotifier(chain)
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

	// Actualizador: repoRoot = padre de serverRoot (paridad index.js:49-53).
	// La versión embebida (httpapi.Version) se usa para comparar contra el
	// último release tag en modo estable (layout install.sh).
	upd := updater.New(filepath.Clean(filepath.Join(cfg.ServerRoot, "..")), cfg.GithubRepo, cfg.GithubToken, httpapi.Version, dbHandle.DB)

	// Fase 10: motor de orquestación (plan→apply→state).
	orchMgr := orchestr.New(dbHandle)

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

	// Fase 9 R2/R3: TLS autofirmado + pairing token (on-box only).
	// Se generan ANTES del handler para que serverFP esté disponible en Deps.
	var (
		serverFP string
		tlsConf  *tls.Config
	)
	if cfg.Onbox {
		certPath := filepath.Join(cfg.DataDir, "cert.pem")
		keyPath := filepath.Join(cfg.DataDir, "key.pem")
		var err2 error
		tlsConf, serverFP, err2 = tlscert.Ensure(certPath, keyPath)
		if err2 != nil {
			return fmt.Errorf("cert on-box: %w", err2)
		}
		log.Printf("[netpulse] TLS autofirmado on-box: %s", certPath)
		log.Printf("[netpulse] FINGERPRINT SPKI (sha256): %s", serverFP)

		// Pairing token (generado en el primer arranque). NO se loguea (issue
		// #214): está disponible en la UI de Ajustes > Adopción (GET
		// /api/pairing/token, admin) y un proceso cualquiera con acceso a los
		// logs podría adoptar agentes con él.
		if _, perr := httpapi.EnsurePairingToken(dbHandle); perr != nil {
			return fmt.Errorf("pairing token: %w", perr)
		}
	}

	handler := httpapi.NewHandler(httpapi.Deps{
		Config:   cfg,
		DB:       dbHandle,
		Adapter:  adapter,
		Hub:      hub,
		Secret:   secret,
		Static:   static,
		Updater:  upd,
		Agents:   agentReg,
		Pool:     sshPool,
		Rearmer:  arm,
		AgentHub: agentHub,
		ServerFP: serverFP,
		Orchestr: orchMgr,
		LastOverview: func() *adapters.Overview {
			return p.LastOverview()
		},
		PollNow: p.PollNow,
		Started: time.Now(),
	})

	// Envolver el handler con GET /fingerprint (sin auth) si on-box.
	if cfg.Onbox {
		fp := serverFP
		fpMux := http.NewServeMux()
		fpMux.HandleFunc("GET /fingerprint", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"spki_sha256":%q}`, fp)
		})
		fpMux.Handle("/", handler)
		handler = fpMux
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           withSSEWriteTimeout(handler, sseWriteTimeout),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 1)
	go func() {
		scheme := "http"
		if cfg.Onbox {
			scheme = "https"
			srv.TLSConfig = tlsConf
		}
		log.Printf("[netpulse] v%s · modo %s · %s://localhost:%d", httpapi.Version, adapter.Mode(), scheme, cfg.Port)
		staticDesc := staticDir
		if staticDesc == "" {
			staticDesc = "(embed)"
		}
		log.Printf("[netpulse] datos: %s · estáticos: %s", cfg.DataDir, staticDesc)
		p.Start()
		upd.Start()
		if eventsCollector != nil {
			eventsCollector.Start()
		}
		if cfg.Onbox {
			errCh <- srv.ListenAndServeTLS("", "")
		} else {
			errCh <- srv.ListenAndServe()
		}
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
		if eventsCollector != nil {
			eventsCollector.Stop()
		}
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
		if n == nil {
			continue
		}
		// Defensa en profundidad (issue #57): si un nil-encapsulado entra en
		// la cadena, la interfaz no es nil pero el valor sí. No debe paniquear.
		if reflect.ValueOf(n).Kind() == reflect.Ptr && reflect.ValueOf(n).IsNil() {
			continue
		}
		n.Notify(ev)
	}
}
