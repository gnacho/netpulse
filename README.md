# NetPulse

> 🇪🇸 [Versión en español](README.es.md)

Read-only PWA dashboard for monitoring a home network of OpenWrt/GL.iNet
routers: fleet status, per-router health, connected devices, WireGuard peers,
AdGuard Home stats and alerts — in real time.

- **Frontend** (`app/`): React 19 + Vite + Tailwind + shadcn/ui. Installable PWA.
- **Backend** (`server/`): Node + Hono + SQLite (better-sqlite3). Single-user
  auth (session cookie + bcrypt), SSE live updates (5 s), demo/live adapters.
- **Discovery**: scans the LAN for OpenWrt/GL.iNet routers (TCP :22 sweep →
  ubus/GL-UI fingerprint → SSH key check). The gateway is auto-detected on
  first boot; the rest are added from Settings.
- **SSH key flow**: the server generates its own ed25519 keypair and shows the
  public key in Settings — you authorize it on each router you want to monitor
  (`/etc/dropbear/authorized_keys`). Read-only polling: ubus, `/proc`, iwinfo,
  `bridge fdb`, `wg show`.

## Quick start

```bash
cd server
npm install
cp .env.example .env      # set AUTH_PASS
npm run dev               # backend on :3000

cd ../app
npm install
npm run dev               # frontend on :5173 (proxies /api → :3000)
```

Production: `cd app && npm run build`, then run the backend with
`NODE_ENV=production` — it serves `app/dist` + `/api/*` on `:3000`.
systemd unit: `deploy/netpulse.service`.

## Tests

```bash
cd server && npm test
```

## License

[AGPL-3.0](LICENSE)
