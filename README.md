# NetPulse

<p align="center">
  <a href="README.md">English</a> |
  <a href="README.es.md">Español</a>
</p>

<p align="center">
  <a href="https://netpulse.cloudless.club"><img alt="Website" src="https://img.shields.io/badge/Website-netpulse.cloudless.club-blue"></a>
  <a href="https://demo.netpulse.cloudless.club"><img alt="Live demo" src="https://img.shields.io/badge/Live%20demo-demo.netpulse.cloudless.club-blue"></a>
  <a href="https://github.com/gnacho/netpulse/releases"><img alt="Release" src="https://img.shields.io/github/v/release/gnacho/netpulse"></a>
  <a href="https://github.com/gnacho/netpulse/actions/workflows/release.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/gnacho/netpulse/release.yml?branch=main"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/gnacho/netpulse"></a>
  <a href="https://ko-fi.com/gnacho"><img alt="Support on Ko-fi" src="https://img.shields.io/badge/Ko--fi-Donate-ff5e5b?logo=ko-fi&logoColor=white"></a>
</p>

<p align="center"><a href="https://demo.netpulse.cloudless.club"><strong>Try the live demo</strong></a> on <code>demo.netpulse.cloudless.club</code></p>

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

> **Try the live demo**
>
> See it running without installing anything. Head to **[demo.netpulse.cloudless.club](https://demo.netpulse.cloudless.club)** — a full sample network, no sign-up required. In read-only mode, so you can explore freely.

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

## What's new

Each release is pushed to the fleet through the built-in auto-updater. Newer
versions surface a short, human-readable "what changed" panel in the app so
you know what to expect before you update. Here is a running summary of the
latest improvements, each linked to the GitHub issue that drove it.

> These are written for people, not changelogs: what the feature does for you,
> not the function names behind it.

### Latest (v2.28.20)

- **Quiet clients stay on their AP** ([#678](https://github.com/gnacho/netpulse/issues/678)): a wired device behind a bridged AP that goes silent no longer drifts to the main router — the server remembers where the cable actually is, and only local sightings count.
- **VPN tunnel lines follow the nodes** ([#677](https://github.com/gnacho/netpulse/issues/677)): rearranging the map no longer leaves WireGuard curves pointing at empty space; they are drawn from the peer to wherever Internet actually is.
- **The agent recovers on its own after a long outage** ([#680](https://github.com/gnacho/netpulse/issues/680)): stale buffered payloads are discarded instead of deadlocking the queue with anti-replay 401s.

### Earlier (v2.28.19)

- **Alerts (and everything else) render in your language** ([#671](https://github.com/gnacho/netpulse/issues/671)). Alert titles, descriptions and suggestions used to be server-generated Spanish, so English users saw a mixed feed. Events now carry their type and values and the app translates them in the active language — the full family, from unknown devices to port alerts.
- **Your hostnames survive the browser translator** ([#666](https://github.com/gnacho/netpulse/issues/666)). The page used to declare Spanish even with an English UI, which made browsers offer to translate it — and the translator happily "corrected" hostnames like crowed-pixeltab into crowded-pixeltab. The declared language now follows the UI and network data is marked do-not-translate.
- **OpenWrt packages for x86/64** ([#672](https://github.com/gnacho/netpulse/issues/672)). The agent and on-router server are now published for x86/64 systems (24.10 .ipk, 25.12 .apk), with the package architecture derived from the SDK.

### Earlier (v2.28.18)

- **The topology map now reflects where things are actually plugged, and it stops jumping around** ([#656](https://github.com/gnacho/netpulse/issues/656)). Wired clients of a bridged access point (a dumb AP on the same LAN as the main router) show under that AP instead of drifting up to the parent; quiet devices (TVs, receivers, media players) keep their place on the map instead of falling off their switch whenever they go silent; and client icons no longer swap spots on every refresh.
- **The layout is yours to adjust** ([#656](https://github.com/gnacho/netpulse/issues/656)). A new edit mode (admin only) lets you drag routers, switches and devices to bring satellites closer or push them apart; once saved, the layout stays frozen and only changes when you edit it again or reset to the automatic arrangement.
- **Proxmox hosts show as proper nodes with their VMs nested underneath** ([#561](https://github.com/gnacho/netpulse/issues/561)). Each PVE host renders as a round node labeled with its name, its containers grouped beneath it with a +N badge, and dragging the host moves its whole VM family. The device card also gained a discreet "Tag" button so admins can label a device without typing its MAC.

### Earlier (v2.28.17)

- **An OpenWrt router no longer shows a false "host key changed" when polled** ([#651](https://github.com/gnacho/netpulse/issues/651)). Discovery (OpenSSH with `accept-new`) used to pin only the single host-key algorithm it negotiated (ed25519), while the polling pool (Go) negotiates another one (ecdsa/rsa) with the same server; a router with several host keys then showed a spurious "ssh host key changed" and demanded a pointless re-onboard. Discovery now pins **all** of the router's host keys (ed25519, ecdsa and rsa), so the poller always finds the one it negotiates, without weakening the anti-MITM protection: if a router changes every key (reflash), the "host key changed" detection still kicks in.

### Earlier (v2.28.16)

- **WireGuard peers are now labeled by their OpenWrt-configured name** ([#663](https://github.com/gnacho/netpulse/issues/663)). In the topology, WireGuard peers that weren't already mapped to a NetPulse device now show the human-readable name from the peer's UCI description (priority: NetPulse device name → UCI description → tunnel IP) instead of the raw tunnel IP. The poll reads `wg show dump` and `uci show network` in a single SSH session and preserves a down tunnel's state.

### Fixed

- **A native OpenWrt router no longer shows as "Managed switch" in the devices table** ([#660](https://github.com/gnacho/netpulse/issues/660)). The Role column only distinguished gateway and "agent only", and everything else fell into "Managed switch"; it now switches on the device type and shows "OpenWrt" for native (SSH agent) routers, leaving "Managed switch"/External for SNMP-polled ones.
- **An SNMP-polled switch no longer loses its traffic history or connected devices** ([#661](https://github.com/gnacho/netpulse/issues/661)). The SNMP path computed the per-port byte counters and rates but never persisted them, so the switch's traffic history was empty even though the poll read the data (now via `recordPortSamples`, the same path as the SSH/agent flows). The FDB also no longer drops MACs whose index didn't resolve to a known port (before, the switch showed 0 devices despite having clients) and there's a Q-BRIDGE MIB fallback for switches that only expose it that way. The fleet card now plots the switch's aggregate traffic in bps, and the port chart of an SNMP switch uses bps (beacons without byte counters keep using fps).

### Earlier (v2.28.15)

- **Port traffic charts now show frames/s for devices that don't count bytes** ([#641](https://github.com/gnacho/netpulse/issues/641)). RTLPlayground switch beacons (KP-9000) only report cumulative frame counters, not bytes. The server now derives frames/s from those counters (raw, 5m and daily) and the port card in the detail page plots fps instead of bps for sources without byte counters, so a managed switch port chart no longer looks empty.
- **Fleet cards and the router detail show the real per-band client split** ([#645](https://github.com/gnacho/netpulse/issues/645)). Each router card now lists its online clients by band (2.4/5/6 GHz and wired) computed server-side from the same source as the total, and only renders bands that actually have clients: a switch with no WiFi radios no longer shows WiFi bands stuck at zero.
- **The traffic sparkline of a switch without bps throughput is no longer a flat zero line** ([#648](https://github.com/gnacho/netpulse/issues/648)). For sources that only report per-port frames, the card's 24h traffic mini-chart is now derived from its ports' frames/s instead of the bps table (which was always empty for them), so the KP-9000 card shows its real activity.
- **RTLPlayground beacon switches report real firmware, uptime and MAC** ([#639](https://github.com/gnacho/netpulse/issues/639)). The server polls the switch's HTTP console for beacon agents, and the router card now shows the actual firmware version, uptime and MAC of the switch instead of placeholders.

### Fixed

- **A managed switch detail page no longer shows its ports twice** ([#644](https://github.com/gnacho/netpulse/issues/644)). The front-panel LED chassis card duplicated the Ethernet ports card with RJ45 jacks; the front panel is gone and the ports card already shows link, health and per-port history.
- **Port series of a beacon switch are no longer polluted with zeros** ([#642](https://github.com/gnacho/netpulse/issues/642)). External sources only persist their real counters now, keeping the port series clean.

### Earlier (v2.28.14)

- **The channel plan no longer counts your own mesh as interference, and won't suggest a channel that crosses into DFS** ([#631](https://github.com/gnacho/netpulse/issues/631)). In the channel analysis, the BSSIDs of your own access points no longer score as neighbors (they are excluded by matching the MAC prefix of your monitored routers), and candidate channels are validated against the radio width: at 40/80/160 MHz only blocks that stay out of DFS channels (52-64, 100-144) are offered. This stops NetPulse recommending a channel your mesh satellites already use, or an 80 MHz block that would cross into a DFS band.
- **Rearm now recovers an agent stuck in a 401 loop** ([#630](https://github.com/gnacho/netpulse/issues/630)). If a restart doesn't bring the agent back, NetPulse regenerates the agent token and applies it on the router (rewrites the env + restarts) without a reinstall, so a token that drifted out of sync is fixed in place. Token rotation on a reinstall is also atomic: if the SSH step fails, the server restores the previous token so the agent never ends up pushing a token that is no longer accepted.

### Earlier (v2.28.13)

- **A cleaner Settings page, one column at a time** ([#627](https://github.com/gnacho/netpulse/issues/627)). The Settings page is now a single column with a sticky index that follows you as you scroll, so each card (Appearance, Data & thresholds, Services, Network, Integrations, Administration, Account, About) is full-width and easier to use. The Appearance card shows real theme previews (light/dark/system) and a two-column palette; the WAN speed test runs a real test from the server and fills download/up in place. Labs toggles appear individually (Orchestration, Channels, Upgrades) once Labs is on.
- **Regenerate an agent token without reinstalling** ([#627](https://github.com/gnacho/netpulse/issues/627)). When you regenerate a router's agent token from Settings, NetPulse now pushes the new token to the router and restarts the agent in place, so the device keeps reporting without a reinstall. The install one-liner is only needed when the router can't be reached over SSH.
- **Actualizaciones: autodetect the firmware image** ([#629](https://github.com/gnacho/netpulse/issues/629)). On the firmware upgrades page, NetPulse looks up the router's image from its own firmware (board + target + version) on the OpenWrt download index and prefills the URL and checksum, with an "Autodetect image" button. Manual entry stays available when it can't be resolved.
- **Uninstall the agent on tight routers** ([#624](https://github.com/gnacho/netpulse/issues/624)). Routers with little free space (like the UniFi 6 Lite) couldn't fit a reinstall; there's now a clean "Uninstall" action that stops and removes the agent.

### Fixed

- **Copy buttons show the raw i18n key** ([#628](https://github.com/gnacho/netpulse/issues/628)). The copy buttons in the adoption card used a missing `common.copy` key, so their tooltip showed `common.copy`; the key now exists and translates to "Copy"/"Copiar".

### Earlier (v2.28.12)

- **Set a custom SSH port per router** ([#605](https://github.com/gnacho/netpulse/issues/605)). Not every router keeps its SSH daemon on port 22 - some run it elsewhere. Now each router can be given its own SSH port when you add or edit it, and every server-side path (probing, actions, agent install, gateway discovery) uses it automatically. Leave it empty and NetPulse keeps using 22.
- **Roaming matrix grouped per device** ([#600](https://github.com/gnacho/netpulse/issues/600)). In WiFi Roaming → Matrix, columns are now grouped under the access point's name, with a header row that labels each band (2.4G/5G). No more a loose column per AP-band.
- **Cleaner 802.11r wording and no phantom devices** ([#601](https://github.com/gnacho/netpulse/issues/601)). The section title is now "By AP" instead of "By router", the column is "Device", and devices without radios (like the gateway) no longer show up in the per-router detail. Routers discovered automatically are also named after their hostname, not their hardware model, so the UI shows device names across the app.
- **Own channel width in the channel scan** ([#602](https://github.com/gnacho/netpulse/issues/602)). In the channel analysis, the row for your own network now shows the channel width (for example `1 · 20 MHz`) next to the channel number, cross-referenced with the router's channel plan.
- **The UI no longer goes black when a WireGuard client connects** ([#592](https://github.com/gnacho/netpulse/issues/592)). NetPulse reports a WireGuard peer's type as plain text, and uses "unknown" when it cannot match a type. The Home card built the peer icon with no fallback, so an unknown (or unmatched) peer produced a blank icon and React crashed the whole page the moment a WireGuard client connected (or when you opened the UI from a connected client). NetPulse now has a fallback icon for these, plus a known "unknown" type, so the page keeps rendering.

### Earlier (v2.28.11)

- **The agent no longer degrades your Wi-Fi while scanning** ([#591](https://github.com/gnacho/netpulse/issues/591)). The per-router agent used to run a full active Wi-Fi scan on every probe, so ~every 30-37s per device. On access points that pulls the radio off channel and drops IoT/weak clients. It now scans at most every 10 minutes, after boot, or immediately when the server asks for a refresh - and the embedded agent version bumps so the fleet updates itself.

### What gets discovered

NetPulse discovers client devices from three sources that run on the monitored routers:

1. **DHCP leases** - every time the agent polls, it reads the local DHCP lease table.
2. **Bridge FDB** - wired clients are learned from the switch bridge forwarding database.
3. **mDNS / Bonjour** - when `umdns` is installed on OpenWrt, the agent browses advertised services and resolves hostnames.

LLDP is used only to identify neighbouring routers/switches, not end devices. Discovery requires the NetPulse agent to be running on the router that sees the clients; a fresh install with only manually onboarded routers and no agent on the gateway will show an empty device list until the agent is installed.

Per-client traffic prefers `nlbwmon` for wired clients (per-MAC counters) and falls back to the hostapd byte counters for Wi-Fi clients. `nlbwmon` is not included in every OpenWrt build and can be memory-hungry on very low-RAM routers; `vnstat` tracks per-interface totals, not per-client MAC traffic, so it is not a substitute. If a wired client shows no traffic series, install `nlbwmon` on that router (or rely on the Wi-Fi fallback).

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

## User manual

Area by area and menu by menu, what each screen shows, what data it needs,
how to read it and the design decisions behind it, plus step-by-step
procedures (add a router, read the health score, use the topology map,
interpret the channel plan, schedule a firmware upgrade, reserve an IP,
block a device):

- [Manual de usuario (español)](docs/manual.es.md)
- [User manual (English)](docs/manual.en.md)

## Roadmap

| Phase | Status | Highlights |
|---|---|---|
| **1 — Topology v5** | ✅ | Semantic map: live FDB + LLDP, backhaul, managed vs. inferred switches, hypervisors with nested CTs, time-series collector |
| **2 — Alerts, Push & agent pilot** | ✅ | 6-category alerts, native Web Push (VAPID), on-demand refresh, OpenWrt agent pilot (token ingest, SSH fallback, procd) |
| **3 — Read-only base + Node→Go** | ✅ | React PWA, read-only SSH polling (ubus, /proc, iwinfo), AdGuard + WireGuard, multi-user auth, backend migrated to a single Go binary |
| **4 — View-model + Settings revamp** | ✅ | API as a presentation view-model (`vm: 1`), single-sourced demo canon, semantic topology in the snapshot |
| **5 — Agent resilience** | ✅ | Router-side watchdog + heartbeat, server-side manual rearm, TTL auto-rearm, admin-only mutation routes |
| **6 — Agent security** | ✅ | HMAC-SHA256 on agent ingest, serve the agent binary from the server itself |
| **7 — Agent deep dive** | ✅ | Real-time wifi events (`iw event`), bidirectional SSE, `.ipk` packaging (11-12 MB RSS, <1% CPU) |
| **8 — Consolidation** | ✅ | `/api/health` metrics, persistent agent registry, retention ladder (raw 7d → 5min buckets 1y → daily ∞), recharts v3, outgoing alert webhooks |
| **9 — On-box** | ✅ | UCI config, AUTH_PASS bootstrap, TLS self-signed + SPKI pinning, pairing token, OpenWrt server package |
| **10 — Orchestration** | ✅ | Plan→apply→state engine + sandboxed executor (10.1), AdGuard (10.2), WiFi guest, DDNS, SQM, usteer modules. WireGuard moved to Phase 17 |
| **11 — LuCI package** | ✅ | `luci-app-netpulse` shipped as `.ipk`/`.apk` on every release: local agent status/view (procd, UCI, logs, restart/rearm) + bridge to the web app |
| **12 — Security audit** | ✅ | TRUST_PROXY, anti-replay on ingest, body cap, password min 10 |
| **13 — Robustness audit** | ✅ | Single-flight GetOverview, SSE write deadline, sshpool dial race, error wrapping |
| **14 — WiFi/roaming visibility** | ✅ | DAWN signal matrix, 802.11r status per SSID, channel utilization survey, persistent roaming events feed (30d). Polish pending: channel-analysis chart clarity ([#618](https://github.com/gnacho/netpulse/issues/618)) |
| **15 — Reports** | 🔄 | Daily/week/month availability. Pending: traffic, activity, alert summary, export, and per-client/VLAN network insights ([#588](https://github.com/gnacho/netpulse/issues/588)) |
| **16 — Advanced alerts** | 🔮 | Custom threshold rules, new alert types (roaming failure, channel congestion), scheduled silence, email |
| **17 — Write to routers** | 🔄 | UCI ownership + safe apply with rollback (#451), channel planning (#452), firmware upgrades (#453); full module index (AdGuard full, WiFi guest, DDNS, QoS, WireGuard, OpenVPN, Tailscale, Batman, DPI) |
| **18-20 — Beta-testing program** | 🔮 | Module groups by risk (low / medium / high) with stable + unstable release channels and external beta-testers |

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

### Installing the agents (recommended: let the app do it)

Once the router is in the table and the server's SSH key is authorized on
it, the app can install and start the agent by itself: open the **Routers**
page and scroll to the agents table at the bottom. Every OpenWrt router
shows a row there; routers without an agent offer an **Install agent**
button, and existing agents offer **Reinstall**. Over one SSH session the
server registers the agent if needed (its token is created on the fly),
detects the architecture, downloads the embedded agent binary from itself
(token-authenticated), verifies its SHA256, writes the config, installs
the procd init (with a self-heal step that re-downloads the binary after a
`sysupgrade`, which only preserves `/etc`), sets up the watchdog cron and
restarts the service. The agent shows up as connected within seconds.
Re-running it is safe: it rotates the token and reinstalls.

For routers the server cannot SSH into, use the pairing token instead:
Settings → Agent adoption shows a reusable pairing token, and
`install-agent.sh --pairing-token` (run from any machine that can reach
both the router and the server) registers the agent on first contact.

## OpenWrt packages

Every `v*` release ships the agent and the LuCI app as installable OpenWrt
packages alongside the tarballs:

- `netpulse-agent` as `.ipk` (OpenWrt 24.10 SDK, mediatek/filogic) and `.apk`
  (OpenWrt 25.12 SDK, qualcommax/ipq807x).
- `luci-app-netpulse` as `.ipk` and `.apk` (`all` arch, any target): LuCI
  pages that show the local agent status, let you restart it, edit its UCI
  config and jump to the NetPulse web app.

Grab the assets from the
[latest release](https://github.com/gnacho/netpulse/releases) and install on
the router (LuCI System > Software, or over SSH):

```sh
# OpenWrt 24.10 (ipk)
opkg install ./netpulse-agent_*.ipk ./luci-app-netpulse_*.ipk

# OpenWrt 25.12 (apk)
apk add --allow-untrusted ./netpulse-agent-*.apk ./luci-app-netpulse-*.apk
```

`luci-app-netpulse` depends on `netpulse-agent` and `luci-base`; installing
both packages together resolves the dependencies without extra feeds. After
install, LuCI picks up the new pages automatically (the package restarts
`rpcd` and clears the index cache).

The package ships an empty config, so the service stays inactive until you
point it at your server (the app cannot know these values). If the server
can already SSH into the router, skip all of this and press **Install
agent** in the Routers page agents table: it registers the agent, writes
the config and starts the service for you. Otherwise, create the agent
via the pairing token (Settings → Agent adoption plus `install-agent.sh
--pairing-token`) or `POST /api/agents`, and on the router:

```sh
uci set netpulse-agent.main.server='http://<netpulse-server-ip>:3000'
uci set netpulse-agent.main.slug='<slug>'
uci set netpulse-agent.main.token='<64-hex token>'
uci commit netpulse-agent
service netpulse-agent enable && service netpulse-agent start
```

If the server can already SSH into the router, prefer the automated path
above: **Reinstall** does all of this for you.

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

The legacy Node backend was removed (it is not deployed or updated anymore,
decision 5-Ago-2026); its git history is preserved. Migration from its
database happens automatically on the first Go boot.

## Tests

```bash
cd server-go && go test ./...
```

## Big thanks

NetPulse wouldn't exist without [OpenWrt](https://openwrt.org/). The whole
premise of the project, that you can own and control your network hardware
instead of depending on a vendor's closed firmware, only works because
OpenWrt exists. If NetPulse is useful to you, the real credit goes to the
OpenWrt community.

## License

[AGPL-3.0](LICENSE)
