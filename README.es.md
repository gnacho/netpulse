# NetPulse

Dashboard PWA de monitorización de red doméstica (**solo lectura**) para una red
de 4 routers: 1 GL.iNet Flint 2 (GL-MT6000, con AdGuard Home y WireGuard) + 3
puntos de acceso OpenWrt.

- **Frontend** (`app/`): React 19 + Vite + Tailwind + shadcn/ui. PWA instalable.
- **Backend** (`server/`): Node + Hono + SQLite (better-sqlite3). Auth
  multiusuario con cookie de sesión, SSE en tiempo real (5 s), adapters demo/live.
- **Backend Go** (`server-go/`): reescritura completa del backend en Go
  (`net/http` estándar, `modernc.org/sqlite`, binario estático único con el
  frontend embebido). Reemplazo directo: misma API/variables/`.env`, migración
  automática desde la BD de Node en el primer arranque. Unit systemd:
  `deploy/netpulse-go.service`.
- **Collector** (`collector/`): sidecar en Go que sondea la latencia TCP a cada
  router (lee la lista de routers de `netpulse.db` en solo lectura) y guarda su
  propia SQLite de series temporales (`metrics.db`, raw → buckets 5 min → diario).
  **TODO**: que server-go lea estas series de largo plazo del collector en vez
  de depender solo de su tabla `metrics` de 5 s.

## Estructura

```
app/          # Frontend React (PWA). Contrato de datos: app/src/data/mock.ts
server/       # Backend Hono + SQLite (ver ARCHITECTURE.md)
  src/
    index.js  # Entry: valida .env, arranca Hono + poller + jobs
    demo/     # Dataset canónico (port ESM del mock del frontend)
    adapters/ # demo.js (dataset + random walk) · openwrt.js · adguard.js · wireguard.js
server-go/    # Backend Go (reemplazo directo del Node; ver server-go/README.md)
collector/    # Sidecar Go: latencia TCP a routers + series temporales propias
deploy/       # Units systemd de referencia (netpulse, netpulse-go, netpulse-collector)
```

## Requisitos

- **Node 24 LTS** recomendado en el servidor (o la LTS vigente; `engines` >= 22).
  `better-sqlite3` trae prebuilds para las LTS actuales.
- En modo live: acceso root por SSH (clave) a los routers y AdGuard Home
  accesible por HTTP desde el servidor.

## Desarrollo

```bash
# Backend (puerto 3000, modo demo por defecto)
cd server
npm install
cp .env.example .env    # edita AUTH_PASS
npm run dev             # node --watch

# Frontend (puerto 5173, proxifica /api → localhost:3000)
cd app
npm install
npm run dev
```

## Producción

```bash
cd app && npm run build          # genera app/dist/

cd server
cp .env.example .env             # AUTH_USER/AUTH_PASS, DEMO_MODE, ROUTERS_JSON…
NODE_ENV=production node src/index.js   # sirve app/dist + /api/* en :3000
```

systemd: ver `deploy/netpulse.service` (usuario dedicado `netpulse`, hardening
básico; la skill de infraestructura lo refina: HTTPS, CAP_NET_BIND_SERVICE…).

## Modos de datos

- **Demo** (`DEMO_MODE=1` o sin `ROUTERS_JSON`): sirve el dataset canónico del
  mockup con random walk suave cada 5 s. Ideal para desarrollo y demos.
- **Live** (`DEMO_MODE=0` + `ROUTERS_JSON`): sondea los routers por
  ubus/SSH, AdGuard por HTTP API y WireGuard con `wg show`. Un router caído
  queda `offline` + alerta; el resto sigue funcionando.

Variables de entorno: `server/.env.example` (todas documentadas ahí y en
`ARCHITECTURE.md`). Contrato API: `design/api-contract.md`.

## Tests

```bash
cd server && npm test   # node --test: auth+rate-limit, snapshot demo, paginación
```
