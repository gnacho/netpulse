# NetPulse

<p align="center">
  <a href="README.md">English</a> |
  <a href="README.es.md">Español</a>
</p>

<p align="center">
  <a href="https://github.com/gnacho/netpulse/releases"><img alt="Release" src="https://img.shields.io/github/v/release/gnacho/netpulse"></a>
  <a href="https://github.com/gnacho/netpulse/actions/workflows/release.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/gnacho/netpulse/release.yml?branch=main"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/gnacho/netpulse"></a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/hero-en-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/hero-en-light.png">
    <img alt="NetPulse overview with a network health score, live traffic chart, AdGuard Home stats, WireGuard peers and the alerts feed" src="assets/hero-en-light.png" width="800">
  </picture>
</p>

NetPulse is a read-only PWA for monitoring a home network built on
OpenWrt/GL.iNet routers: fleet status, per-router health, connected devices,
a live topology map, WireGuard peers, AdGuard Home stats and alerts, in real
time. One static Go binary with the frontend embedded, self-hosted on a
small Linux box.

## Why does this exist?

I believe in digital sovereignty: if a device makes you depend on its
cloud, its firmware or its vendor, it isn't 100% yours. That's why I've
always favored hardware I can flash or root. My home layout ended up with
four routers: a Flint 2 as the main one and three Xiaomi AX6 access points
bought second-hand for 30 euros each. Cheap, powerful, and all running
OpenWrt. That sovereignty let me orchestrate and personalize the network
exactly how I wanted (not without some challenges), but I always missed a
unified view of what was going on: what connects where, what's healthy,
what isn't. There was nothing out there, or I couldn't find it, so I built
it. NetPulse is that global viewer: it analyzes your network, spots
anomalies, and warns you.

## Why this stack?

- **Go, single static binary**: a 24/7 monitor on a small LXC. The stdlib
  `net/http` ServeMux, no framework, `go:embed` for the frontend. Upgrade
  is swapping one file.
- **`modernc.org/sqlite`, CGO off**: fully static, no C toolchain needed
  on the target. Time series, users and sessions in one embedded SQLite
  file (WAL).
- **Read-only by design**: the server generates its own ed25519 keypair;
  you authorize the public key on each router and it only ever reads
  (ubus, `/proc`, iwinfo, `bridge fdb`, `wg show`). It cannot change your
  network.
- **React 19 + Vite + Tailwind PWA**: installable, live over SSE (5 s),
  the same UI shell as my other apps.
- **systemd, no Docker**: it monitors a network; it doesn't need a
  container to do it.

## Features

- **Fleet overview**: health score, live traffic, latency, per-router
  status (CPU, memory, temperature, uptime).
- **Live topology map**: inferred from the bridge FDB (and LLDP when
  available), with wired/wireless clients, switches and hypervisors
  detected, WireGuard tunnels drawn peer to Internet.
- **Devices**: every client with type classification (hostname patterns +
  OUI), first seen, band, signal.
- **WireGuard**: peers, latest handshakes, transfer per peer.
- **AdGuard Home**: query stats and top blocked domains.
- **Alerts**: temperature, firmware available, new device, handshake, with
  a bell feed.
- **Multi-user auth**: bcrypt passwords, per-user language (ES/EN), admin
  and viewer roles.
- **Demo mode** (`DEMO_MODE=1`): a 67-device sample network, no routers
  needed.
- **Optional collector sidecar**: TCP latency probes per router with its
  own long-term time series.

## Screenshots

**Topology: inferred live from the bridge FDB, tunnels included**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-topology-en-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-topology-en-light.png">
  <img alt="Topology map with the gateway in the center, three access points, wired and wireless clients, an inferred switch and the WireGuard tunnel to Internet" src="assets/screenshot-topology-en-light.png" width="800">
</picture>

**Devices: every client classified, with band and signal**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-devices-en-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-devices-en-light.png">
  <img alt="Device list with type icons, hostname, IP, band, signal strength and the router each client is attached to" src="assets/screenshot-devices-en-light.png" width="800">
</picture>

**Routers: per-router health at a glance**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-router-en-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-router-en-light.png">
  <img alt="Routers view with per-router cards showing model, firmware, CPU, memory, temperature and uptime" src="assets/screenshot-router-en-light.png" width="800">
</picture>

