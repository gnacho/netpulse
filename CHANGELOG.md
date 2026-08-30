# Changelog

Todos los cambios notables de NetPulse se documentan en este fichero.

El formato se basa en [Keep a Changelog](https://keepachangelog.com/es/1.1.0/),
y este proyecto se adhiere a [Versionado Semántico](https://semver.org/lang/es/).

## [Unreleased]

## [2.22.0] - 2026-08-30

### Added

- **Módulo de orquestación DAWN (#402)**: nueva tarjeta "DAWN" en `/orchestration` para desplegar y alinear la configuración de roaming (802.11k/v/r, mobility domain, shared key, broadcast IP, random_bssid=0) en los APs. El backend probea el estado actual vía SSH, genera un plan declarativo con las ops necesarias, y verifica la malla DAWN tras el apply (`ubus call dawn get_network`). Incluye rollback y i18n ES/EN.

## [2.21.0] - 2026-08-30

### Added

- **Rollback de planes delegado a NetGrip (#397)**: `POST /api/plans/{id}/rollback` ahora delega las ops inversas al executor de NetGrip cuando el router tiene executor token (mismo criterio que el apply), asentando el plan como `rolled_back` vía la máquina de estados existente. El fallback al agente embebido por SSE se mantiene.

## [2.20.0] - 2026-08-30

### Added

- **Delegación de planes a NetGrip (#339)**: si un router tiene un executor token de NetGrip (enviado con el snapshot en #340), `POST /api/plans/{id}/apply` delega la ejecución de las ops a NetGrip vía `POST /api/executor/apply`. Si NetGrip no responde, se mantiene el fallback al agente embebido por SSE.
- **Backups centralizados de snapshots UCI (#340)**: el endpoint `POST /api/config-backup` ahora recibe snapshots gzip desde NetGrip autenticados con el token de agente. Los endpoints de lectura/borrado (`GET /api/config-backup`, `GET /api/config-backup/{id}`, `DELETE /api/config-backup/{id}`) requieren rol admin.

### Fixed

- **Texto seleccionable en el feed de alertas (#393)**: las filas del feed de `/alerts` renderizaban todo su contenido dentro de un `<button>`, y los navegadores desactivan la selección de texto dentro de los botones. La fila pasa a ser un `div role="button"` (Enter/Espacio siguen funcionando) con un guard por movimiento de puntero: arrastrar para seleccionar no despliega la alerta, un click normal sí. Corrige además el anidado inválido de botones (silenciar dentro de la fila).
- **Banner de actualización sincronizado con el check manual (#392)**: el ribbon de nueva versión y la comprobación de Ajustes comparten el mismo estado; ya no divergen tras un check manual.

## [2.19.2] - 2026-08-30

### Security

- El servidor rechaza al arrancar valores placeholder en `AUTH_PASS` y `SESSION_SECRET` (#391).

## [2.19.1] - 2026-08-29

### Fixed

- Añade el tracker de Umami a la landing para registrar visitas (#384).

## [2.19.0] - 2026-08-29

### Added

- **AdGuard Home remoto (#379)**: el ajuste de AdGuard ahora permite elegir entre el modo GL.iNet (en el router) y el modo estándar (instancia remota). En modo estándar se configura FQDN/IP + puerto, y NetPulse se conecta directamente por HTTP a la API /control.
- NetGrip embedded agents are told apart (kind netgrip) in the agents view:
  Estado (active/inactive/not installed) and a new Agente column (NetPulse/
  NetGrip/Externo). NetPulse can update them centrally via the SSE upgrade
  event (NetGrip runs its own self-updater); rearm/reinstall are refused.
- install-agent.sh and the agent apk hand off to NetGrip when detected
  instead of installing the standalone binary; --update-netgrip deploys the
  latest NetGrip release with a downgrade guard.
- Public `agent/runtime` package so the agent can run embedded (used by
  NetGrip).
- Releases now attach the on-box server package (netpulse .apk/.ipk).

### Fixed

- **Caché de DNS con TTL (#376)**: el agente cachea las resoluciones respetando el TTL real, reduciendo latencia y consultas repetidas.
- **UI en inglés sin cadenas españolas (#378)**: el feed de alertas demo ahora es bilingüe y no muestra textos en español cuando la interfaz está en inglés.
- **Documentación del descubrimiento (#375)**: el README explica qué fuentes usa NetPulse para descubrir dispositivos (ARP, DHCP leases, WiFi, LLDP, DNS, OUI).
- Agent no longer burns CPU on iwinfo shell-outs (reported 25% on MT7621):
  clients come from ubus hostapd get_clients, single-pass iwinfo fallback,
  radios cached out of the event path.
- Pre-existing frontend build errors on main (unused vars/imports,
  possibly-undefined palettes) that broke the dist CI job.

## [2.18.0] - 2026-08-28

### Added

- **Alertas inteligentes de puerto (#303)**: `PortMonitor` rastrea transiciones de estado y detecta flapping (>5 transiciones en 10 min). Integrado en las rutas de agente, SSH y SNMP.
- **Alerta puerto fantasma (#307)**: alerta cuando un puerto con trafico estable se queda sin trafico durante 12+ muestras consecutivas. Requiere historico previo para evitar falsos positivos.
- **Alerta enlace degradado (#308)**: alerta cuando la velocidad negociada baja por debajo de la mitad del historico dominante durante 3+ polls consecutivos.
- **Health score por puerto (#299)**: puntuacion 0-100 por puerto basada en estado de enlace (30%), utilizacion (25%), errores (25%) y flapping (20%). Expuesto en la API de detalle del router.
- **Widget panel frontal del switch (#306)**: LEDs por puerto (verde/ambar/rojo/gris) con etiquetas de velocidad e indicadores PoE. Tooltips al pasar el raton. Activo para switches con 8+ puertos. Colapso responsive en movil.

## [2.17.0] - 2026-08-28

### Added

- **Poller SNMP para switches gestionados (#309)**: nuevo paquete `server-go/internal/snmp/` con gosnmp. Sondeo de ifTable/ifXTable (estado, velocidad, contadores, errores), dot1dTpFdbTable (tabla MAC-puerto), sysUpTime/sysDescr. Config SNMP por router (toggle + community + puerto). Integrado en el ciclo de polling junto a SSH/agente.
- **Series temporales por puerto (#302)**: paquete `server-go/internal/portseries/`. Tres tablas SQLite: raw (7d), buckets 5min (1 año), diario (indefinido). Job nocturno de rollup + purga. API `GET /api/routers/:id/ports/:portId/series`. Sparkline SVG en el panel de puerto con selector 24h/7d/30d.

## [2.16.0] - 2026-08-28

### Added

- **Base OUI embebida (#314)**: 39769 prefijos MAC para resolver fabricante de dispositivos en vivo. `Device.Manufacturer` ahora se rellena desde el prefijo OUI en lugar de "Desconocido".
- **Sugerencias accionables en alertas (#310)**: campo `hint` opcional con sugerencia estática por tipo de alerta (agente caído, temperatura alta, WAN caído, etc.). Visible en el feed, push notifications y service worker.
- **Topología LLDP como ground truth (#300)**: los enlaces de infraestructura (router-switch, switch-switch en cadena) usan LLDP como fuente primaria. FDB queda como fallback cuando lldpd no está instalado.
- **Vista VLAN de solo lectura (#315)**: panel colapsable en el detalle del router con tabla de VLANs (ID, puertos, tagged/untagged, PVID) parseada de `bridge vlan show`.
- **Monitorización SFP/DDM (#313)**: sonda `ethtool -m` en el agente + parsing de campos SFP del beacon. Temperatura, voltaje y potencia óptica TX/RX en el detalle de puerto. Alertas por RX < -14 dBm y temperatura > 70 °C.
- **Clasificación por huella DHCP/LLDP (#301)**: el clasificador de dispositivos amplía las reglas de hostname con DHCP vendor class, client-id y capacidades LLDP.
- **Descubrimiento de tipo de dispositivo (#304)**: filtros por tipo y badges en el inventario de dispositivos. La API devuelve `typeCounts` agrupado.

## [2.15.0] - 2026-08-28

### Added

- **Switches gestionados como agentes beacon UDP (#291, #292, #294)**: los switches RTL8372/8373 con firmware RTLPlayground (p. ej. el KP-9000) se integran por un canal UDP propio: beacon de estado cada 30 s (velocidad de enlace y contadores de tramas por boca), datagrama FDB cada 5 min (~600 B, MACs normalizadas al formato canónico) y eventos inmediatos (loop, port up/down, disabled/recovered) que alimentan las alertas. El token del agente se valida igual que el ingest HTTP (kv sha256), con rate limit por IP y estampado de hora en el servidor (el emisor no tiene RTC).
- **Descubrimiento y pareado de switches (#291)**: un switch sin parear anuncia su identidad (modelo + versión de firmware) por broadcast; NetPulse lo lista como candidato y el admin lo configura con un slug y el comando `beacon <ip> <slug> <token>`.
- **Detección de reinicio del switch (#291)**: el contador seq del beacon vuelve a 1 tras un boot; el servidor lo detecta y publica una alerta informativa "Switch reiniciado".
- **Atribución de bocas consciente de la infraestructura (#291)**: los clientes WiFi atribuyen la boca a su AP; los prefijos OUI de virtualización marcan hipervisores con su VM/CT detrás; las etiquetas curadas nombran dispositivos o infraestructura; los satélites ya no adoptan dispositivos aprendidos por su uplink (el switch deja de atribuirse toda la red vista por lan1).
- **Documentación de onboarding (#292/#293)**: `docs/AGENTE-RTL-SWITCH.md` con el procedimiento completo para incorporar switches embebidos.

### Fixed

- **Test beacon con race en CI lenta (#295)**: la consulta a la kv podía ganar al write del goroutine del listener; ahora se espera con deadline.
- **Ventana sin atribución tras reinicios**: el beacon persiste la última tabla MAC conocida, así el arranque restaura los nombres de bocas y clientes al instante.
- **Panel de detalle del AP**: la fila de backhaul/latencia ahora usa la IP real del gateway (192.168.1.1) en vez de un literal del canon demo (192.168.8.1) y ocupa una fila compacta a ancho completo.
- **Las bocas de un switch cableado ya no muestran secciones WiFi vacías** y se mantienen en una única fila horizontal con scroll si no caben.

## [2.14.0] - 2026-08-27

### Added

- **Asistente de actualización multi-paso (#280)**: aplicar una actualización ya no es un botón ciego. El asistente confirma con el changelog real (cuerpo del commit en rolling, notas del release en estable) y un aviso explícito de caída del servicio que hay que marcar; ejecuta con pasos visibles (obtener código, descargar binario, verificar integridad, instalar, reiniciar) alimentados por hitos `STEP:`/`PROGRESS:` del script de update, transmitidos por SSE (`GET /api/update/stream`, admin) con fallback a polling; y recarga la app solo cuando responde un proceso distinto (uptime baseline). La descarga del binario de CI se verifica contra el digest sha256 publicado antes del swap.
- **Sección de flota de agentes en Dispositivos (#284)**: la vista de agentes pasa a ser una sección al final de la página de dispositivos (la ruta `/agents` redirige), con rearmar, copiar comando, reinstalar y **actualizar agente** por fila.
- **Actualización de agentes con progreso en vivo (#284)**: el agente reporta pasos con timestamps (descargando con % cada 5%, comprobando integridad, instalando, reiniciando) que el servidor expone como historia (`upgrade.steps`) y la UI pinta como línea de tiempo; los runs rápidos se resumen ("Actualizado · N pasos · X s").
- **Cola de actualizaciones para agentes desconectados (#284)**: si un agente no tiene su stream SSE conectado, el comando de upgrade se encola y se envía en cuanto reconecta (flush on-connect del hub); la UI lo muestra como "esperando conexión" y lo sigue hasta que resuelve.
- **Verificación de integridad del binario de agente (#284)**: el servidor sirve el binario embebido con cabecera `X-Checksum-Sha256` y el agente comprueba el sha256 antes de hacer el swap (paso "comprobando integridad"); si no cuadra, aborta sin tocar el binario en marcha.
- **Menús renombrados (#284)**: "Routers" pasa a "Dispositivos" y "Dispositivos" a "Clientes" (más preciso: la página lista routers, switches y APs), alineando paleta de comandos, resumen y ajustes.

### Fixed

- **Backoff de reconexión del agente que nunca se reseteaba (#284)**: tras varios reinicios del servidor, los agentes acababan reintentando su stream de comandos cada 5 minutos para siempre, con lo que el servidor solo alcanzaba a los que habían reconectado por casualidad ("upgrade enviado a 1 de 5"). El backoff ahora se resetea tras una conexión establecida.
- **Sección "Novedades" vacía (#280)**: cuando el cuerpo del commit solo contiene trailers de git (Co-authored-by, etc.), la sección de novedades del asistente ya no se renderiza.
- **Upgrades de flota no muestreados (#284)**: al lanzar "Actualizar agentes" se dispara un refresco inmediato de agentes que engancha el sondeo rápido (2 s), en vez de esperar al ciclo de reposo de 30 s con el que un upgrade en LAN ocurría entero sin verse.

## [2.13.1] - 2026-08-25

### Fixed

- **Consolidación de alertas de auto-rearme (#271)**: el supervisor emitía una alerta "Auto-rearme sin recuperación" por cada reintento (cada 10 min), llenando el feed con decenas de copias del mismo incidente. Ahora la primera falla crea la alerta y los reintentos la actualizan (ts/descripción) en su sitio; al recuperarse el agente se cierra el incidente y el próximo fallo crea una alerta nueva.
- **Boca WAN con datos reales de conexión (#276)**: el detalle del gateway muestra en el tooltip de la boca WAN el protocolo (PPPoE), IP pública, puerta de enlace y servidores DNS, sondados en vivo vía `ubus call network.interface.wan status` (cache 60 s).
- **Botón atrás fijo en el detalle de router (#275)**: el botón "Atrás" vivía en la cabecera del detalle y se perdía al hacer scroll. Ahora es una flecha icon-only fija en el topbar (desktop) y en el header móvil, siempre visible en `/routers/:id`.
- **Tooltips de bocas LAN/WAN estilo tarjeta (#276)**: al pasar el cursor sobre una boca se muestra una tarjeta con el dispositivo conectado (nombre, velocidad, MAC e IP); las libres muestran "libre".
- **Topología sin hint engañoso (#276)**: eliminado el texto "clic para ver el dispositivo" de la tarjeta de dispositivo (muchos chips no se pueden clicar porque otros se interponen).
- **Reenvío DNS del módulo AdGuard al puerto correcto (#270)**: dnsmasq reenviaba a la UI HTTP (puerto 3000) en vez del puerto DNS real; ahora usa un puerto DNS dedicado (5353) fuera del 53 para no colisionar con dnsmasq.
- **Test de informe semanal independiente de la fecha (#273)**: los tests usaban semanas ISO fijas y rompían el CI al avanzar la semana actual; ahora calculan las fechas relativas a hoy.

## [2.13.0] - 2026-08-24

### Added

- **Resumen reorganizado (#265)**: tarjeta hero en una sola columna centrada: saludo con el estado debajo ("Tu red está perfecta" cuando todo va bien, o contador dinámico "N alertas importantes" cuando hay penalizaciones), donut de salud a 220 px centrado y fila de stats (latencia, dispositivos) debajo. El contador sale del breakdown existente del healthScore, sin cambios de backend.

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
