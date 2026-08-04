# NetPulse — Arquitectura

> v2.0.0 (era Go). El backend Node (`server/`) está **archivado como fallback**
> (ver README, sección de rollback). El backend activo es `server-go/`.

## Vista de capas

```
┌─────────────────────────────────────────────────────────────────┐
│ Frontend (app/) React 19 + Vite PWA                             │
│   EventSource /api/stream · fetch /api/* (cookie sesión)        │
└──────────────▲──────────────────────────────────────────────────┘
               │ HTTP/JSON + SSE (mismo origen en producción)
┌──────────────┴──────────────────────────────────────────────────┐
│ server-go — binario único (frontend embebido con go:embed)      │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ cmd/netpulse — entrypoint, graceful shutdown (salvavidas 3s)│ │
│ │ internal/httpapi — net/http mux + middleware                │ │
│ │   security headers → requireAuth → handlers /api/*          │ │
│ │ internal/sse — hub /api/stream (máx 10→503, :hb 30s,        │ │
│ │   eventos snapshot/alert/bye/shutdown)                      │ │
│ │ internal/staticspa — dist embebido + override STATIC_DIR +  │ │
│ │   SPA fallback (404 plano "404 Not Found" fuera de SPA)     │ │
│ └───────────────┬─────────────────────────────┬───────────────┘ │
│ ┌───────────────▼──────────────┐  ┌───────────▼───────────────┐ │
│ │ internal/poller (5 s)        │  │ internal/db (SQLite, WAL) │ │
│ │  tick → Snapshotter →        │  │  sessions · kv            │ │
│ │  broadcast SSE + persistir   │  │  login_attempts           │ │
│ │  (persistencia solo live)    │  │  metrics · adguard_stats  │ │
│ └───────────────┬──────────────┘  └───────────────────────────┘ │
│ ┌───────────────▼─────────────────────────────────────────────┐ │
│ │ internal/adapters — contrato Snapshotter (única fuente)     │ │
│ │  demo     dataset canónico + random walk                    │ │
│ │  live     OpenWrtClient/router: ubus HTTP + sondas SSH      │ │
│ │           (sshpool persistente, backoff, single-flight)     │ │
│ │           wireguard · adguard (estándar y GL.iNet)          │ │
│ │           topology (FDB/OUI → puertos, switches, hipervis.) │ │
│ │ internal/auth · config · discover · updater · routerstore · │ │
│ │   sshkey · security                                         │ │
│ └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────────┐
│ collector/ — sidecar opcional (proceso independiente)           │
│   Sonda TCP :22 a los routers (5 s) → series largas en su propia│
│   SQLite (raw 7d → buckets 5min 366d → daily; NightlyJob 03:30) │
│   Lee netpulse.db en SOLO LECTURA para conocer los targets.     │
│   /healthz en 127.0.0.1:9100. Hoy NO hay integración con        │
│   server-go (pendiente: ver docs/AGENTE-OPENWRT.md).            │
└─────────────────────────────────────────────────────────────────┘
```

## Flujo SSE (`/api/stream`)

1. Cliente conecta con cookie válida (`requireAuth`) → hub registra el stream
   (máx `MAX_SSE_CLIENTS=10`, si no `503 too_many_clients`).
2. Evento `snapshot` **inmediato** (último overview del poller).
3. Cada tick (5 s): snapshot → `broadcast('snapshot', …)` + persistencia
   (solo live). Alertas nuevas → evento `alert`.
4. Heartbeat `:hb` cada 30 s; cabecera `X-Accel-Buffering: no`.
5. Sesión revocada/expirada con el stream abierto → evento `bye` y cierre.

## Modos de datos

| | Demo | Live |
|---|---|---|
| Activación | `DEMO_MODE=1` | `DEMO_MODE=0` + routers configurados |
| Fuente | dataset canónico + random walk | Sondeo real por router |
| Topología | fixtures (switch LLDP GS308E, fantasma, Proxmox+CTs) | FDB `brctl showmacs` + OUI (+ LLDP si `lldpd`) |
| Fallos | — | Router caído → `status:"offline"` + alerta; el resto sigue |

## Topología: qué sabemos y qué no (principio de honestidad)

- **FDB** (`brctl showmacs br-lan`): cada MAC cableada se aprende en un puerto
  físico. Una MAC → `Device.Port`. Varias MACs en un puerto → algo multiplexa.
- **Clasificación OUI** del puerto multi-MAC: OUI de hipervisor (Proxmox
  `BC:24:11`, VMware `00:50:56`/`00:0C:29`, Hyper-V `00:15:5D`, KVM `52:54:00`)
  con exactamente un host → CTs/VMs anidados bajo el host (nunca inventamos un
  switch); resto → nodo **"Switch o bridge (inferido)"** (sin IP, borde
  discontinuo, tooltip explícito sobre la incertidumbre). En APs un puerto
  multi-MAC es casi seguro el uplink → solo se infiere en el gateway.