## What to expect

NetPulse is a personal project, built for my own network and released as
free software (AGPL-3.0). It is and will always be free. I work on it in my
free time: there's a long list of ideas, but little time, and it evolves
following my own needs first. With contributions or support it might grow
faster, but I can't promise anything. **Honest scope note**: so far it has
only been tested with my own hardware (a GL.iNet Flint 2 gateway and three
Xiaomi AX6 access points running OpenWrt) plus WireGuard and AdGuard Home.
Other OpenWrt devices should work, but yours would be the first to tell.

## Roadmap

| Phase | Status | Highlights |
|---|---|---|
| **1 — Read-only base** | ✅ | React PWA, read-only SSH polling (ubus, `/proc`, iwinfo), AdGuard Home + WireGuard, multi-user auth, LAN discovery, backend migrated from Node to Go (single binary with embedded app) |
| **5 — Topology v5** | ✅ | Real semantic map: live FDB + LLDP, backhaul, managed vs. inferred switches, hypervisors with nested CTs, time-series collector |
| **6 — Alerts, Push & agent (pilot)** | ✅ | Alerts with 6 categories and per-category config (urgent/all/none), native Web Push (VAPID), on-demand refresh, switch/bridge audit with reconciled data canon, OpenWrt agent pilot (token ingest, SSH fallback, procd) |
| **6.5 — Versioned view-model** | ✅ | API as a presentation view-model (`vm: 1`), single-sourced demo canon in Go → JSON, server-stamped `Device.infra`, semantic topology in the snapshot, Settings revamp (greeting name, real system info, streamlined AdGuard/users/routers) |
| **8 — Agent resilience** | ✅ | Router-side watchdog + heartbeat (v2.3.0), server-side manual rearm (v2.4.0), TTL auto-rearm (v2.4.0) and admin-only mutation routes (v2.4.1) |
| **7 — TLS & hardening** | 🔮 | HTTPS on CT 226 (unlocks real Web Push), HMAC-SHA256 on agent ingest, serve the agent binary from the server itself |
| **9 — Agent deep dive** | ⏳ | Native netlink/nl80211, real-time ubus events, `.ipk` package, real-hardware benchmarking |
| **10 — On-router client** | 🔮 | `luci-app-netpulse`: lightweight LuCI client consuming the versioned view-model (6.5 makes the API ready for this) |
| **11 — Write/orchestration** | 🔮 | Plan → apply → state (Terraform pattern), transactional `uci`, strict allowlist; starts with AdGuard Home |
| **Backlog** | 📋 | Integrate collector series into server-go · real push verification (FCM) · series retention + weekly availability report |

Full detail in [docs/ROADMAP.md](docs/ROADMAP.md).

## Installation

Requirements: Linux (x86_64, arm64 or armv7) with systemd.

```bash
curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install.sh | sh   # (recommended)
```

The installer is plain, readable shell: [inspect it first](install.sh). It
detects your distro and arch, downloads the verified release (sha256
against `checksums.txt`), creates a sandboxed `netpulse` systemd service
and prints the initial admin password once. Update by re-running the same
line; remove with `sh install.sh --uninstall`.

Optional latency sidecar (time series of TCP probes to each router):

```bash
curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install-collector.sh | sh
```

Stable binaries are published per `v*` tag (goreleaser); rolling per-commit
builds live in the `go-latest` prerelease for the in-app updater.

## Connecting your routers

The server generates its own ed25519 keypair and shows the public key in
Settings. Authorize it on each router you want to monitor
(`/etc/dropbear/authorized_keys`). The gateway is auto-detected on first
boot via LAN discovery (TCP :22 sweep, ubus/GL-UI fingerprint); the rest
are added from Settings. Polling is strictly read-only.

## Development

```bash
# Backend (Go; serves app/dist via go:embed)
cd server-go
cp ../app/dist internal/staticspa/dist -r   # the embedded dist is never tracked
go build -o netpulse ./cmd/netpulse && DEMO_MODE=1 ./netpulse

# Frontend (dev server with proxy)
cd app
npm install
npm run dev
```

The legacy Node backend (`server/`) is archived as a documented fallback;
migration from its database happens automatically on the first Go boot.

## Tests

```bash
cd server-go && go test ./...
```

## License

[AGPL-3.0](LICENSE)
