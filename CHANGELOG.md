# Changelog

Todos los cambios notables de NetPulse se documentan en este fichero.

El formato se basa en [Keep a Changelog](https://keepachangelog.com/es/1.1.0/),
y este proyecto se adhiere a [Versionado Semántico](https://semver.org/lang/es/).

## [Unreleased]

## [2.12.0] - 2026-08-24

### Added

- **Módulo WireGuard de orquestación (Fase 10.3, #92)**: despliegue declarativo de peers WireGuard (interfaz wg0 + secciones wgpeer) vía plan/apply/state, healthcheck con `wg show`, rollback automático si el túnel no queda up y protección anti-lockout (nunca borra el peer del admin).
- **Recuperación de agentes caídos desde la app (#245)**: nueva vista de flota de agentes con estados fresh/stale/offline y last-seen, acción de reinstall vía SSH y comando one-liner como fallback.
- **Labels de puertos/VLAN de LuCI como fuente de nombres en topología (#258)**: el agente lee `port_labels`/`vlan_labels` de `/etc/config/luci` y la topología las usa como nombre preferente.
- **Demo**: tracker GoatCounter opcional (`VITE_GC_COUNT`) y nombre propio por defecto en inglés para primeros visitantes.

### Changed

- **CSP endurecida (#212)**: el script anti-black-screen y el splash se externalizan y se elimina `unsafe-inline` de `script-src`/`style-src`.
- **Landing**: animaciones de lightbox y botones (press feedback), reveals restringidos a cards/paneles.
- **Topología**: rings wifi más compactos y cable del host alejado de los APs.

### Fixed

- **SSE (#199, #200)**: limpieza de conexiones por identidad (no mata streams nuevos con el mismo slug) y broadcast no bloqueado por clientes lentos.
- **Backend auditoría (#202-#206, #209)**: race en notifier Close, reintentos de webhook con backoff, fugas/derribos en la pool SSH, poda de presencia sin límite y hash bcrypt dummy válido (sin oráculo de enumeración de usuarios).
- **Paginación (#201)**: `start` negativo ya no causa panic con queries enormes.
- **Reports (#207)**: UpPct semanal clampeado a 100 igual que availability.
- **LLDP (#208)**: `lldpDownUntil` protegido con mutex.
- **Frontend (#141, #172, #183, #187)**: snackbar de instalación PWA, layout mobile de topología, transición con slide entre vistas y rings de topología compactos.
- **i18n de demo (#237, #238)**: idioma forzable en build y nombres propios traducidos al idioma activo.
- **Test de availability**: clampeado el valor esperado para que no dependa de la hora del día.

## [2.11.2] - 2026-08-23

### Added

- **TLS híbrido post-quantum y auto-actualización de agentes** destacados en la landing (X25519MLKEM768, Go 1.24+; self-update por flota).
- **Detalle del router**: campo `lldpAvailable` para señalar cuándo falta `lldpd` en el dispositivo sondeado (requisito para detección de switches gestionados).
- **Empaquetado OpenWrt**: los releases ahora adjuntan `.ipk`/`.apk` del agente y `.ipk` de `luci-app-netpulse` (el workflow no se disparaba por el GITHUB_TOKEN).
- **Alertas de demo localizadas** al idioma activo (estático y modo demo).
- **Mensaje amistoso "solo con backend en vivo"** en Reports/Roaming cuando la demo estática sirve HTML para `/api/*`.

### Fixed

- **Seguridad**: `http.Server` con ReadTimeout/WriteTimeout (SSE exento); `login_attempts` se resetea al expirar el bloqueo; verificación de Origin en mutaciones (CSRF defensa en profundidad); el token de pairing ya no se loguea en claro; `413` real para bodies sobredimensionados; `AUTH_PASS` mínimo 10; el webhook se loguea solo por host; el backup descargable documenta que contiene credenciales.
- **Alerta de dispositivo desconocido**: ya no se re-dispara por cada reconexión (memoria por MAC) ni para dispositivos con hostname conocido.
- **Alertas offline** de dispositivos conocidos se limpian al recuperar la conexión (flapping IPv6).
- **Descubrimiento de links LLDP** por identidad de router (fallback mgmt-IP/nombre) cuando la chassis-MAC difiere de la del bridge.
- **Gateway sin SSH** ya no se muestra como "offline": se distingue de "sin acceso".
- **Topología**: el popup de detalle ya no desaparece antes de clicar.
- **WAN**: el jitter simulado solo actúa en modo demo (no corrompe el chart en vivo).
- **Frontend**: fetch races canceladas (Roaming/Reports), polling con cleanup (applyPlan, banner), NaN en AdGuard card, claves estables en Home, unread coherente, timers limpiados, 401 de `/api/dawn` redirige a login, `noUncheckedIndexedAccess` habilitado.
- **A11y**: tabs WAI-ARIA con navegación por teclado.

### Changed

- El release v2.11.1 ajustó `AGENT_VERSION` para que derive del tag y `httpapi.Version` sea inyectable; este release lo consolida.

## [2.11.0] - 2026-08-23

### Added

- **Firmware objetivo por router**: versión configurable en Ajustes; si el
  firmware instalado no la cumple, NetPulse emite una alerta no urgente, marca
  el router en la lista y penaliza ligeramente su salud. #241
- **Rotación de la clave SSH del servidor**: `POST /api/config/sshkey/rotate`
  (admin) con confirmación en la UI; respalda el par anterior y regenera uno
  nuevo. #242
- **Actualización del agente desde la app**: `updateAvailable` en la lista de
  agentes y botón "Actualizar agente" en el detalle del router; el agente
  descarga el binario nuevo del servidor, lo intercambia y se reinicia solo. #243
- **Reinstalación del agente desde la app**: botón junto a LuCI/SSH que
  repara un agente borrado (p. ej. por una actualización de firmware del
  router) vía SSH: descarga el binario, escribe la config y reactiva el
  servicio, rotando el token. #246
- **Estados de agente diferenciados**: "Agente caído" (registrado pero sin
  respuesta) frente a "Agente no instalado" (router agent-only sin agente),
  ambos en rojo; se expone `router.agentOnly` en el view-model.
- **Roadmap**: Fase 17.12 "Firmware update in-app (labs)".

### Fixed

- **Copiar comando SSH en LAN HTTP**: fallback a `execCommand` cuando la
  Clipboard API no está disponible (HTTP), y el toast solo confirma si la
  copia tuvo éxito.
- **`updateAvailable` coherente**: la versión del binario embebido ahora se
  inyecta por CI con el mismo valor que la versión del agente (antes una
  constante fija que nunca coincidía).

## [2.10.2] - 2026-08-21

### Added

- **Dispositivos de confianza**: nueva allowlist `known_macs` gestionable desde
  Ajustes ("Dispositivos de confianza"). Una MAC registrada nunca dispara la
  alerta "Dispositivo desconocido" y su nombre se usa como alias. Endpoints
  admin `GET/PUT/DELETE /api/settings/known-macs`. #196

### Fixed

- **Falsos positivos de "Dispositivo desconocido"**: la alerta se disparaba
  con los propios routers de la red y con dispositivos ya conocidos cuando el
  sondeo del gateway era lento (su bridgeMAC se colaba como cliente de un AP)
  o el lease DHCP no se resolvía en ese tick. Ahora la bridgeMAC de cada
  router se persiste en `routers.mac` y se excluye siempre, y la alerta de
  desconocido deja de ser urgente (warn informativo, categoría `clients`
  pasa a `all`). #196

## [2.10.1] - 2026-08-18

### Fixed

- **Aviso de actualización sin versión duplicada**: cuando GitHub publica una
  release cuyo nombre coincide con el tag (p. ej. `v2.10.0`), el banner ya no
  muestra "Nueva versión disponible: v2.10.0 — v2.10.0". Ahora solo aparece la
  versión una vez. #191

- **Hint visible en instalaciones estables**: en despliegues que usan
  `install.sh` (modo estable), el banner ahora muestra debajo del mensaje
  "Instalación estable: re-ejecuta install.sh para actualizar", haciendo
  explícito por qué no hay botón "Actualizar" y clarificando el enlace
  "Ver en GitHub" como siguiente paso. #191

## [2.10.0] - 2026-08-14

### Added

- **Eventos offline/online de dispositivos**: el poller registra las
  transiciones de presencia wireless (un dispositivo que deja de verse 3
  ticks seguidos = `offline`; al reaparecer = `online`) con timestamp, router
  y última señal. Nueva tabla `device_events` (retención 30 días) y endpoint
  `GET /api/device-events` con filtros `limit/since/router/mac/state`.
  Permite correlacionar incidentes (qué cayó, cuándo, cuánto tardó en volver).
  #184

## [2.9.6] - 2026-08-13

### Added

- **Historial de actualizaciones**: nueva tabla `update_history` y endpoint
  `GET /api/updates/history` (admin) con el registro de cada apply (cuándo,
  SHA desde/hacia, iniciador, duración, estado). Visible en Ajustes. #159

- **Velocidad WAN contratada**: campo en Ajustes para declarar el ancho de
  bajada/subida contratado (Mbps). Se persiste y el gráfico de tráfico WAN
  muestra una línea de referencia al nivel contratado y la utilización
  ("Pico hoy X % del contratado"). `GET/PUT /api/settings/wanspeed` (admin)
  y `wan.contractDownMbps/contractUpMbps` en el overview. #151

### Changed

- **Readiness checks antes de aplicar actualizaciones**: `GET /api/update/status`
  expone pre-flight checks (disco, git limpio, red a GitHub, sin update en
  curso); la UI bloquea el botón de aplicar si no están listos. #160

- **Confirmación post-update**: tras aplicar y reiniciar, si el commit cambió
  se muestra un aviso de una sola vez "Actualizado a <sha>". #161

### Fixed

- **Readiness git**: el check de working tree limpio ignoraba los ficheros
  untracked (`.env`, `data/`, binarios, backups), bloqueando el apply para
  siempre en un layout real. Ahora usa `--untracked-files=no` (un
  `git reset --hard` no toca untracked). #160

## [2.9.5] - 2026-08-13

### Fixed

- **Ingesta externa en modo demo**: los colectores/scrapers externos (p. ej.
  un scraper de switch vía timer) recibían `409 demo_read_only` al empujar
  datos en modo demo y reportaban fallos de un estado esperado. Ahora la
  ingesta se acepta como no-op benigno (`202` + `demo:true`) sin tocar la BD,
  manteniendo la validación de token y firma HMAC. #168

## [2.9.4] - 2026-08-13

### Fixed

- **Resumen en móvil**: las tarjetas de servicios forzaban columnas
  implícitas en la rejilla de 1 columna, dejando el saludo y el tráfico
  montados en la misma línea horizontal (el saludo colapsaba a ancho 0).
  Ahora usan clases de columna estáticas solo a partir de `lg`. #173

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