- **LLDP** (`lldpd` en routers/APs, `lldpcli -f json show neighbors` en tier
  lento): asciende el nodo a **"managed"** (identificado: chasis, IP de
  gestión, capacidades, puerto remoto, badge cyan) y puebla `Device.Lldp`.

## View-model versionado (Fase 4)

La API es un **view-model de presentación versionado**: `GET /api/overview`
lleva siempre `vm` (`adapters.ViewModelVersion`, hoy `1`). Un cliente (p.ej.
Fase 9 LuCI) debe rechazar/avisar si `vm` supera la versión que soporta. Bump
de versión = cualquier cambio incompatible de forma; añadir campos opcionales
NO bumpea. El overview gana además `topology` (semántica precalculada:
enlaces, anillos por router y peers ocultos "+N") y los devices pueden llevar
`infra` (`hypervisor`|`ct`|`managed-switch`, sellado server-side — la app no
infiere). El dataset demo canónico es single-source en Go:
`app/src/data/demo-canon.json` se GENERA con `go run ./cmd/gen-demo-canon`
(hay test de frescura; la app lo importa en build — `mock.ts` re-exporta
desde el JSON, sin arrays canon duplicados).

Consumo en la app: `types.ts` fija `VM_SUPPORTED = 1` y DataProvider avisa
por consola (una vez, sin romper UI) si llega un `vm` mayor; la topología
semántica del snapshot se usa cuando está presente (anillos, enlaces y "+N"
del server; la geometría de píxeles sigue siendo local) con fallback exacto
al cálculo cliente si el servidor es viejo.

Endpoints de soporte (Fase 4): `GET /api/system/info` (datos reales del
proceso: versión Go, distro, kernel, CPU, RAM, uptime — alimenta el bloque
Sistema de Acerca de) y `PUT /api/users/me/display-name` (nombre de saludo
del Resumen, ≠ username; columna `users.display_name` migrada como
`language`; `GET /api/auth/me` y `/api/users` lo devuelven).

## Auth y seguridad

- Credenciales en `.env` (`AUTH_USER`/`AUTH_PASS`); bcrypt (coste 10) en
  `kv.auth_pass_hash` con fingerprint sha256 de la env para re-hash si cambia.
- Cookie `session` = `id.hmac` (HMAC-SHA256, comparación timing-safe),
  httpOnly, SameSite=Lax, 30 d; `Secure` según `COOKIE_SECURE`
  (`auto`/`always`/`never`). Rotación de sesión tras login.
- Rate-limit de login persistente: bloqueo 5 min al 5º fallo
  (`login_attempts`). **Nota de paridad:** el contador no se resetea tras
  expirar el bloqueo (literal al backend Node original); ante un atacante
  persistente el bloqueo se rearma al instante — es deliberado.
- **Credenciales de routers solo server-side:** el acceso es por clave SSH del
  servidor (`SSH_KEY_PATH`); la API solo expone la pública
  (`/api/config/sshkey`). La tabla `routers` no guarda secretos.
- **SSH host-key accept-new (TOFU):** el primer contacto con un router confía
  y anota su clave (`known_hosts` en DATA_DIR). Es paridad deliberada del
  backend original: un MITM en ese primer contacto quedaría confiado. Si el
  segmento es hostil, pre-carga `known_hosts` a mano.
- La password de AdGuard (estándar o GL.iNet) vive en `kv` y nunca aparece en
  respuestas, errores ni logs (el comando de login GL la lleva embebida y no
  se ecoa).
- `.env` y `server/data/` están en `.gitignore`; CI corre gitleaks sobre todo
  el historial.

## Tablas SQLite (`DATA_DIR/netpulse.db`, WAL + synchronous NORMAL)

| Tabla | Contenido |
|---|---|
| `sessions(id, created_at, expires_at, ua)` | Sesiones (cookie `session=id.hmac`, 30 d) |
| `kv(key, value)` | `session_secret`, `auth_pass_hash` (bcrypt) + fingerprint, `adguard_*`, `go_migration`, `alerts.config.v1`, `alerts.read.v1`, `agent.token.<slug>` (sha256), `push.vapid.*` |
| `login_attempts(ip, attempts, locked_until)` | Rate-limit persistente |
| `metrics(router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps)` | 1 fila/router/tick; retención 7 días |
| `adguard_stats(ts, queries, blocked)` | 1 fila/tick; retención 7 días |
| `push_subscriptions(endpoint, keys_auth, keys_p256dh, user_agent, created_at)` | Suscripciones Web Push; 404/410 al enviar → borrado |

