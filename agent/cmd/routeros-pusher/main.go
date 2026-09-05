// routeros-pusher — external pusher for MikroTik RouterOS boxes.
//
// Unlike netpulse-agent (which runs locally on OpenWrt and shells out to
// ubus/proc), RouterOS is a closed appliance OS: there is nowhere to run a
// Go binary on the device itself. RouterOS 7+ does expose a REST API
// remotely (https://<router>/rest/...), so this tool polls that API from
// wherever it runs (the NetPulse host, a container, a cron box) and pushes
// the result to NetPulse's existing external-pusher ingest endpoint
// (POST /api/ingest/agent, Kind: "external") — no server-side changes
// needed, same wire format as agent/probe.Payload.
//
// Config (env):
//
//	ROUTEROS_HOST     RouterOS REST API base URL (e.g. https://192.168.1.1)
//	ROUTEROS_USER     RouterOS API user
//	ROUTEROS_PASS     RouterOS API password
//	ROUTEROS_INSECURE skip TLS verification ("1"; RouterOS ships self-signed certs)
//	NETPULSE_SERVER   NetPulse server URL (e.g. https://netpulse.local:3000)
//	NETPULSE_TOKEN    agent token for the slug (created via POST /api/agents)
//	NETPULSE_SLUG     router slug in NetPulse
//	NETPULSE_INTERVAL push interval, Go duration (default "30s")
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// Version del pusher (se reporta en cada push).
var Version = "0.1.0"

type routerOSResource struct {
	CPULoad      string `json:"cpu-load"`
	FreeMemory   string `json:"free-memory"`
	TotalMemory  string `json:"total-memory"`
	Uptime       string `json:"uptime"`
	Version      string `json:"version"`
	BoardName    string `json:"board-name"`
	Architecture string `json:"architecture-name"`
}

type routerOSIdentity struct {
	Name string `json:"name"`
}

// durationRe: RouterOS uptime strings look like "1w2d3h4m5s" — any subset
// of those units, in that order, no separators.
var durationRe = regexp.MustCompile(`(\d+)([wdhms])`)

// parseRouterOSUptime converts a RouterOS uptime string to seconds.
// ponytail: regex parse, no full grammar — RouterOS's format is fixed and
// documented, upgrade to a real parser only if a future RouterOS version
// changes it.
func parseRouterOSUptime(s string) (float64, error) {
	matches := durationRe.FindAllStringSubmatch(s, -1)
	if matches == nil {
		return 0, fmt.Errorf("routeros-pusher: unparseable uptime %q", s)
	}
	unitSeconds := map[string]float64{"w": 604800, "d": 86400, "h": 3600, "m": 60, "s": 1}
	var total float64
	for _, m := range matches {
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, fmt.Errorf("routeros-pusher: bad uptime component %q: %w", m[0], err)
		}
		total += n * unitSeconds[m[2]]
	}
	return total, nil
}

// buildSystemData maps a RouterOS resource+identity pair to the SystemData
// shape NetPulse already understands (SPEC-AGENTE-PILOTO §1 wire format).
func buildSystemData(res routerOSResource, id routerOSIdentity) (*probe.SystemData, error) {
	cpuLoad, err := strconv.Atoi(res.CPULoad)
	if err != nil {
		return nil, fmt.Errorf("routeros-pusher: bad cpu-load %q: %w", res.CPULoad, err)
	}
	uptime, err := parseRouterOSUptime(res.Uptime)
	if err != nil {
		return nil, err
	}
	total, _ := strconv.ParseFloat(res.TotalMemory, 64)
	free, _ := strconv.ParseFloat(res.FreeMemory, 64)

	sys := &probe.SystemData{CPU: &cpuLoad}
	sys.SysInfo = &probe.SysInfo{Uptime: uptime}
	sys.SysInfo.Memory.Total = total
	sys.SysInfo.Memory.Free = free
	sys.Board = &probe.BoardInfo{
		Model:     res.BoardName,
		Hostname:  id.Name,
		System:    "routeros",
		BoardName: res.BoardName,
	}
	sys.Board.Release.Version = res.Version
	sys.Board.Release.Target = res.Architecture
	return sys, nil
}

func fetchJSON(ctx context.Context, client *http.Client, url, user, pass string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return fmt.Errorf("routeros-pusher: GET %s: %s: %s", url, resp.Status, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func fetchRouterOS(ctx context.Context, client *http.Client, host, user, pass string) (*probe.SystemData, error) {
	var res routerOSResource
	if err := fetchJSON(ctx, client, host+"/rest/system/resource", user, pass, &res); err != nil {
		return nil, err
	}
	var id routerOSIdentity
	if err := fetchJSON(ctx, client, host+"/rest/system/identity", user, pass, &id); err != nil {
		return nil, err
	}
	return buildSystemData(res, id)
}

func pushPayload(ctx context.Context, client *http.Client, server, token string, p probe.Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/api/ingest/agent", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return fmt.Errorf("routeros-pusher: POST ingest: %s: %s", resp.Status, respBody)
	}
	return nil
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("routeros-pusher: missing required env var", "var", key)
		os.Exit(1)
	}
	return v
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	routerosHost := mustEnv("ROUTEROS_HOST")
	routerosUser := mustEnv("ROUTEROS_USER")
	routerosPass := mustEnv("ROUTEROS_PASS")
	server := mustEnv("NETPULSE_SERVER")
	token := mustEnv("NETPULSE_TOKEN")
	slug := mustEnv("NETPULSE_SLUG")

	interval := 30 * time.Second
	if v := os.Getenv("NETPULSE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			slog.Error("routeros-pusher: bad NETPULSE_INTERVAL", "err", err)
			os.Exit(1)
		}
		interval = d
	}

	routerosClient := &http.Client{Timeout: 10 * time.Second}
	if os.Getenv("ROUTEROS_INSECURE") == "1" {
		routerosClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // opt-in for RouterOS self-signed certs
	}
	netpulseClient := &http.Client{Timeout: 10 * time.Second}

	ctx := context.Background()
	// ponytail: fixed-interval loop, no backoff/jitter — a failed tick just
	// waits for the next one. Add real backoff if flapping RouterOS
	// connectivity turns out to spam logs/NetPulse.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		sys, err := fetchRouterOS(ctx, routerosClient, routerosHost, routerosUser, routerosPass)
		if err != nil {
			slog.Error("routeros-pusher: fetch failed", "err", err)
		} else {
			payload := probe.Payload{
				Router:   slug,
				Ts:       time.Now().Unix(),
				Version:  Version,
				Kind:     "external",
				Interval: int(interval.Seconds()),
				Data:     probe.PayloadData{System: sys},
			}
			if err := pushPayload(ctx, netpulseClient, server, token, payload); err != nil {
				slog.Error("routeros-pusher: push failed", "err", err)
			} else {
				slog.Info("routeros-pusher: pushed", "slug", slug)
			}
		}
		<-ticker.C
	}
}
