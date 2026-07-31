# NetPulse — Arquitectura

## Vista de capas

```
┌─────────────────────────────────────────────────────────────────┐
│ Frontend (app/) React 19 + Vite PWA                             │
│   EventSource /api/stream · fetch /api/* (cookie sesión)        │
└──────────────▲──────────────────────────────────────────────────┘
               │ HTTP/JSON + SSE (mismo origen en producción)
┌──────────────┴──────────────────────────────────────────────────┐
│ server/src/index.js  (entry, graceful shutdown)                 │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ app.js — Hono                                               │ │
│ │  security.js headers → auth.js requireAuth → routes/        │ │
│ │  routes/auth.js   login/logout/me                           │ │
│ │  routes/data.js   overview/routers/devices/alerts/topology  │ │
│ │  sse.js           /api/stream (hub, heartbeat, bye)         │ │
│ │  health.js        /api/health (público) + /health           │ │
│ └───────────────┬─────────────────────────────┬───────────────┘ │
│                 │                             │                 │
│ ┌───────────────▼──────────────┐  ┌───────────▼───────────────┐ │
│ │ poller.js (5 s)              │  │ db.js (better-sqlite3)    │ │
│ │  tick → adapter.getOverview  │  │  sessions · kv            │ │
│ │  → broadcast SSE             │  │  login_attempts           │ │
│ │  → persist metrics/adguard   │  │  metrics · adguard_stats  │ │
│ └───────────────┬──────────────┘  └───────────────────────────┘ │
│ ┌───────────────▼─────────────────────────────────────────────┐ │
│ │ adapters/ (fuente de datos, única para routes y poller)     │ │
│ │  demo.js  dataset canónico (demo/dataset.js) + random walk  │ │
│ │  openwrt.js  ubus JSON-RPC/HTTP → fallback SSH              │ │
│ │  adguard.js  HTTP API /control/* (basic auth)               │ │
│ │  wireguard.js  wg show <iface> dump vía SSH (gateway)       │ │
│ └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## Flujo SSE (`/api/stream`)

1. Cliente conecta (con cookie válida; `requireAuth` antes) → hub registra el
   stream (máx `MAX_SSE_CLIENTS=10`, si no `503 too_many_clients`).
2. Evento `snapshot` **inmediato** (último overview del poller).
3. Cada tick del poller (5 s): `adapter.tick()` → `getOverview()` →
   `broadcast('snapshot', overview)` + persistencia en SQLite. Alertas nuevas →
   evento `alert` individual.
4. Heartbeat `:hb` cada 30 s. Sin `Content-Encoding: gzip` manual; cabecera
   `X-Accel-Buffering: no` para que nginx no bufee.
5. Si la sesión se revoca/expira con el stream abierto → evento `bye` y cierre.

## Modos de datos

| | Demo | Live |
|---|---|---|
| Activación | `DEMO_MODE=1` o sin `ROUTERS_JSON` | `DEMO_MODE=0` + `ROUTERS_JSON` |
| Fuente | `demo/dataset.js` (port del mock canónico) + random walk suave | Sondeo real cada tick |
| Routers | 4 del canon | `OpenWrtClient` por router (ubus HTTP→SSH) |
| AdGuard | Canon | `GET /control/status` + `/control/stats` |
| WireGuard | Canon (bytes crecientes) | `wg show wg0 dump` vía SSH en el gateway |
| Fallos | — | Router caído → `status:"offline"` + alerta; el resto sigue |

## Tablas SQLite (`DATA_DIR/netpulse.db`, WAL + synchronous NORMAL)

| Tabla | Contenido |
|---|---|
| `sessions(id, created_at, expires_at, ua)` | Sesiones (cookie `session=id.hmac`, 30 d) |
| `kv(key, value)` | `session_secret` autogenerado, `auth_pass_hash` (bcryptjs) + fingerprint |
| `login_attempts(ip, attempts, locked_until)` | Rate-limit persistente (5 min tras 5 fallos) |
| `metrics(router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps)` | 1 fila/router/tick; retención 7 días |
| `adguard_stats(ts, queries, blocked)` | 1 fila/tick; retención 7 días |

Jobs de mantenimiento (cada hora): borrado de filas > 7 días, sesiones
expiradas y `PRAGMA wal_checkpoint(TRUNCATE)`.

## Auth (single-user)

- Credenciales en `.env` (`AUTH_USER`/`AUTH_PASS`). La password nunca se
  persiste en claro: bcryptjs → `kv.auth_pass_hash` (con fingerprint sha256 de
  la env para re-hashear si cambia).
- Cookie `session` = `id.hmac` (HMAC-SHA256), httpOnly, SameSite=Lax, 30 d.
  `Secure` cuando la petición llega por HTTPS (`COOKIE_SECURE=auto`; forzable
  con `always`/`never`).
- Rotación de sesión tras login (la anterior se destruye).

## Variables de entorno

Ver `server/.env.example`. Resumen: `PORT`, `NODE_ENV`, `STATIC_DIR`,
`DATA_DIR`, `SESSION_SECRET` (opcional), `AUTH_USER`, `AUTH_PASS`,
`DEMO_MODE`, `MAX_SSE_CLIENTS`, `ROUTERS_JSON`, `SSH_KEY_PATH`,
`ADGUARD_URL/USER/PASS`, `WG_INTERFACE`, `COOKIE_SECURE`.

## Estáticos y SPA fallback

Hono sirve `STATIC_DIR` (`../app/dist` por defecto). Fallback a `index.html`
para cualquier ruta que no sea `/api/*` (404 JSON) ni `/assets/*` (404).
