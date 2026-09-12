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
</p>

<p align="center">
  <strong>The pulse of your home network. Real time. No cloud.</strong><br>
  Watch your routers, draw your topology, score your network's health and get
  alerted, from a single self-hosted binary. Nothing ever leaves your LAN.
</p>

<p align="center">
  <a href="https://demo.netpulse.cloudless.club"><strong>Try the live demo</strong></a> ·
  <a href="https://netpulse.cloudless.club/features"><strong>Full feature tour</strong></a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/hero-en-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/hero-en-light.png">
    <img alt="NetPulse overview with a network health score, live traffic chart, AdGuard Home stats, WireGuard peers and the alerts feed" src="assets/hero-en-light.png" width="800">
  </picture>
</p>

## See it before you install it

You don't need a single router to see NetPulse working:

- **[demo.netpulse.cloudless.club](https://demo.netpulse.cloudless.club)** is the real app
  with a full sample network loaded, read-only, no sign-up. Click around, change
  the theme, switch languages, watch the live updates.
- **[netpulse.cloudless.club/features](https://netpulse.cloudless.club/features)**
  is the complete feature inventory: every screen and feature, with the
  technical decisions and the honest scope of each one.

## Why NetPulse?

If you run OpenWrt, you already own your network. But owning it means
*knowing* it: what connects where, what's healthy, what changed overnight.
LuCI shows you one router at a time; NMS suites like Zabbix or LibreNMS are
built for datacenters. What was missing is the thing in between: a home NOC
that installs in minutes and just shows you your network.

NetPulse is that missing piece. It grew out of my own network: a GL.iNet
Flint 2 as the gateway and three second-hand Xiaomi AX6 access points, all on
OpenWrt, each with its own LuCI. I wanted one screen that told me the truth
about the whole thing, so I built it and use it every morning.

Three rules shape everything it does:

- **Monitoring never writes.** The server generates its own SSH key; you
  authorize the public key on each router and NetPulse only ever *reads*
  (ubus, `/proc`, iwinfo, `bridge fdb`, `wg show`). Anything that writes
  (reserving an IP, blocking a device, orchestrating services, flashing
  firmware) is an explicit, admin-triggered action, applied with a config
  snapshot and automatic rollback.
- **No cloud, no accounts, no telemetry.** One static Go binary with the web
  app embedded, running on a small box inside your LAN. SQLite for the time
  series, WAL mode, no external services.
- **Free as in forever.** AGPL-3.0, no premium tier waiting behind a paywall.
  If it's useful to you, a star on GitHub is the way to say thanks.

## What you get

**Fleet overview and a health score that means something.** One 0-100 score
condenses latency, loss, uptime and temperature per router and for the whole
network, updated live every 5 seconds over SSE. Live WAN traffic against your
contracted plan, alert feed, AdGuard Home stats and WireGuard peers on the
same screen (that's the screenshot above).

**A topology map that draws itself.** Inferred live from the bridge FDB (and
LLDP where available): wired and wireless clients under the router and port
they actually talk through, managed and inferred switches, Proxmox hosts with
their VMs nested, and WireGuard tunnels drawn from peer to Internet. Quiet
devices keep their place; nothing jumps around on refresh. Drag nodes to
arrange it and the layout stays yours.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-topology-en-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-topology-en-light.png">
    <img alt="Topology map with the gateway in the center, three access points, wired and wireless clients, an inferred switch and the WireGuard tunnel to Internet" src="assets/screenshot-topology-en-light.png" width="800">
  </picture>
</p>

**Every device, classified.** Hostnames, IPs, type detection (hostname
patterns + OUI), first seen, band and signal for Wi-Fi clients, and the router
each one is attached to. Label devices, reserve IPs, and see when something
new shows up.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-devices-en-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-devices-en-light.png">
    <img alt="Device list with type icons, hostname, IP, band, signal strength and the router each client is attached to" src="assets/screenshot-devices-en-light.png" width="800">
  </picture>
</p>

**Per-router health at a glance.** Model, firmware, CPU, memory, temperature,
uptime and live traffic for each router, with port-level history and per-band
client splits. SNMP-polled managed switches are first-class citizens too.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-router-en-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-router-en-light.png">
    <img alt="Routers view with per-router cards showing model, firmware, CPU, memory, temperature and uptime" src="assets/screenshot-router-en-light.png" width="800">
  </picture>
</p>

**And the rest of the tour:**

- **WireGuard**: peers with real names, latest handshakes, transfer per peer.
- **AdGuard Home**: query stats, blocked share, top blocked domains.
- **Wi-Fi and roaming**: signal matrix per AP, 802.11r status, channel
  utilization, persistent roaming events.
- **Alerts that find you**: temperature, new device, firmware available, WAN
  down, agent issues; bell feed plus native Web Push to your phone.
- **Multi-user**: bcrypt passwords, admin and viewer roles, per-user language
  (ES/EN).
- **Installable PWA**: phone or desktop, live over SSE, light/dark themes.
- **A good OpenWrt citizen**: native `netpulse-agent` packages (`.ipk`/`.apk`),
  a `luci-app-netpulse` page, UCI config, procd init, watchdog. Or run the
  whole server on-box.
- **Self-updating**: the built-in updater checks for releases and applies them
  with an atomic swap.

That is still a selection: **[every feature is on the website](https://netpulse.cloudless.club/features)**,
one by one, and everything above is clickable in the **[live demo](https://demo.netpulse.cloudless.club)**.

## How discovery works

NetPulse discovers client devices from three sources on the monitored
routers: DHCP leases, the bridge FDB (wired clients) and mDNS when `umdns` is
installed. LLDP identifies neighbouring routers and switches. Per-client
traffic prefers `nlbwmon` (per-MAC counters, wired) and falls back to hostapd
byte counters (Wi-Fi); if a wired client shows no traffic series, install
`nlbwmon` on that router. `vnstat` counts per-interface totals, not per
client, so it is not a substitute.

## Get started in about five minutes

You need a Linux box with systemd (x86_64, arm64 or armv7; a small LXC or VM
is plenty) and OpenWrt/GL.iNet routers on your LAN.

**1. Install the server** on the box:

```bash
curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install.sh | sh
```

The installer is plain, readable shell ([inspect it first](install.sh)): it
detects distro and arch, downloads the release verified against
`checksums.txt`, sets up a sandboxed `netpulse` systemd service and prints
the initial admin password once. Re-running the same line updates;
`sh install.sh --uninstall` removes.

**2. Open the app** at `http://<server-ip>:3000`, log in with the printed
password and the gateway is already there: NetPulse finds it on first boot
via LAN discovery (a TCP :22 sweep with ubus/GL-UI fingerprinting).

**3. Authorize the SSH key.** The server generated its own keypair on first
boot. Copy the public key from **Settings → Network** and, on each router you
want to monitor, append it:

```sh
echo "<public key from Settings>" >> /etc/dropbear/authorized_keys
```

**4. Install the agents from the app.** Open **Routers** and scroll to the
agents table: every OpenWrt router gets a row, and routers without an agent
offer an **Install agent** button. One click and the server registers the
agent, pushes the right binary for the router's architecture, writes the
config and starts the service; it shows up as connected within seconds.
Routers the server cannot SSH into use the pairing token instead
(**Settings → Agent adoption**, see the folded section below).

**5. Take it with you.** Install the PWA from your browser (Add to home
screen / Install app) and enable Web Push in Settings: alerts reach your
phone even with the tab closed.

Prefer to see it on your own hardware first? `DEMO_MODE=1 ./netpulse` runs
the same app with a 67-device sample network, no routers needed.

<details>
<summary><strong>Other install paths</strong></summary>

**OpenWrt packages.** Every release ships `netpulse-agent` and
`luci-app-netpulse` as installable packages (`.ipk` for OpenWrt 24.10,
`.apk` for 25.12, plus x86/64). Grab them from the
[latest release](https://github.com/gnacho/netpulse/releases):

```sh
# OpenWrt 24.10 (ipk)
opkg install ./netpulse-agent_*.ipk ./luci-app-netpulse_*.ipk

# OpenWrt 25.12 (apk)
apk add --allow-untrusted ./netpulse-agent-*.apk ./luci-app-netpulse-*.apk
```

The package ships an empty config, so point it at your server before
starting:

```sh
uci set netpulse-agent.main.server='http://<netpulse-server-ip>:3000'
uci set netpulse-agent.main.slug='<slug>'
uci set netpulse-agent.main.token='<64-hex token>'
uci commit netpulse-agent
service netpulse-agent enable && service netpulse-agent start
```

If the server can already SSH into the router, skip all of this and press
**Install agent** in the app: it does every step for you.

**Pairing token.** For routers the server cannot reach over SSH, Settings →
Agent adoption shows a reusable pairing token:
`install-agent.sh --pairing-token` (run from any machine that can reach both
the router and the server) registers the agent on first contact.

**Latency sidecar.** Optional long-term TCP latency probes per router:

```bash
curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install-collector.sh | sh
```

**On-box mode.** No spare box? The server itself runs on an OpenWrt router,
with UCI config and self-signed TLS with SPKI pinning.

</details>

## Updates and version history

Releases are signed, checksummed and pushed to your fleet by the built-in
auto-updater; the app shows a short human-readable "what changed" panel
before you update. The full version history lives in
[CHANGELOG.md](CHANGELOG.md) and on the
[releases page](https://github.com/gnacho/netpulse/releases); the plan ahead
is in [docs/ROADMAP.md](docs/ROADMAP.md).

## What to expect

NetPulse is a personal project, built for my own network and released as
free software (AGPL-3.0). It is and will always be free. I work on it in my
free time and it evolves following my own needs first; with contributions or
support it might grow faster, but I can't promise anything. **Honest scope
note**: so far it has been tested with my own hardware (a GL.iNet Flint 2
gateway and three Xiaomi AX6 access points running OpenWrt) plus WireGuard
and AdGuard Home. Other OpenWrt devices should work, but yours would be the
first to tell.

## Documentation

- **[User manual](docs/manual.en.md)** (also in
  [Spanish](docs/manual.es.md)): every screen explained, menu by menu, with
  step-by-step procedures.
- **[Website](https://netpulse.cloudless.club)**: the project in five
  minutes, with a [feature tour](https://netpulse.cloudless.club/features),
  FAQ and screenshots.
- **[Roadmap](docs/ROADMAP.md)**: what's done, what's next.
- **[Discussions](https://github.com/gnacho/netpulse/discussions)**: questions,
  ideas and shape-the-future talk.

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

# Tests
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
