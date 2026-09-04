# NetPulse user manual

NetPulse is a read-only PWA (progressive web app) for monitoring a home network built on OpenWrt/GL.iNet routers: fleet status, per-router health, connected clients, a live topology map, WiFi roaming status, alerts and, in experimental mode, a few write actions (change a WiFi channel, flash firmware, orchestrate services). It runs as a single self-contained Go binary with the frontend embedded, served from a small Linux box.

This manual walks through every area of the application: what each screen shows, where its data comes from (read-only SSH polling, agents, usteer, NetGrip, etc.), how to read each number or color, the design decisions worth understanding and step-by-step procedures for the common tasks. At the end there is a section clarifying what lives in NetPulse and what still lives in the router's own panel (LuCI or NetGrip).

## Areas at a glance

- [Overview](#overview)
- [Devices (the router fleet)](#devices-the-router-fleet)
- [Device detail](#device-detail)
- [Clients](#clients)
- [Topology](#topology)
- [Alerts](#alerts)
- [WiFi roaming](#wifi-roaming)
- [Channel plan](#channel-plan)
- [Firmware upgrades](#firmware-upgrades)
- [Reports](#reports)
- [Orchestration](#orchestration)
- [Help](#help)
- [Settings](#settings)
- [What lives in NetPulse vs the router panel](#what-lives-in-netpulse-vs-the-router-panel)

> **Note on naming.** The interface distinguishes two lists that look similar. The fleet page (route `/routers`) is called **Devices** and shows routers and access points. The client page (route `/devices`) is called **Clients** and shows the devices that connect to the network (phones, computers, TVs, cameras...). This manual uses the names exactly as they appear in the app.

---

## First access

NetPulse is multi-user with password authentication. The installer prints an initial admin password once; use it to log in for the first time.

1. Open your NetPulse server address in a browser.
2. Enter the username and password. After several failed attempts, access locks temporarily (a short countdown).
3. You land on the **Overview**.
4. Under **Settings > My profile** you can change your password (minimum 10 characters), edit the name used in the Overview greeting and choose a language (Spanish or English).

There is also a **demo mode** that opens without a password or backend: it shows a sample network with dozens of devices so you can explore the UI. In demo there is no real data and the write features are disabled.

---

## Overview

**Route:** `/`. This is the landing page after login.

### What it shows

- **Greeting and status**: a time-of-day greeting with your name, plus a line summarizing whether there are important alerts.
- **Network health ring**: a 0 to 100 score with a qualitative label (Excellent, Good, Warning, Critical).
- **Subscores**: small bars per dimension (for example coverage, temperature) that feed the overall score.
- **Penalty breakdown**: a list of the concrete factors that subtract points, and how much each subtracts.
- **Latency** to the gateway (ms) and **total clients**.
- **WAN traffic** live (download and upload).
- **Service cards**: AdGuard Home and WireGuard (only if you enabled them in Settings).
- **Recent alerts** and a **routers row**.
- In live mode (not demo): **collector** charts (TCP latency per router) and **WiFi SLEs** (Service Level Expectations).
- In demo: **Top devices** by traffic.

### What data it needs

The Overview aggregates everything the server has already collected: health is computed from router metrics (temperature, weak-client signal, WAN latency), WAN traffic comes from the gateway, and the services (AdGuard, WireGuard) are read from the gateway in read-only mode. In demo mode the values come from a simulated dataset.

### How to read it

- The **ring** is the overall score: the closer to 100, the better. The label (Excellent, Good, Warning, Critical) is the quick summary.
- The **subscores** use the same colors as the rest of the app: green (>= 90), amber (>= 70), red (below). They tell you which area is dragging the score down.
- The **breakdown** tells you exactly what is penalizing (for example "High temperature on the patio router") and, with a number, the weight of each penalty. This tells you what to fix first.
- **Latency** and **WAN traffic** are live metrics: they change with real network use.

### Design decisions

- Health is not a magic number: it is a score computed from thresholds that you can adjust under **Settings > Data & thresholds** (temperature 65 C by default, signal -70 dBm, latency 50 ms). Changing a threshold changes the score.
- The ring updates in real time over SSE; the server polls the routers every 5 seconds and pushes a snapshot to all connected clients.
- In demo there is no real data, and the collector/SLE cards do not appear (they depend on the backend).

### Procedure: read the network health

1. Open **Overview**.
2. Look at the ring: the overall score and its label.
3. If it is not Excellent, look at the **subscores** to locate the affected dimension.
4. Open the **penalty breakdown** and note the factor with the most weight.
5. Go to the relevant screen (for example **Devices** for temperature, or **Clients** for weak signal) to act.

---

## Devices (the router fleet)

**Route:** `/routers`. UI title: **Devices**.

### What it shows

- **Header** with the status summary ("N devices, N online, N warned"), a colored dot per router, a refresh button and, if there are outdated agents, a button to upgrade them all.
- **Router cards** (2 per row): model, firmware, status, CPU, RAM, temperature, uptime, traffic and latency.
- **Comparison table** of all routers.
- **Agents section** at the bottom: per router, whether it has an agent, its type (native NetPulse, NetGrip or external), its version and status, with buttons to install, reinstall or upgrade the agent.

### What data it needs

Routers are onboarded under **Settings > Devices** (see the onboarding procedure below). The server polls them over read-only SSH (ubus, `/proc`, iwinfo) every 5 seconds; routers with an agent also push data every 15 seconds. A router with no agent and no accessible SSH does not report CPU/RAM/temperature.

### How to read it

- The **colored dot** per router: green = online, amber = warned (for example high temperature), red = down.
- The **card** summarizes the key metrics; the warning icon appears when temperature exceeds the threshold.
- In the **agents section**, "Agent down" means the agent was registered but stopped reporting (usually fixed by reinstalling from the router detail).
- "No system metrics" means that device is monitored by SNMP or beacon and does not report CPU/RAM/temperature.

### Design decisions

- **Polling is read-only.** The server generates its own ed25519 keypair; you authorize its public key on each router and it only ever reads. It cannot change your network.
- **Installing the agent is optional but recommended.** Without an agent on the gateway, the client list stays empty until you install it, because client discovery (DHCP, bridge FDB, mDNS) is done by the agent of the router that sees those clients.
- **The gateway is auto-detected** on first boot via LAN discovery (TCP :22 sweep and ubus/GL-UI fingerprint); the rest are added manually.
- Some routers have an agent that lives inside the **NetGrip panel** (runs on the router's port 8080): in that case "Update" upgrades the panel itself, not a binary.

### Procedure: add a router

1. Go to **Settings > Devices** (admin only).
2. Click **Discover on the network** so the server scans the local network and finds devices, or use the manual add form.
3. If it is the gateway, mark it as **Main gateway**. Choose the type (router, managed switch or external).
4. Copy the server's **SSH public key** (shown in the same section) and authorize it on the router (`/etc/dropbear/authorized_keys`). Without this, the server cannot poll it.
5. Save. The router appears in the fleet and starts reporting as soon as polling reaches it.

### Procedure: install the agent on a router

1. With the server's SSH key already authorized on the router, open **Devices**.
2. Scroll down to the **agents section**.
3. In the router's row, click **Install agent** (or **Reinstall** if there was already one).
4. The server registers the agent (creates its token on the fly), detects the architecture, downloads the binary from itself, verifies its SHA256, writes the config, installs the init and restarts the service.
5. The agent shows as connected within seconds.

For routers the server cannot reach over SSH, use the **pairing token** (Settings > Agent adoption) with `install-agent.sh --pairing-token`.

### Procedure: upgrade all agents

1. In **Devices**, if there are outdated agents you will see a notice with the count.
2. Click **Update agents** and confirm.
3. Follow the progress panel: per agent you will see the status (waiting, downloading, installing, updated, or no response). An agent that does not respond within a few seconds requires manual reinstall.

---

## Device detail

**Route:** `/routers/:id`. UI title: **Device detail**.

### What it shows

Clicking a router in the fleet opens its detail, which changes with the role:

- **Header**: name, model, role (Main gateway or AP), status, uptime and, if applicable, a high-temperature warning with elapsed time.
- **Performance**: charts of CPU, RAM and temperature over time (if the source can provide them; otherwise a placeholder explains there are no vitals).
- **Info**: firmware, short model, and a link to the router's panel (NetGrip or LuCI) when applicable.
- For the **gateway**: WAN latency, AdGuard Home and WireGuard panels, and the physical **ports**.
- For the **APs**: **backhaul** panel (the return link toward the gateway) and **radios + ports**.
- **VLANs** of the bridge (read-only) if present.
- **Clients of this router**: the devices connected to it.

### What data it needs

The detail is fed by the read-only SSH polling and by the extras the backend returns (radios, LAN ports with their device, VLANs). The AdGuard and WireGuard panels read the gateway; the backhaul panel only appears on non-gateway routers.

### How to read it

- The **temperature banner** appears when the router is in "warned" status and the hot metric is temperature; it includes advice (ventilation).
- The **performance charts** are historical and let you see whether a CPU or temperature spike is punctual or sustained.
- The **ports** show what is connected to each LAN port (and link to the client's record in **Clients**).
- The **AdGuard/WireGuard** panels are the read-only view of what is configured in the router's panel (see the final section).

### Design decisions

- The template differs between the gateway and the APs because their responsibilities differ: the gateway concentrates WAN, AdGuard and WireGuard; the APs show backhaul and radios.
- The detail is **not cleared on refresh** to avoid flicker on each SSE snapshot; it is only replaced when something changed.

---

## Clients

**Route:** `/devices`. UI title: **Clients**.

### What it shows

- **Stats strip**: online now, new in the last 7 days, clients with weak signal (< -70 dBm) and clients protected by AdGuard.
- **Filter bar**: by router, by band (2.4 GHz / 5 GHz / cable), by device type (multi-select) and an "Only online" switch.
- **List or grid** of clients with classified type, name, manufacturer, IP/MAC, remaining DHCP lease time, the router they are attached to, band and signal.
- Expanding a client: MAC, DHCP lease, first seen, manufacturer, hostname, 24 h traffic, current traffic with a sparkline, AdGuard status and a rename action (change local icon/name).

### What data it needs

Clients are discovered from three sources running on the monitored routers: (1) the DHCP lease table, (2) the bridge FDB for wired clients and (3) mDNS/Bonjour when `umdns` is installed. Type classification uses hostname patterns and manufacturer OUI. Discovery requires the agent to run on the router that sees the clients; without an agent on the gateway, the list stays empty.

### How to read it

- The **signal** follows the usual scale: green above -55 dBm, cyan between -55 and -70, amber below -70 (considered weak).
- **"New"** marks devices first seen in the last 7 days; dimmed **"Offline"** marks known but disconnected devices.
- **DHCP leases** can be "Static IP (reservation)", "renews in Xh Ymin" or "Expired".
- The **infrastructure badges** (Hypervisor, CT, Managed switch) identify network equipment detected via LLDP or sealed by the server.

### Design decisions

- The default view orders online clients first (by traffic) and leaves the offline ones at the end.
- NetPulse is read-only by design, with opt-in exceptions for admins: a client's edit sheet lets you **reserve its IP** (static DHCP lease on the gateway) and **block/unblock its access** (firewall rule on the router it is attached to). Those are the only device writes; everything else about the router is configured in its own panel.
- It also lets you **mark a MAC as trusted** in Settings (so it stops alerting as "unknown") and **rename / change the icon** of a client.

### Procedure: rename or change a client's icon

1. In **Clients**, locate the device (search by name, IP or MAC with the search box).
2. Expand its record and click **Edit** (or the pencil icon in the row).
3. Choose another icon or name and save. In live mode the change is saved on the server; in demo, only in your browser.

### Procedure: mark a device as trusted

1. Go to **Settings > Trusted devices** (admin only).
2. Add the device's MAC. It stops alerting as "unknown" and its name is used as an alias.

### Procedure: reserve an IP (static DHCP lease)

Admin only, live mode. The reservation is written to the gateway's DHCP (the router serving leases), not to the router the client is attached to.

1. In **Clients**, expand the device record and open it in edit mode.
2. Under **Reserve IP**, tick **Use current IP** or type another IP (it must be free: if another MAC already holds it, the server rejects it with a conflict notice).
3. Click **Save IP**. The record now shows **Reserved as \<IP\>**.
4. To release it, clear the IP and save (or remove the reservation from the same field).

Notes: it is idempotent (reserving the same IP twice does not duplicate), and if the dnsmasq restart fails after writing, the server rolls the change back so the config is never left half-written.

### Procedure: block or unblock a device's access

Admin only. **Block** adds a firewall rule (DROP) for the device's MAC on the router it is attached to, so it loses network access; the block does not affect its DHCP lease.

1. In **Clients**, expand the device record and open it in edit mode.
2. Under **Block device**, click **Block**. The record shows the **Blocked** state.
3. To revert, click again (unblock). The state is queried live, so blocking the device from the router's own panel is reflected here too.

---

## Topology

**Route:** `/topology`. UI title: **Topology**.

### What it shows

An interactive SVG map (with pan and zoom) of the network, inferred live: the gateway in the center, the access points, wired and wireless clients, switches (managed or inferred), hypervisors with their containers, and the WireGuard tunnels drawn toward the Internet. Below, a **legend** and a **links table**.

### What data it needs

The map is built from the bridge FDB (for wired clients), LLDP when available (to identify neighboring routers and switches), the gateway's WireGuard data and WiFi signals. Wireless clients are associated to the AP that sees them.

### How to read it

- Each node has a shape and color according to its role: routers, clients, switches, hypervisors, containers and tunnels have distinct representations (the legend clarifies this).
- The **lines** are the links (cable, WiFi or VPN tunnel); hovering a link highlights the corresponding row in the links table.
- A client with **weak signal** is marked with the warning color.
- **WireGuard tunnels** are drawn from the peer toward the Internet, with its IP.

### Design decisions

- The topology is **inferred**, not configured: it is deduced from the router tables, which is why you sometimes see an "inferred" switch you never onboarded.
- LLDP only identifies neighboring routers and switches, not end clients.
- The **Refresh** button forces an immediate backend poll (POST /api/refresh) and the map updates with the snapshot arriving over SSE, without reloading the page.

### Procedure: use the topology map

1. Open **Topology**.
2. Use the zoom controls (**Zoom in / Zoom out / Reset view**) and drag to move the canvas.
3. Toggle **Labels** (names) and **Flow** (packet animation) as needed.
4. Hover a node or link to see details; click to open the record.
5. Use the **links table** to list all connections and locate a specific one.
6. (Admin only, live) Click **Tag devices** to mark a device as hypervisor/switch or assign it to a host.

---

## Alerts

**Route:** `/alerts`. UI title: **Alerts**.

### What it shows

- **Summary**: three counters (unread warnings, alerts today, critical) with a per-cause breakdown.
- **Alert configuration**: per category, the desired level (Urgent only / All / None) and a custom rules manager.
- **Filters**: by severity, by kind, by category and "Unread only".
- **Timeline feed** grouped by day, with severity by color and expandable context (a sparkline with a threshold line, or the WireGuard peer / affected device data).

### What data it needs

Alerts are generated by the server from what it already monitors: temperature, available firmware, new devices, WireGuard handshakes, WAN outages, weak signal, etc. There are six categories (Router, Internet, Clients, Signal, VPN, System) and four severities (warning, critical, info, resolved). In demo mode the feed is an enriched mockup; in live mode only real alerts are shown.

### How to read it

- The **left stripe color** and the icon indicate severity: amber = warning, red = critical, blue = info, green = resolved.
- The **colored dot** marks unread items. The read/unread state is stored by the server (source of truth).
- Expanding an alert shows its **context**: a sparkline with the threshold line (for metrics), or the peer/device data involved, and a direct link to the affected router.
- The **"urgent"** badge marks alerts requiring immediate attention.

### Design decisions

- Configuration is **per category and per level**, not per alert: you can silence the whole "Clients" category without touching the rest.
- You can **silence** a specific alert for 1 hour, 24 hours or forever.
- The **feed uses simulated infinite scroll**: reaching the bottom shows the end of history.

### Procedure: configure which alerts you want

1. Open **Alerts** and click **Configuration**.
2. For each category (Router, Internet, Clients, Signal, VPN, System) choose the level: **Urgent only**, **All** or **None**.
3. If you need custom rules (custom thresholds), use the **rules** manager at the bottom of the panel.
4. Changes save instantly (you will see the confirmation).

### Procedure: mark alerts as read

1. Click an alert to expand it; it is marked read automatically.
2. For all of them, use **Mark all as read**.

---

## WiFi roaming

**Route:** `/roaming`. UI title: **WiFi roaming**.

### What it shows

Five tabs:

- **Matrix**: each client's signal as seen by each access point (usteer's "hearing map"). One cell per client/AP with the dBm value and color.
- **802.11r**: Fast BSS Transition status per SSID and per router, with the 11r/11k/11v/PMF flags and detected anomalies.
- **Survey**: per-channel WiFi utilization (noise, busy percentage, RX/TX) per router and radio.
- **Events**: connections, disconnections and roaming decisions per client, with a 30-day history.
- **Re-anchor**: clients that would be better on another AP, with an action to move them.

### What data it needs

- **Matrix** and **Re-anchor** depend on **usteer** (or DAWN, which is deprecated and shows a notice). Without an active roaming daemon on the routers, these tabs have no data.
- **802.11r** reads `uci show wireless` from each router with WiFi (one SSH per router).
- **Survey** uses `iw survey dump` (one SSH per WiFi router).
- **Events** arrive via continuous agent ingestion into the SQLite database (30 days).

### How to read it

- In the **Matrix**, each cell is a client's signal as seen by an AP: green >= -65 dBm, amber between -65 and -80, red below -80. Clients that see several APs with similar signal are roaming candidates.
- In **802.11r**, the global state summarizes whether fast roaming is enabled on all, some or no SSIDs; anomalies (for example mismatched mobility domains) are marked in red.
- In **Survey**, a channel with busy >= 70 % appears in red (congested); >= 40 % in amber. Noise closer to 0 dBm is worse (-90 is optimal, -70 bad).
- In **Events**, the type is distinguished by icon (connection, disconnection, roaming decision).

### Design decisions

- The **802.11r**, **Survey** and **Events** tabs load lazily (when opened) because each costs one SSH per router.
- The **Re-anchor** tab "kicks" a client off its current AP to force reconnection; it is a write that acts on the roaming daemon (usteer), not a config change.

### Procedure: read the signal matrix

1. Open **WiFi roaming > Matrix**.
2. Filter by band if needed (2.4 GHz / 5 GHz).
3. Look for rows where a client sees several APs with similar signal (amber): they are roaming or repositioning candidates.
4. Enable **Weak signal only** to see just clients with poor signal on all APs.

### Procedure: reanchor a client

1. Open **WiFi roaming > Re-anchor**.
2. Review the table: client, current AP, recommended AP and the estimated gain (+dBm).
3. Click **Move** on the desired row. The daemon kicks the client off its current AP so it picks the recommended one.

---

## Channel plan

**Route:** `/wifi/channel-plan`. UI title: **Channel plan**. Marked "labs" in the menu.

### What it shows

Per router, one card per WiFi radio with the **current channel**, the **recommended channel** (computed from neighbor scans) and, when available, a score (current -> best). Below, a table of **neighbor APs** with SSID, BSSID, band, channel and signal.

### What data it needs

The plan is computed from each radio's **neighbor scans**. There is a key difference here: routers running the **NetGrip** panel (the gateway and the APs managed by its panel) **do not scan neighbors**; routers with the **NetPulse agent** do. That is why some routers show no recommendation.

### How to read it

- **Current channel** in large text, **recommended channel** highlighted when different.
- The **score** (when present) compares the current channel quality with the best: "X -> Y".
- In the **neighbor APs** table, signal is shown in dBm and the band (2.4 / 5 / 6 GHz) is deduced from the frequency.

### Design decisions

- Applying a channel **restarts that router's WiFi** for a few seconds and its clients reconnect. The change runs as a plan (see Orchestration) and is recorded, with the option to **revert to the previous channel** with one click.
- If a radio has no editable UCI section, instead of the apply button you will see a notice to **update the router's agent**.
- This page is a write feature: it uses the same plan engine as Orchestration.

### Procedure: change a radio's WiFi channel

1. Open **Channel plan** and choose the router in the selector.
2. In the radio card, compare current and recommended channels.
3. Click **Apply channel** and confirm (it warns about the brief restart of that network).
4. Follow the status (Applying... -> Channel applied). To undo, click **Back to [channel]** (shows the channel you had before).

---

## Firmware upgrades

**Route:** `/firmware-upgrades`. UI title: **Firmware upgrades**. Marked "labs" and admin only.

### What it shows

One card per router with the upgrade target fields (current version, target version, model/target, image URL, SHA256 checksum), the status of the last attempt and the actions to upgrade now or schedule.

### What data it needs

It is a managed flow: you must provide the OpenWrt image URL, the model/target and, recommended, the SHA256 checksum. The agent detects the router's system (model, board, version, target) and shows it as "Detected system" to help you fill the fields.

### How to read it

- The attempt **status** uses color labels: Requested, Downloading, Backing up, Verifying, Flashing, Done, Failed, Scheduled.
- A **failure notice** shows the error and its date; an old failure does not reflect the agent's current state (it can be dismissed).
- The **confirmation** reminds you that a config backup is saved before flashing, that the agent verifies the SHA256 (if you provided it) and that the router reboots and is out of service for several minutes.

### Design decisions

- **Verify before flashing**: if there is a checksum and it does not match, the upgrade stops and the router is left untouched. Without a checksum, it flashes without verifying (you are warned).
- **Automatic backup** of the config before flashing.
- You can **schedule** an unattended upgrade at a specific local time (and cancel it before it starts).
- The target that gets flashed is the **saved** one, not the one you are mid-editing (the modal summarizes exactly what will be flashed).

### Procedure: upgrade a router's firmware

1. Go to **Firmware upgrades** (admin only).
2. In the router's card, fill in target version, model/target and image URL. Use the "Detected system" the agent shows.
3. Paste the image's **SHA256 checksum** (recommended).
4. Click **Save**.
5. Click **Upgrade now** (or pick a date/time and **Schedule**) and confirm.
6. Follow the status until Done. For an unattended upgrade, use **Schedule** and you can **Cancel schedule** before it starts.

---

## Reports

**Route:** `/reports`. UI title: **Reports**.

### What it shows

The **availability** report per router: a table with one column per period and availability as a percentage, plus an average column, and a per-router detail with average latency, total traffic and minutes with data.

### What data it needs

It is fed by the time series the server accumulates. There are three granularities: **Daily** (7/14/30/60 days), **Weekly** (2/4/8/12 weeks) and **Monthly** (3/6/12/24 months). A nightly rollup fills the report each day; the current period is shown partial.

### How to read it

- The **availability bar** uses the semantic color: green >= 99 %, amber >= 95 %, red below.
- Availability = minutes with data over the total of the period.
- In the per-router detail: **average latency** (ms), **total traffic** (up + down) and **minutes with data** per period.

### Design decisions

- Only the **availability** report is implemented; the rest (traffic, activity, alert summary, export) is on the roadmap.
- You can **download the CSV** of the current view.

### Procedure: check availability

1. Open **Reports**.
2. Choose the granularity (**Daily / Weekly / Monthly**) and the number of periods.
3. Read availability per router and per period; open the detail for latency and traffic.
4. If you need the data outside the app, click **CSV**.

---

## Orchestration

**Route:** `/orchestration`. UI title: **Orchestration**. Marked "labs", admin only and **hidden by default** (enabled from Settings).

### What it shows

A module selector (AdGuard, Guest WiFi, DDNS, SQM, WireGuard, usteer) with its fields, and the **plan -> apply -> state** flow. You generate a plan of UCI changes, review the diff and apply it; the result is recorded.

### What data it needs

Each module writes to the selected router. By default only the **gateway** is listed; an "advanced" toggle allows a non-gateway router (with a warning). Changes are applied over SSE through the connected agent.

### How to read it

- The **plan** shows the UCI operations (kind + description) that will run.
- The plan **status**: pending, applying, applied, failed or rolled back.
- If the backend rejects the plan (for example the resource is managed by the firmware or is gateway-only), a specific notice appears.

### Design decisions

- This is the most general **write** feature: a plan -> apply -> state engine with an isolated executor.
- All modules are **gateway-only by default** (for safety); the advanced toggle is opt-in.
- The method (apk/opkg/binary/active...) is detected from the module and shown as a label.

### Procedure: configure a service via orchestration

1. Enable Orchestration under **Settings** (if you do not see it in the menu).
2. Open **Orchestration** and choose the module.
3. Fill the fields and click **Generate plan**.
4. Review the plan diff.
5. Click **Apply** and wait for the final status (applied or failed).

---

## Help

**Route:** `/help`. UI title: **Help**.

### What it shows

An in-app quick guide with guided walkthroughs for first-day tasks: first boot and password, onboarding a router or switch, installing the agent, reading the channel plan and upgrading firmware. Each walkthrough cites the real UI buttons (rendered as chips).

### Design decisions

The steps reference the **same UI labels** (same i18n), so the help evolves with the app: if a button changes name, the walkthrough becomes desynced and is detected. There are no screenshots on purpose (they age badly).

### Procedure: use the help

1. Open **Help**.
2. Expand the walkthrough you need.
3. Follow the numbered steps, which cite the buttons exactly as they are in the app.

---

## Settings

**Route:** `/settings`. UI title: **Settings**.

This is the largest screen. Main sections:

- **Data & thresholds**: units (Mbps / MB/s), refresh interval (3 s / 5 s / 10 s / paused), and the temperature (50-85 C), signal (-80 to -60 dBm) and latency (20-200 ms) thresholds that feed the health score. Includes the contracted WAN speed and a periodic speed test.
- **Appearance**: theme (light/dark/system), color palette, accent color, density (comfy/compact) and reduced animations.
- **Services**: which services are shown (AdGuard, WireGuard) and the Orchestration toggle.
- **Notifications**: alert badge, pulsing dots and sound.
- **Administration** (admin only, live): check for updates, backups, users and demo mode.
- **Devices** (admin only): add/edit/delete routers, SSH public key, firmware target and SNMP config.
- **Topology overrides** and **Trusted devices** (admin only).
- **Agent adoption**: pairing token and server fingerprint.
- **AdGuard Home**: configuration of the read-only connection to the router's AdGuard panel.
- **My profile**: name, language, password and sign out.
- **API Tokens**: bearer tokens for integrations.
- **About**: version, links, push/PWA, Telegram and system data.

### Design decisions

- The **language, theme and threshold** settings are saved per user/browser; the language chosen in live mode is persisted on the backend.
- In **demo mode** Settings is read-only (you cannot change the network configuration).
- Administration mutations (routers, users) require admin role and a live backend.

### Procedure: change language or theme

1. Open **Settings**.
2. For the language, use the selector in **My profile**.
3. For the theme, under **Appearance** choose light, dark or system (and optionally palette and accent).

### Procedure: adjust the health thresholds

1. Open **Settings > Data & thresholds**.
2. Move the temperature, signal and latency sliders.
3. Watch the **simulated score** (the small ring) to see the immediate effect on the overall score.

---

## What lives in NetPulse vs the router panel

NetPulse was born as a **read-only viewer**: its server generates its own ed25519 keypair, you authorize its public key on each router and it only ever reads (ubus, `/proc`, iwinfo, `bridge fdb`, `wg show`). That means there are areas that are **not NetPulse pages** and are configured in the router's own panel:

- **WireGuard**: NetPulse reads peers, handshakes and transfer (in the gateway detail and in Topology), but **creating or editing tunnels is done in the router's panel** (LuCI on OpenWrt, or the NetGrip/GL.iNet panel on port 8080).
- **AdGuard Home**: NetPulse shows query stats and blocked domains, but **AdGuard configuration** (lists, rules, upstream DNS) is done in the router's AdGuard Home panel.
- **Ports and VLANs**: NetPulse shows what is connected to each LAN port and the bridge VLANs (read-only), but **assigning ports/VLANs** is done in the router's panel.

That said, NetPulse has been adding **opt-in write features** (marked "labs" and, mostly, admin only), which are done from the app itself:

- **Channel plan**: change a radio's WiFi channel (with revert).
- **Firmware upgrades**: flash an OpenWrt image (with verification and backup).
- **IP reservations**: pin a client's IP as a static DHCP lease on the gateway (Clients section).
- **Device blocking**: cut a client's network access by MAC (Clients section).
- **Orchestration**: apply UCI changes for AdGuard, guest WiFi, DDNS, SQM, WireGuard and usteer.
- **Agent management**: install, reinstall and upgrade the NetPulse agent on the routers.

Practical rule: **to view and monitor, use NetPulse; to configure the network, use the router's panel**, except for the "labs" write features that the app itself offers you explicitly.

---

*This manual describes the application's behavior as implemented in the code. Some features depend on what runs on your routers (NetPulse agent, usteer, DAWN, NetGrip) and on the mode in which you run NetPulse (demo or live); when a screen requires one of these pieces, it is noted in its section.*
