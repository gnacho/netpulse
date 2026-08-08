# Changelog

Todos los cambios notables de NetPulse se documentan en este fichero.

El formato se basa en [Keep a Changelog](https://keepachangelog.com/es/1.1.0/),
y este proyecto se adhiere a [Versionado Semántico](https://semver.org/lang/es/).

## [Unreleased]

## [2.8.0] - 2026-08-08

### Added

- **Fase 14.3 — Estado 802.11r en /roaming**: nueva pestaña que muestra por
  SSID y por router si Fast BSS Transition está activo, con mobility_domain,
  modo FT (over-the-air/over-the-DS), tipo de auth (PSK local vs RADIUS
  externo) y banderas 802.11k/v/w (PMF). Datos leídos por SSH de
  `uci show wireless`. Endpoint `GET /api/dot11r` (503 si ningún router lo
  soporta). #105 / #106

- **Fase 14.4 — Utilización por canal WiFi en /roaming**: nueva pestaña
  Survey con noise floor y % de ocupación por canal, leído de
  `iw dev wlanX survey dump`. Los canales congestionados son la causa más
  habitual de WiFi lento en casa. Endpoint `GET /api/survey`. #111 / #113

- **Fase 14.5 — Eventos de roaming con histórico en /roaming**: nueva
  pestaña Eventos con feed persistente de 30 días de conexiones,
  desconexiones y decisiones DAWN. Ingesta continua cada 60s vía SSH
  `logread | grep`, deduplicación por hash. Endpoint
  `GET /api/roam-events`. #114 / #115

- **Editar configuración de router**: nuevo endpoint
  `PUT /api/config/routers/{id}` que permite editar host, nombre, tipo,
  gateway y `agent_only` sin borrar y recrear (lo cual perdía el ID y el
  histórico de métricas). UI de edición inline en Ajustes. #109 / #110

### Fixed

- **GetDawn timeout en routers agent-only**: el handler intentaba SSH a
  switches sin SSH (switch16), perdiendo 4s por cada llamada. Ahora se
  saltan los routers `agent_only`.

- **Survey filtraba interfaces no-wlan**: el parser solo aceptaba
  `wlan0/wlan1`, descartando silenciosamente todos los radios de los
  Redmi AX6 (driver mt76, interfaces `phy0-ap0`/`phy1-ap0`). Acepta
  cualquier interfaz listada por `iw dev`.

### Changed

- **Routers agent-only (Phase 14.3 bundled)**: mergeada la rama limpia
  `fix/agent-only-switches` con `RouterConfig.AgentOnly`, `StalePayload`,
  rol Switch, gate en topología y migración de BD. Antes no estaba en
  main y bloqueaba la compilación de la Fase 14.3.

## [2.7.2] - 2026-08-07

### Fixed

- **Single-flight de GetOverview**: un panic en buildOverview congelaba el poller
  para siempre (sfInFlight sin reset, canal nunca cerrado); los seguidores 2..N
  recibían (nil, nil) del canal cerrado. Reescrito con estructura compartida
  sfCall, close(done) como pura señal para N seguidores, y recover del líder
  transformando panic en error.
- **Cliente SSE zombi bloqueaba el broadcast**: un peer desaparecido sin cerrar
  TCP llenaba el buffer del kernel y fmt.Fprint bloqueaba el Broadcast síncrono
  de todos los clientes (~15 min). Añadido write deadline 10s vía
  ResponseController en Hub y AgentHub. Añadido ReadHeaderTimeout: 10s al
  http.Server.
- **Carrera en SSHPool.dial**: dos dialers concurrentes al mismo host filtraban
  una conexión SSH. Añadido single-flight por host (canal dialing) y chequeo de
  p.closed en el mutex.
- **%v → %w en dos envolturas de error** (config.go y rearmer.go) que rompían
  errors.Is/As aguas arriba.

## [2.7.1] - 2026-08-07

### Fixed

- **Panic del poller por notifier nil-encapsulado**: un puntero `nil` empaquetado
  en la interfaz `alerts.Notifier` (webhook inactivo) no es `nil` para `if n != nil`,
  y `Notify` paniqueaba al emitir una alerta URGENTE de "dispositivo desconocido".
  Los notifiers se filtran ahora en su tipo concreto antes de empaquetar en la
  interfaz, con defensa adicional vía `reflect` en `notifierChain.Notify`.

## [2.7.0] - 2026-08-06

### Added

- Fase 12 en el ROADMAP: auditoría de seguridad y robustez (release bug-hunting).
- `TRUST_PROXY`: cuando el servidor va detrás de un proxy/reverse, la IP del
  cliente se toma de `X-Forwarded-For` y el tráfico HTTPS de
  `X-Forwarded-Proto`. Por defecto no se confía en esas cabeceras.
- Ventana de frescura del `ts` del agente en la ingesta (`AGENT_MAX_TS_DRIFT_S`,
  default 5 min): rechaza pushes con marca de tiempo fuera de rango.

### Fixed

- **Bypass de rate-limit por `X-Forwarded-For` falso**: sin `TRUST_PROXY`, la IP
  del cliente es la del socket; ya no se puede falsear para evadir el bloqueo
  de fuerza bruta del login ni el rate-limit de la ingesta.
- **Anti-replay en la ingesta de agentes**: un token robado ya no permite
  reinyectar payloads antiguos capturados.
- **Body sin límite en endpoints JSON** (login, usuarios, config, alertas,
  push): techo de 64 KB por petición.
- **Password mínima 10 caracteres** en alta y cambio de contraseña (antes 6).

## [2.6.0] - 2026-08-05

- Fase 7 (agente a fondo): iw events en tiempo real, SSE bidireccional
  (AgentHub + refresh), `.ipk` vía opkg en gateway, profiling, CI de
  empaquetado `.ipk`/`.apk`, landing web con posicionamiento OpenWrt nativo.
- Fase 8 (consolidación): métricas en `/api/health`, registry de agentes
  persistente, escalera de retención, recharts v3, webhook saliente con DLQ.
- Actualización de stack: TS 7, Tailwind 4, Vite 8, react-router 8, sqlite 1.56.
