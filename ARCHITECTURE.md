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
| `kv(key, value)` | `session_secret`, `auth_pass_hash` (bcrypt) + fingerprint, `adguard_*`, `go_migration` |
| `login_attempts(ip, attempts, locked_until)` | Rate-limit persistente |
| `metrics(router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps)` | 1 fila/router/tick; retención 7 días |
| `adguard_stats(ts, queries, blocked)` | 1 fila/tick; retención 7 días |

Jobs de mantenimiento (cada hora): borrado de filas > 7 días, sesiones
expiradas y `PRAGMA wal_checkpoint(TRUNCATE)`. Migración Node→Go automática
con backup atómico (marca `kv.go_migration`).

## Estáticos y SPA fallback

`internal/staticspa` sirve el `dist` embebido (override con `STATIC_DIR`).
Fallback a `index.html` para rutas no-API; `404 Not Found` plano (text/plain)
para `/api/*` desconocidas, `/assets/*` inexistentes y métodos no-GET.
El repo commitea un `dist/index.html` placeholder para que un checkout fresco
compile; el dist real lo genera CI antes del build de Go.

## Variables de entorno

Ver `server-go/.env.example`. Resumen: `PORT`, `STATIC_DIR`, `DATA_DIR`,
`SESSION_SECRET` (opcional), `AUTH_USER`, `AUTH_PASS`, `DEMO_MODE`,
`MAX_SSE_CLIENTS`, `SSH_KEY_PATH`, `ADGUARD_*`, `WG_INTERFACE`,
`COOKIE_SECURE`, `UPDATE_*`.

## Hoja de ruta

- **Fase 6 — agente OpenWrt (nativo en el router):** diseño en
  `docs/AGENTE-OPENWRT.md` (agente stateless con push HTTPS+token, procd,
  tmpfs; el colector-satélite actual se conserva para series largas).
- Integración de las series del collector en server-go (hoy son independientes).
