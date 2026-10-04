// Package mqttpub publishes the NetPulse fleet state to an MQTT broker with
// Home Assistant MQTT Discovery (#825).
//
// It is opt-in (NETPULSE_MQTT_ENABLED=1 plus a broker host), disabled in demo
// mode and fail-silent: a broker that is down never affects the server. Only
// publishing is used, so there is no subscription and no command surface.
package mqttpub

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gonzalop/mq"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

const (
	defaultPort     = 1883
	defaultInterval = 30 * time.Second
	initialBackoff  = 5 * time.Second
	maxBackoff      = 2 * time.Minute
	publishTimeout  = 10 * time.Second
)

// Config is the publisher configuration.
type Config struct {
	Enabled  bool
	Host     string
	Port     int
	User     string
	Pass     string
	Instance string
	Interval time.Duration
}

// ConfigFromEnv reads the configuration from the environment. lookup follows
// the os.LookupEnv shape so it can be injected in tests. Missing values fall
// back to defaults; an enabled config without a host is disabled.
func ConfigFromEnv(lookup func(string) (string, bool)) Config {
	cfg := Config{Port: defaultPort, Interval: defaultInterval, Instance: "default"}
	if v, ok := lookup("NETPULSE_MQTT_ENABLED"); ok {
		cfg.Enabled = isTrue(v)
	}
	if v, ok := lookup("NETPULSE_MQTT_HOST"); ok {
		cfg.Host = strings.TrimSpace(v)
	}
	if v, ok := lookup("NETPULSE_MQTT_PORT"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			cfg.Port = n
		}
	}
	if v, ok := lookup("NETPULSE_MQTT_USER"); ok {
		cfg.User = strings.TrimSpace(v)
	}
	if v, ok := lookup("NETPULSE_MQTT_PASS"); ok {
		cfg.Pass = v
	}
	if v, ok := lookup("NETPULSE_MQTT_INSTANCE"); ok && strings.TrimSpace(v) != "" {
		cfg.Instance = strings.TrimSpace(v)
	}
	if v, ok := lookup("NETPULSE_MQTT_INTERVAL"); ok {
		if sec, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && sec > 0 {
			cfg.Interval = time.Duration(sec) * time.Second
		}
	}
	cfg.Instance = sanitize(cfg.Instance)
	if cfg.Host == "" {
		cfg.Enabled = false
	}
	return cfg
}

func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// Publisher keeps the broker connection and publishes the state periodically.
type Publisher struct {
	cfg      Config
	version  string
	snapshot func() *adapters.Overview
	demo     bool

	seenAlerts  map[string]struct{}
	seenInit    bool
	lastRouters string
}

// New builds a publisher. snapshot returns the latest overview (nil allowed
// before the first poll).
func New(cfg Config, version string, snapshot func() *adapters.Overview, demo bool) *Publisher {
	return &Publisher{
		cfg:        cfg,
		version:    version,
		snapshot:   snapshot,
		demo:       demo,
		seenAlerts: map[string]struct{}{},
	}
}

// Enabled reports whether the publisher will run.
func (p *Publisher) Enabled() bool { return p.cfg.Enabled && !p.demo && p.snapshot != nil }

// Start runs the publisher until ctx is cancelled. It is a no-op when not
// enabled, so callers can start it unconditionally.
func (p *Publisher) Start(ctx context.Context) {
	if !p.Enabled() {
		if p.cfg.Enabled && p.demo {
			log.Printf("[mqtt] disabled in demo mode")
		}
		return
	}
	go p.run(ctx)
}

func (p *Publisher) run(ctx context.Context) {
	backoff := initialBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		client, err := p.dial(ctx)
		if err != nil {
			log.Printf("[mqtt] connect to %s:%d failed: %v", p.cfg.Host, p.cfg.Port, err)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = initialBackoff
		log.Printf("[mqtt] connected to %s:%d as instance %q", p.cfg.Host, p.cfg.Port, p.cfg.Instance)

		p.publishAvailability(client, "online")
		p.publishDiscovery(client)
		p.publishCycle(client)

		p.loop(ctx, client)

		// The retained availability must not stay "online" after an
		// intentional stop (a graceful DISCONNECT suppresses the will).
		p.publishAvailability(client, "offline")
		_ = client.Disconnect(context.Background())
		return
	}
}

