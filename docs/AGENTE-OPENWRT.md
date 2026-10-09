# Fase 6 — Agente nativo OpenWrt (`netpulse-agent`)

> Estado: **piloto implementado** (2026-08-02) + **seguridad HMAC + binario desde server** (v2.5.0, 2026-08-05):
> ingesta `POST /api/ingest/agent` + tokens por equipo + adapter live-agent con fallback SSH + binario
> `agent/` (probe compartido, push con backoff, procd, install-agent.sh) +
> **HMAC-SHA256 obligatorio** (cabecera `X-Agent-Signature`) + **binario servido desde
> `GET /api/agents/{slug}/binary?arch=...`** (embebido vía go:embed, sin GitHub).
> Medido: **5,8 MB arm64** (suelo realista con TLS/x509; el objetivo inicial
> ≤ 3 MB no es alcanzable sin renunciar a HTTPS — cabe sobrado en MT6000/MT7981;
> la variante `/tmp` cubre flashes justas). Pendiente incremento 2: netlink/
> nl80211 nativo, eventos ubus, `.ipk`, medición en hardware real.

## Por qué un agente

El sondeo SSH actual es correcto pero caro en un router (handshake SSH + spawn
de N comandos cada pocos segundos) y no permite tiempo real. Un agente nativo
en Go puede:

- Leer netlink directamente (rtnetlink → FDB/ARP; nl80211 → estaciones wifi
  con señal/tasas por cliente) sin parsear CLI ni spawn.
- Suscribirse a eventos `ubus` (hostapd assoc/disassoc) → el dashboard ve
  clientes entrar/salir **al instante**, no en el siguiente poll.
- Empujar (push) al servidor: en runtime ya no hace falta SSH ni credenciales
  de router en el servidor para ese equipo.

## Decisiones de diseño

### 1. Binario separado, stateless, sin SQLite

El collector-satélite actual (`collector/`) mantiene series largas con SQLite
en una máquina siempre encendida — eso sigue igual. El **agente-router es otro
binario** (`netpulse-agent`) que NO usa `modernc.org/sqlite`:

| | collector-satélite | netpulse-agent (router) |
|---|---|---|
| Binario (est.) | ~13–17 MB | objetivo **≤ 3 MB** (stdlib + x/crypto opcional) |
| Estado | SQLite WAL (raw/buckets/daily) | **stateless**: buffer en RAM con cap + drop-oldest |
| Disco | persistente | **nada en flash** (anti-desgaste NAND); logs a syslog/stderr |
| Canal | local + /healthz | **push HTTPS al servidor** con token + backoff |
| Init | systemd | **procd** (`/etc/init.d/netpulse-agent`, respawn) |

Razón: en el router la escritura cada 1–5 s degrada la NAND 24/7 y 64 MB de
cache SQLite no son aceptables. El agente recolecta, acumula en RAM (p.ej.
ventana de 10–30 s) y empuja; si el servidor no responde, el buffer se acota y
descarta lo más viejo (mismo patrón que el fix del buffer del satélite).

### 2. Canal push y seguridad

- Endpoint nuevo en server-go: `POST /api/ingest/agent` (a añadir en Fase 6).
  - Auth: token por equipo (header `Authorization: Bearer <token>`), generado
    en la UI al "adoptar" un agente, guardado hasheado en `kv`.
  - TLS: el despliegue habitual ya termina TLS delante de server-go; en LAN
    plana se acepta HTTP con token (documentar el riesgo).
- Payload: NDJSON o JSON único con `{"router": "<slug>", "ts": …, "devices": …, "radios": …, "ports": …, "lldp": …}` — **mismos shapes que `internal/adapters` produce hoy**, para que el servidor solo tenga un nuevo adapter `live-agent` que reutiliza el pipeline (snapshot → SSE → persistencia).
- El servidor sigue funcionando en Tier 0/1 para equipos sin agente: un router
  puede degradar de agente a SSH si el agente deja de empujar (marca
  `agent_last_seen`; si expira → fallback SSH + aviso en UI).

### 3. Instalación y ciclo de vida en OpenWrt

