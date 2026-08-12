# Changelog

Todos los cambios notables de NetPulse se documentan en este fichero.

El formato se basa en [Keep a Changelog](https://keepachangelog.com/es/1.1.0/),
y este proyecto se adhiere a [Versionado Semántico](https://semver.org/lang/es/).

## [Unreleased]

## [2.9.3] - 2026-08-12

### Fixed

- **Resumen WAN en modo live**: "Pico hoy", "Media" y "Total 24h" se
  calculaban solo en el modo demo; en live quedaban siempre a 0 / "—".
  Ahora se calculan desde las métricas almacenadas del gateway (pico del
  día con su hora, media de bajada 24h y volumen total 24h). #169

## [2.9.1] - 2026-08-11

### Added

- **Orquestación Fase 18 (17.2–17.4)**: módulos de WiFi invitado (SSID
  aislado con subred propia), DDNS (ddns-scripts vía UCI) y QoS/SQM
  (sqm-scripts con cake) con UI unificada en `/orchestration`. El motor
  gana los Kinds `uci_add`, `uci_set_named`, `uci_delete_section` y
  `tcp_check` (healthcheck de puerto con reintentos). #131 / #133 / #136

- **Topología portable**: refactor del layout (anillos densos, resolver de
  colisiones, círculo virtual) sin coordenadas hardcodeadas de una red
  concreta. #138 / #140

- **Ajustes → Acerca de**: los cuatro tiles enlazan ahora a su destino
  (GitHub, la web del proyecto, Ko-fi y el Club Cloudless). #162

### Fixed

- **AdGuard solo en el gateway**: el plan se rechaza (422) si el router
  destino no es el gateway, ya está el fork de fábrica o no hay RAM
  suficiente, salvo opt-in explícito. #120 / #130

- **Orquestación oculta por defecto**: el menú `/orchestration` solo se
  muestra si el admin lo activa en Ajustes. #121 / #129

## [2.9.0] - 2026-08-09

### Added

- **Fase 17.1 — Módulo AdGuard Home (despliegue completo)**: el motor de
  orquestación (Fase 10) pasa de configurar UCI a desplegar un servicio
  entero. Un probe SSH detecta el escenario del router (firmware GL.iNet
  con fork propio → abort; apk/opkg en feeds; binario ya presente; binario
  oficial de GitHub) y genera el plan correspondiente. Nuevos Kinds del
  executor: `apk_install`, `download` (allowlist URL), `write_file`,
  `extract_tarball`, `chmod`, `service start/stop`, `mv`. El endpoint
  `POST /api/plans` devuelve 422 `managed_by_firmware` para routers con
  AdGuard de fabricante. UI `/orchestration` muestra el método detectado.
  #116 / #122

### Fixed

- **Discover responde a la cancelación del cliente**: el scan de la LAN
  propagaba el `context.Context` de la request HTTP, así que cancelar la
  petición (cerrar la pestaña, corte de red) dejaba el barrido corriendo
  con el lock de caché tomado. Ahora `pool`, `tcpOpen`, `postUbus`,
  `getRoot` y `probeSsh` respetan `ctx` y el scan aborta en cuanto el
  cliente se desconecta. #107 / #123

- **Home: el tráfico WAN ya no aparece duplicado**: HeroStrip mostraba
  `↓ down / ↑ up` instantáneos junto a WanTraffic, que ya pinta el gráfico
  down/up y las métricas 24h. HeroStrip pasa a mostrar solo saludo +
  HealthRing + latencia (y nº de dispositivos en móvil). #108 / #124

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