func (p *Publisher) dial(ctx context.Context) (*mq.Client, error) {
	addr := fmt.Sprintf("tcp://%s:%d", p.cfg.Host, p.cfg.Port)
	opts := []mq.Option{
		mq.WithProtocolVersion(mq.ProtocolV311),
		mq.WithClientID("netpulse-" + p.cfg.Instance),
		mq.WithKeepAlive(60 * time.Second),
		mq.WithAutoReconnect(true),
		mq.WithReconnectBackoff(2*time.Second, maxBackoff, true),
		mq.WithConnectTimeout(15 * time.Second),
		mq.WithWill(availabilityTopic(p.cfg.Instance), []byte("offline"), 1, true),
		mq.WithOnConnect(func(c *mq.Client) {
			// Fires on the initial connect and on every reconnect.
			p.publishAvailability(c, "online")
			p.publishDiscovery(c)
			p.publishCycle(c)
		}),
		mq.WithOnConnectionLost(func(_ *mq.Client, err error) {
			log.Printf("[mqtt] connection lost: %v", err)
		}),
	}
	if p.cfg.User != "" {
		opts = append(opts, mq.WithCredentials(p.cfg.User, p.cfg.Pass))
	}
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return mq.DialContext(dctx, addr, opts...)
}

// loop publishes on the configured interval until ctx ends.
func (p *Publisher) loop(ctx context.Context, client *mq.Client) {
	interval := p.cfg.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if client.IsConnected() {
				p.publishCycle(client)
			}
		}
	}
}

// publishCycle publishes the instance status, every router state and any new
// alert. It also republishes discovery when the set of routers changes.
func (p *Publisher) publishCycle(client *mq.Client) {
	ov := p.snapshot()
	if ov == nil {
		return
	}
	p.publishStatus(client, ov)
	p.publishRouters(client, ov)
	p.publishNewAlerts(client, ov)

	key := routerSetKey(ov)
	if key != p.lastRouters {
		p.lastRouters = key
		p.publishDiscovery(client)
	}
}

// publishNewAlerts emits an event per unseen alert. The first overview only
// seeds the set, so a fresh server start does not replay the whole backlog.
func (p *Publisher) publishNewAlerts(client *mq.Client, ov *adapters.Overview) {
	if !p.seenInit {
		for _, a := range ov.Alerts {
			if a.ID != "" {
				p.seenAlerts[a.ID] = struct{}{}
			}
		}
		p.seenInit = true
		return
	}
	for _, a := range ov.Alerts {
		if a.ID == "" {
			continue
		}
		if _, ok := p.seenAlerts[a.ID]; ok {
			continue
		}
		p.seenAlerts[a.ID] = struct{}{}
		payload, err := json.Marshal(alertEvent{
			ID: a.ID, Severity: a.Severity, Title: a.Title, Description: a.Description,
			RouterID: a.RouterID, Urgent: a.Urgent, Ts: a.Ts,
		})
		if err != nil {
			continue
		}
		publish(client, eventTopic(p.cfg.Instance, "alert"), payload, false)
	}
}

func (p *Publisher) publishAvailability(client *mq.Client, value string) {
	publish(client, availabilityTopic(p.cfg.Instance), []byte(value), true)
}

// publish sends one payload at QoS 1 (retained when retain is true).
func publish(client *mq.Client, topic string, payload []byte, retain bool) {
	if !client.IsConnected() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	opts := []mq.PublishOption{mq.WithQoS(mq.AtLeastOnce)}
	if retain {
		opts = append(opts, mq.WithRetain(true))
	}
	if err := client.Publish(ctx, topic, payload, opts...).Wait(ctx); err != nil {
		log.Printf("[mqtt] publish %s: %v", topic, err)
	}
}

// sanitize makes a string safe for use as an MQTT topic level.
func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	return strings.NewReplacer("/", "-", "+", "-", "#", "-", " ", "-").Replace(s)
}

// sleepCtx sleeps for d, returning false when ctx ends first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// envLookup is the production lookup (os.LookupEnv), used by main.
func envLookup(k string) (string, bool) { return os.LookupEnv(k) }

// ConfigFromProcessEnv reads the configuration from the process environment.
func ConfigFromProcessEnv() Config { return ConfigFromEnv(envLookup) }