- Build: `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 -trimpath -ldflags="-s -w"`
  (MT7986/MT7981 = Cortex-A53; un solo binario arm64 sirve a los 4 equipos;
  añadir arm/v7 si hiciera falta). Medir tamaño en CI.
- Instalador one-liner (hermano de `install-collector.sh`):
  `install-agent.sh` vía SSH: copia binario a `/usr/sbin/netpulse-agent` (si la
  flash es justa: a `/tmp` + script de arranque que lo re-descarga del
  servidor), token en `/etc/netpulse-agent.env` (chmod 600), init procd con
  `respawn`, `enabled` por defecto.
- La URL base con la que el agente alcanza al server (la del propio comando)
  se resuelve así (#1335): `NETPULSE_AGENT_URL` (p. ej. una URL de la LAN
  como `http://192.168.1.50:8080` cuando el server se sirve tras un reverse
  proxy con dominio público) > `NETPULSE_PUBLIC_URL` > el Host de la
  petición con la que se generó el comando. Sin variables, el comando usa el
  dominio desde el que se copió: los routers darían la vuelta por internet.
- Opcional a futuro: paquete `.ipk` con postinst.
- Upgrade: el updater existente del servidor se extiende a agentes (el agente
  reporta versión en cada push; el servidor ofrece binario firmado sha256).

### 4. Trabajo previo en el repo (ya hecho o en curso)

- [x] Tier 1 LLDP: colector live `lldpcli -f json show neighbors` + promoción
      de nodos "managed" (Fase 5).
- [x] Fixes de robustez del collector-satélite (buffer acotado, Close
      ordenado, healthz sin bloqueo… — Fase 5).
- [ ] Factorizar recolectores: lo que hoy son sondas SSH literales en
      `internal/adapters/openwrt.go` tiene que existir como lógica local
      (netlink/ubus) en el agente. Extraer interfaces `probe`/`sink` cuando se
      aborde Fase 6 — no antes, para no reestructurar en frío.
- [ ] Endpoint de ingesta + adapter `live-agent` en server-go.
- [ ] UI: "adoptar agente" (genera token + muestra el one-liner), badge
      "agente" en la tarjeta del router, aviso de fallback a SSH.

## Recuperación automática desde el server (supervisor, #890)

Cuando un agente registrado deja de empujar (su último push supera el
TTL del registry), el server intenta recuperarlo solo, en este orden de
escalado:

1. **Rearme**: `/etc/init.d/netpulse-agent restart` vía SSH (timeout
   10 s). Se espera un push nuevo hasta 30 s.
2. **Rotación de token**: si el reinicio no devuelve pushes, el server
   rota el token y reescribe el `.env` del agente en caliente (vía SSH,
   timeout 10 s) y espera otro push nuevo hasta 30 s. Arregla el caso de
   un 401 por token desincronizado.
3. **Reinstalación** (opt-in, `NETPULSE_AUTO_REINSTALL=1` +
   `NETPULSE_PUBLIC_URL` o `NETPULSE_AGENT_URL`): script canónico completo
   (binario verificado + config + init self-heal), timeout SSH 300 s.

Anti-martilleo: el supervisor rearma como mucho 1 vez cada 10 min por
slug (reinstala 1 vez cada cooldown propio). NetGrip embebido solo
recibe el paso 1 (restart del servicio netgrip); los switches gestionados
externos no se rearman nunca (su caída la reporta el Dead Man's Switch).

Todo el proceso se registra en el journal del servicio con prefijo
`supervisor:` / `rearmer:` (expiración del TTL, intento SSH, espera de
push, escalado y resultado), para poder seguir la recuperación sin mirar
código. Los agentes NetGrip embebidos y los rearmes manuales desde la UI
siguen el mismo camino y también quedan registrados.

## Qué NO es el agente

- No es obligatorio: NetPulse sigue funcionando 100% en Tier 0 (SSH) +
  Tier 1 (lldpd). El agente es un extra por equipo.
- No sustituye al collector-satélite de series largas (roles distintos:
  el agente empuja estado; el satélite acumula histórico).
- No corre en equipos con flash/RAM muy justos sin la variante `/tmp`
  (documentar requisitos medidos, no estimados: medir en el primer piloto).
