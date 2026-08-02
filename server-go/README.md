# NetPulse — backend Go

Reescritura en Go del backend original de Node (`server/`), con **paridad total
de API** (mismos endpoints, envelopes y auth) y **migración automática** desde
la instalación anterior. Binario estático único (sin CGO) con el frontend
embebido — ideal para el CT de 512MB: sin `node_modules`, sin runtime.

## Stack

- Go ≥1.25, std `net/http` (ServeMux de patrones, sin framework)
- `modernc.org/sqlite` (SQLite puro, sin CGO) — mismo esquema y fichero de DB que la versión Node
- `golang.org/x/crypto` — bcrypt (auth) + ssh (adapters OpenWrt/WireGuard)

## Build

```bash
cd server-go
go build -trimpath -ldflags "-s -w" -o netpulse ./cmd/netpulse
```

El binario embebe `internal/staticspa/dist` (gitignored; **la CI copia `app/dist`
ahí antes de compilar** — ver `.github/workflows/go.yml`; en local hay que hacer lo
mismo una vez o el build de Go falla).
Para desarrollo, `STATIC_DIR=../app/dist` tiene prioridad sobre el embed.

## Run

Mismo `.env` que la versión Node (mismas variables, mismo formato — ver
`.env.example`):

```bash
cp .env.example .env   # edita AUTH_USER/AUTH_PASS, DEMO_MODE=1 para empezar
./netpulse             # http://localhost:3000
```

## Migración desde la versión Node

Automática en el primer arranque si `DATA_DIR` contiene la DB de la versión
Node:

1. `wal_checkpoint(TRUNCATE)` + **backup** a `netpulse.db.bak-<timestamp>`.
2. Se conservan: usuarios (hashes bcrypt portables), `kv` completo (incl.
   `session_secret` → **las sesiones vivas siguen válidas, no hay re-login**),
   sesiones, routers configurados, `device_attrib`, `metrics` y `adguard_stats`.
3. `login_attempts` se resetea (rate-limit limpio) y se marca `go_migration`
   en `kv` (idempotente).

Para migrar: para el servicio Node, configura el mismo `DATA_DIR` en el `.env`
del Go (o copia el directorio de datos) y arranca. El frontend no cambia.

## Actualizaciones

La CI publica `netpulse-server-<sha>-linux-<arch>.tar.gz` en la prerelease
`go-latest` (con tests ejecutados antes del build). `deploy/update.sh` detecta
el backend instalado y, para Go, descarga el binario y hace swap atómico —
`/api/update/*` y el UpdateBanner del frontend funcionan igual que en Node.

## Tests

```bash
go test ./...   # auth, db (+migración con fixture Node), httpapi, adapters, updater
```

## Estructura

```
cmd/netpulse/        entrypoint (config → db+migración → SSE → poller → HTTP → shutdown)
internal/config/     .env + validación fail-fast
internal/db/         schema, WAL, jobs, migrate_node.go
internal/auth/       cookie id.hmac, sesiones, rate-limit, bcrypt, middleware
internal/httpapi/    handlers (auth, users, data, config, update, health)
internal/sse/        hub (MAX_SSE_CLIENTS=10, heartbeat, sin gzip)
internal/poller/     tick 5s, persistencia, alertas
internal/adapters/   contrato Snapshotter + demo (dataset canónico) + live
                     (openwrt ubus/SSH, adguard, adguard-glinet, wireguard, dawn)
internal/routerstore/  CRUD de routers + bootstrap (ROUTERS_JSON/autodetección)
internal/sshkey/     generación/gestión de claves en DATA_DIR/.ssh
internal/discover/   descubrimiento de routers en la LAN
internal/updater/    check/status/apply contra GitHub releases
internal/security/   headers (CSP, HSTS, X-Frame-Options…)
internal/staticspa/  estáticos embebidos + SPA fallback
```