## Alertas (motor `internal/alerts`)

Eventos con `Category` (router|internet|clients|signal|vpn|system), `Urgent`
y `Ts`. El motor aplica **config por categoría de 3 niveles** (`urgent` / `all`
/ `none`, en `kv alerts.config.v1`) EN CREACIÓN: `none` descarta, `urgent` solo
deja pasar los urgentes, `all` pasa todo. Dedup 5 min por
(categoría,título,router), cap 100. Read-state en servidor (`kv
alerts.read.v1`, FIFO 200) — la campana usa `overview.unreadAlerts` (server
truth). Fuentes live: router offline (2 fallos seguidos) y recuperado, WAN
down (lossPct=100 en 2 sondeos), temperatura > 65 °C, señal < −70 dBm
(1/día/device), handshake WireGuard, dispositivo desconocido (sin nombre DHCP,
primer ciclo mudo). La demo siembra sus 5 canon vía `Engine.Seed` (omite solo
el filtro de config). Hook `Notifier` para canales externos.

## Web Push

VAPID en `kv` (generado en primer arranque), suscripciones en tabla propia,
`push.Notifier` envía SOLO las alertas que pasan config y son urgentes
(payload `{title, body, category, severity, url, tag}`; tag = dedup del
navegador). SW propio (`injectManifest`) con handlers `push` y
`notificationclick` → abre `/alerts`. Requiere contexto seguro (HTTPS o
localhost) — en LAN HTTP la tarjeta de Ajustes lo avisa.

## Agente OpenWrt (Fase 3, piloto)

`POST /api/ingest/agent` (Bearer por equipo, token sha256 en `kv`, rate-limit
30/min, body ≤ 2 MB) + CRUD `/api/agents`. El adapter live-agent usa el último
payload como estado del router; si `last_seen` expira (TTL 90 s) degrada a SSH
con alerta `system` y avisa al recuperar. El binario `agent/` (stdlib-only,
CGO=0, ~5,8 MB arm64 — suelo realista con TLS/x509) ejecuta localmente las
sondas del tier rápido (package `probe` compartido con server-go vía
`replace ../agent`), stateless, push con backoff y buffer RAM acotado; procd +
`install-agent.sh`. Diseño completo y hoja de ruta en `docs/AGENTE-OPENWRT.md`.

Jobs de mantenimiento (cada hora): borrado de filas > 7 días, sesiones
expiradas y `PRAGMA wal_checkpoint(TRUNCATE)`. Migración Node→Go automática
con backup atómico (marca `kv.go_migration`).

## Estáticos y SPA fallback

`internal/staticspa` sirve el `dist` embebido (override con `STATIC_DIR`).
Fallback a `index.html` para rutas no-API; `404 Not Found` plano (text/plain)
para `/api/*` desconocidas, `/assets/*` inexistentes y métodos no-GET.
El `dist` embebido NO se commitea (gitignored): un checkout fresco necesita
`npm run build --prefix app && cp -r app/dist server-go/internal/staticspa/dist`
(o `STATIC_DIR`) antes de compilar Go; la CI lo hace siempre antes del build.

## Variables de entorno

Ver `server-go/.env.example`. Resumen: `PORT`, `STATIC_DIR`, `DATA_DIR`,
`SESSION_SECRET` (opcional), `AUTH_USER`, `AUTH_PASS`, `DEMO_MODE`,
`MAX_SSE_CLIENTS`, `SSH_KEY_PATH`, `ADGUARD_*`, `WG_INTERFACE`,
`COOKIE_SECURE`, `UPDATE_*`.

## Hoja de ruta

Ver `docs/ROADMAP.md` (fuente de verdad, actualizada 2026-08-04). Resumen:

- **Fase 7 — TLS y endurecimiento (bloqueante):** HTTPS en CT 226
  (desbloquea Web Push), HMAC en la ingesta, binario del agente servido
  localmente, reinstalación de agentes con versión inyectada.
- **Fase 8 — agente a fondo:** netlink/nl80211 nativo, eventos ubus
  (assoc/disassoc en tiempo real), `.ipk`, medición en hardware real.
  Piloto (ingesta + push + fallback SSH) YA implementado y desplegado en los
  4 routers — ver sección "Agente OpenWrt".
- **Fase 9:** app embebida en routers (server on-box en el gateway,
  `luci-app-netpulse` opcional).
- **Fase 10:** escritura/orquestación (AdGuard → WireGuard → DAWN → Batman)
  con plan/apply/rollback.
- Integración de las series del collector en server-go (hoy son independientes).
