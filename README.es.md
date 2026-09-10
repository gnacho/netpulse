# NetPulse

<p align="center">
  <a href="README.md">English</a> |
  <a href="README.es.md">Español</a>
</p>

<p align="center">
  <a href="https://netpulse.cloudless.club"><img alt="Sitio web" src="https://img.shields.io/badge/Website-netpulse.cloudless.club-blue"></a>
  <a href="https://demo.netpulse.cloudless.club"><img alt="Demo en vivo" src="https://img.shields.io/badge/Live%20demo-demo.netpulse.cloudless.club-blue"></a>
  <a href="https://github.com/gnacho/netpulse/releases"><img alt="Release" src="https://img.shields.io/github/v/release/gnacho/netpulse"></a>
  <a href="https://github.com/gnacho/netpulse/actions/workflows/release.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/gnacho/netpulse/release.yml?branch=main"></a>
  <a href="LICENSE"><img alt="Licencia" src="https://img.shields.io/github/license/gnacho/netpulse"></a>
  <a href="https://ko-fi.com/gnacho"><img alt="Apóyame en Ko-fi" src="https://img.shields.io/badge/Ko--fi-Donate-ff5e5b?logo=ko-fi&logoColor=white"></a>
</p>

<p align="center"><a href="https://demo.netpulse.cloudless.club"><strong>Prueba la demo en vivo</strong></a> en <code>demo.netpulse.cloudless.club</code></p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/hero-es-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/hero-es-light.png">
    <img alt="Resumen de NetPulse con puntuación de salud de red, gráfico de tráfico en vivo, estadísticas de AdGuard Home, peers WireGuard y el feed de alertas" src="assets/hero-es-light.png" width="800">
  </picture>
</p>

NetPulse es una PWA de solo lectura para monitorizar una red doméstica
construida con routers OpenWrt/GL.iNet: estado de la flota, salud por
router, dispositivos conectados, mapa de topología en vivo, peers
WireGuard, estadísticas de AdGuard Home y alertas, en tiempo real. Un
único binario Go estático con el frontend embebido, autoalojado en una
caja Linux pequeña.

> **Prueba la demo en vivo**
>
> Mírala en funcionamiento sin instalar nada. Entra en **[demo.netpulse.cloudless.club](https://demo.netpulse.cloudless.club)** — una red de ejemplo completa, sin registro. En modo de solo lectura, para que explores sin riesgo.

## ¿Por qué existe?

Siempre he creído en la soberanía digital: si un dispositivo te hace
depender de su cloud, de su firmware o de su fabricante, no es 100% tuyo.
Por eso siempre he priorizado hardware que se pueda flashear o rootear.
La distribución de mi casa acabó con cuatro routers: un Flint 2 como
principal y tres puntos de acceso Xiaomi AX6 comprados de segunda mano a
30 euros cada uno. Baratos, potentes, y todos corriendo OpenWrt. Esa
soberanía me permitió orquestar y personalizar la red a mi antojo (no sin
ciertos desafíos), pero siempre eché en falta una vista unificada de lo
que pasaba: qué se conecta dónde, qué va bien, qué no. No había nada, o
no supe encontrarlo, así que me puse manos a la obra. NetPulse es ese
visor global: analiza tu red, detecta anomalías y te avisa.

## ¿Por qué este stack?

- **Go, un único binario estático**: un monitor 24/7 en un LXC pequeño.
  ServeMux de `net/http` de la stdlib, sin framework, `go:embed` para el
  frontend. Actualizar es cambiar un fichero.
- **`modernc.org/sqlite`, CGO off**: totalmente estático, sin toolchain C
  en el destino. Series temporales, usuarios y sesiones en un único
  fichero SQLite embebido (WAL).
- **Solo lectura por diseño**: el servidor genera su propio par de claves
  ed25519; autorizas la clave pública en cada router y solo lee (ubus,
  `/proc`, iwinfo, `bridge fdb`, `wg show`). No puede cambiar tu red.
- **PWA React 19 + Vite + Tailwind**: instalable, en vivo por SSE (5 s),
  la misma shell de UI que mis otras apps.
- **systemd, sin Docker**: monitoriza una red; no necesita un contenedor
  para hacerlo.

## Características

- **Resumen de flota**: puntuación de salud, tráfico en vivo, latencia,
  estado por router (CPU, memoria, temperatura, uptime).
- **Mapa de topología en vivo**: inferido del FDB del bridge (y LLDP
  cuando está disponible), con clientes cableados e inalámbricos, switches
  e hipervisores detectados, y túneles WireGuard dibujados de peer a
  Internet.
- **Dispositivos**: cada cliente con clasificación por tipo (patrones de
  hostname + OUI), primer visto, banda, señal.
- **WireGuard**: peers, últimos handshakes, transferencia por peer.
- **AdGuard Home**: estadísticas de consultas y dominios más bloqueados.
- **Alertas**: temperatura, firmware disponible, nuevo dispositivo,
  handshake, con feed en la campana.
- **Auth multiusuario**: contraseñas bcrypt, idioma por usuario (ES/EN),
  roles admin y viewer.
- **Modo demo** (`DEMO_MODE=1`): una red de muestra de 67 dispositivos,
  sin necesidad de routers.
- **Sidecar collector opcional**: sondeo de latencia TCP por router con
  sus propias series temporales de largo plazo.

## Novedades

Cada versión se empuja a la flota a través del actualizador automático
incorporado. Las versiones nuevas muestran en la app un panel breve y en
lenguaje humano de "qué ha cambiado" para que sepas qué esperar antes de
actualizar. Aquí tienes un resumen en clave de las últimas mejoras, cada una
enlazada al issue de GitHub que la originó.

> Están escritas para personas, no como changelog: qué hace la función por ti,
> no los nombres de las funciones internas.

### Última (v2.28.19)

- **Las alertas (y todo lo demás) en tu idioma** ([#671](https://github.com/gnacho/netpulse/issues/671)). Títulos, descripciones y sugerencias de las alertas los generaba el servidor en español y los usuarios con la app en inglés veían el feed mezclado. Los eventos llevan ahora su tipo y sus valores, y la app los traduce al idioma activo — toda la familia, de dispositivos desconocidos a alertas de puertos.
- **Tus hostnames sobreviven al traductor del navegador** ([#666](https://github.com/gnacho/netpulse/issues/666)). La página declaraba español incluso con la UI en inglés, así que el navegador ofrecía traducirla — y el traductor "corregía" hostnames como crowed-pixeltab a crowded-pixeltab. El idioma declarado sigue ahora a la UI y los datos de red van marcados como no traducibles.
- **Paquetes OpenWrt para x86/64** ([#672](https://github.com/gnacho/netpulse/issues/672)). El agente y el servidor on-router se publican también para sistemas x86/64 (.ipk 24.10, .apk 25.12), con la arquitectura del paquete derivada del SDK.

### Anterior (v2.28.18)

- **El mapa de topología refleja ya dónde está enchufado cada equipo, y deja de saltar** ([#656](https://github.com/gnacho/netpulse/issues/656)). Los clientes por cable de un punto de acceso puente (dumb AP en la misma LAN que el router principal) aparecen bajo ese AP en vez de subir al padre; los dispositivos callados (TVs, receptores, reproductores) conservan su sitio en el mapa en lugar de caerse de su switch cada vez que se duermen; y los iconos ya no se intercambian posiciones en cada refresco.
- **El layout lo ajustas tú** ([#656](https://github.com/gnacho/netpulse/issues/656)). Un modo de edición (solo admin) permite arrastrar routers, switches y dispositivos para acercar o alejar los satélites; al guardar, el layout queda congelado y solo cambia si vuelves a editarlo o lo restableces al automático.
- **Los hosts de Proxmox se ven como nodos con sus VMs anidadas debajo** ([#561](https://github.com/gnacho/netpulse/issues/561)). Cada host PVE se dibuja como nodo redondo con su nombre, sus contenedores agrupados bajo él con badge +N, y arrastrar el host mueve a toda su familia de VMs. La tarjeta de dispositivo gana además un botón discreto "Etiquetar" para etiquetar sin teclear la MAC.

### Anterior (v2.28.17)

- **Un router OpenWrt ya no muestra un falso "host key changed" al sondearlo** ([#651](https://github.com/gnacho/netpulse/issues/651)). El descubrimiento (openssh con `accept-new`) solo fijaba la host key del algoritmo que negociaba (ed25519), mientras que el pool de sondeo (Go) negocia otro (ecdsa/rsa) con el mismo servidor; un router con varias host keys mostraba un "ssh host key changed" espurio y pedía un re-onboard sin motivo. El descubrimiento fija ahora **todas** las host keys del router (ed25519, ecdsa y rsa), así el sondeo siempre encuentra la suya, sin debilitar la protección anti-MITM: si un router cambia todas sus claves (reflash), la detección de "host key changed" sigue activa.

### Anterior (v2.28.16)

- **Los peers WireGuard se etiquetan por su nombre configurado en OpenWrt** ([#663](https://github.com/gnacho/netpulse/issues/663)). En la topología, los peers WireGuard que no estaban ya mapeados a un dispositivo NetPulse se muestran ahora con el nombre legible de la descripción UCI del peer (prioridad: nombre del dispositivo NetPulse → descripción UCI → IP del túnel) en lugar de la IP cruda del túnel. El sondeo lee `wg show dump` y `uci show network` en una sola sesión SSH (con marcador de separación) y conserva el estado de un túnel caído.

### Arreglado

- **Un router OpenWrt nativo ya no aparece como "Managed switch" en la tabla de dispositivos** ([#660](https://github.com/gnacho/netpulse/issues/660)). La columna Role distinguía solo gateway y "solo agente", y el resto caía en "Managed switch"; ahora se basa en el tipo del dispositivo y muestra "OpenWrt" para los routers nativos (agente SSH), dejando "Managed switch"/External para los sondeados por SNMP.
- **El switch sondeado por SNMP ya no pierde el historial de tráfico ni los dispositivos conectados** ([#661](https://github.com/gnacho/netpulse/issues/661)). El sondeo SNMP calculaba los contadores de bytes y la tasa por puerto pero nunca los persistía, así que la "historia de tráfico" del switch quedaba vacía aunque leyera los datos (ahora vía `recordPortSamples`, la misma ruta que SSH/agente). El FDB ya no descarta las MACs cuyo índice no resolvía a un puerto conocido (antes el switch mostraba 0 dispositivos pese a tener clientes) y hay un fallback a la tabla Q-BRIDGE para los switches que solo la exponen. La tarjeta de la flota pinta ahora el tráfico agregado del switch en bps, y la gráfica de puertos de un switch SNMP usa bps (los beacons sin contadores de bytes siguen en fps).

### Anterior (v2.28.15)

- **La gráfica de tráfico de los puertos muestra frames/s cuando el dispositivo no mide bytes** ([#641](https://github.com/gnacho/netpulse/issues/641)). Los beacons de switches RTLPlayground (KP-9000) solo reportan contadores acumulados de tramas, no bytes. El servidor deriva ahora frames/s de esos contadores (raw, 5m y diario) y la tarjeta de Puertos del detalle dibuja fps en lugar de bps para las fuentes sin contadores de bytes, así la gráfica de un puerto de switch gestionado ya no parece vacía.
- **La flota y el detalle muestran el desglose real de clientes por banda** ([#645](https://github.com/gnacho/netpulse/issues/645)). Cada tarjeta de router indica ahora sus clientes online por banda (2.4/5/6 GHz y cable) calculado en el servidor con la misma fuente que el total, y solo pinta las bandas que tienen clientes: un switch sin radios wifi ya no muestra bandas wifi a 0.
- **La línea de tráfico 24h de un switch sin throughput en bps ya no es plana** ([#648](https://github.com/gnacho/netpulse/issues/648)). Para las fuentes que solo reportan tramas por puerto, el mini-gráfico de tráfico de la tarjeta se deriva ahora de los frames/s de sus puertos en lugar de la tabla de bps (que para ellas siempre estaba vacía), así la tarjeta del KP-9000 muestra su actividad real.
- **Los switches beacon RTLPlayground reportan firmware, uptime y MAC reales** ([#639](https://github.com/gnacho/netpulse/issues/639)). El servidor sondea la consola HTTP del switch de los agentes beacon, y la tarjeta del router muestra ahora la versión de firmware real, el tiempo de actividad y la MAC del switch en lugar de marcadores de posición.

### Arreglado

- **El detalle de un switch gestionado ya no pinta sus puertos dos veces** ([#644](https://github.com/gnacho/netpulse/issues/644)). La tarjeta de chasis con LEDs de salud duplicaba a la de Puertos Ethernet con las bocas RJ45; el front panel ha desaparecido y la tarjeta de puertos ya muestra el enlace, la salud y el historial por puerto.
- **Las series de puertos de un switch beacon ya no se ensucian con ceros** ([#642](https://github.com/gnacho/netpulse/issues/642)). Las fuentes externas solo persisten ahora sus contadores reales, manteniendo limpias las series de los puertos.

### Anterior (v2.28.14)

- **El plan de canales ya no cuenta tu propia malla como interferencia ni sugiere un canal que cruce a DFS** ([#631](https://github.com/gnacho/netpulse/issues/631)). En el análisis de canales, los BSSID de tus propios puntos de acceso ya no puntúan como vecinos (se excluyen emparejando el prefijo MAC de tus routers monitorizados), y los canales candidatos se validan contra el ancho de la radio: a 40/80/160 MHz solo se ofrecen bloques que no entran en canales DFS (52-64, 100-144). Evita que NetPulse recomiende un canal que ya usan los satélites de tu malla, o un bloque de 80 MHz que cruzaría a banda DFS.
- **El rearm ahora recupera un agente atascado en bucle de 401** ([#630](https://github.com/gnacho/netpulse/issues/630)). Si un reinicio no trae de vuelta al agente, NetPulse regenera el token del agente y lo aplica en el router (reescribe el `.env` y reinicia) sin reinstalar, así un token desincronizado se arregla en caliente. La rotación de token en un reinstall también es atómica: si el SSH falla, el servidor restaura el token anterior, para que el agente nunca se quede empujando un token que ya no se acepta.

### Anterior (v2.28.13)

- **Una página de Ajustes más limpia, a una columna** ([#627](https://github.com/gnacho/netpulse/issues/627)). La página de Ajustes pasa a una columna única con un índice fijo que te sigue al hacer scroll, así cada tarjeta (Apariencia, Datos y umbrales, Servicios, Red, Integraciones, Administración, Cuenta, Acerca de) ocupa el ancho completo y es más manejable. La tarjeta Apariencia muestra previews reales del tema (claro/oscuro/sistema) y la paleta en dos columnas; el test de velocidad WAN se ejecuta real desde el servidor y rellena descarga/subida en su sitio. Los toggles de Laboratorio aparecen individuales (Orquestación, Canales, Actualizaciones) al activar Labs.
- **Regenerar el token de un agente sin reinstalar** ([#627](https://github.com/gnacho/netpulse/issues/627)). Al regenerar el token de un dispositivo desde Ajustes, NetPulse ahora lo aplica en el propio router por SSH y reinicia el agente, así el dispositivo sigue reportando **sin reinstalar**. El one-liner de instalación solo hace falta cuando el router no es alcanzable por SSH.
- **Actualizaciones: autodetecta la imagen de firmware** ([#629](https://github.com/gnacho/netpulse/issues/629)). En la página de actualizaciones de firmware, NetPulse resuelve la imagen del router a partir de su propio firmware (board + target + version) en el índice de descargas de OpenWrt y **prerrellena** la URL y el checksum, con un botón "Autodetectar imagen". La entrada manual se mantiene cuando no se puede resolver.
- **Desinstalar el agente en routers justos** ([#624](https://github.com/gnacho/netpulse/issues/624)). En routers con poco espacio libre (como el UniFi 6 Lite) no cabía un reinstall; ahora hay una acción "Desinstalar" limpia que detiene y retira el agente.

### Arreglado

- **Los botones de copiar mostraban la clave i18n cruda** ([#628](https://github.com/gnacho/netpulse/issues/628)). Los botones de copiar de la tarjeta de adopción usaban una clave `common.copy` inexistente, así que el tooltip mostraba `common.copy`; la clave ya existe y traduce a "Copy"/"Copiar".

### Anterior (v2.28.12)

- **Puerto SSH personalizado por router** ([#605](https://github.com/gnacho/netpulse/issues/605)). No todos los routers dejan el daemon SSH en el puerto 22: algunos lo tienen en otro. Ahora cada router admite su propio puerto SSH al darlo de alta o editarlo, y todos los caminos del servidor (sondeo, acciones, instalación del agente, descubrimiento del gateway) lo usan automáticamente. Déjalo vacío y NetPulse sigue usando el 22.
- **Matriz de itinerancia agrupada por dispositivo** ([#600](https://github.com/gnacho/netpulse/issues/600)). En WiFi Roaming → Matriz, las columnas se agrupan ahora bajo el nombre del punto de acceso, con una fila de cabecera que etiqueta cada banda (2.4G/5G). Ya no hay una columna suelta por AP-banda.
- **Nombres de 802.11r más claros y sin dispositivos fantasma** ([#601](https://github.com/gnacho/netpulse/issues/601)). El título de la sección pasa a "Por AP" en lugar de "Por router", la columna a "Dispositivo", y los equipos sin radios (como el gateway) ya no aparecen en el detalle por router. Los routers descubiertos automáticamente se nombran por su hostname, no por el modelo de hardware, así la UI muestra nombres de dispositivo en toda la app.
- **Ancho de canal propio en el análisis de canales** ([#602](https://github.com/gnacho/netpulse/issues/602)). En el análisis de canales, la fila de tu propia red muestra ahora el ancho de canal (por ejemplo `1 · 20 MHz`) junto al número de canal, cruzado con el channel-plan del router.
- **La UI ya no se queda en negro al conectarse un cliente WireGuard** ([#592](https://github.com/gnacho/netpulse/issues/592)). NetPulse envía el tipo de peer WireGuard como texto plano y usa "desconocido" cuando no encuentra tipo. La tarjeta de la Home construía el icono sin fallback, así que un peer desconocido (o no mapeado) producía un icono en blanco y React tiraba toda la página en cuanto un cliente WireGuard se conectaba (o al abrir la UI desde él). NetPulse tiene ahora un icono de respaldo para estos casos y un tipo "desconocido" conocido, así la página sigue renderizando.

### Anterior (v2.28.11)

- **El agente ya no degrada tu WiFi al escanear** ([#591](https://github.com/gnacho/netpulse/issues/591)). El agente por router lanzaba un escaneo WiFi activo completo en cada sondeo, es decir cada ~30-37 s por dispositivo. En puntos de acceso eso saca la radio del canal y suelta clientes IoT/débiles. Ahora escanea como mucho cada 10 minutos, tras el arranque, o de inmediato cuando el servidor pide un refresh, y la versión de agente embebida sube para que la flota se actualice sola.

### Qué se descubre

NetPulse descubre dispositivos clientes a partir de tres fuentes que se ejecutan en los routers monitorizados:

1. **Leases DHCP**: en cada push, el agente lee la tabla local de leases DHCP.
2. **FDB del bridge**: los clientes cableados se aprenden de la base de datos de reenvío del switch.
3. **mDNS / Bonjour**: si `umdns` está instalado en OpenWrt, el agente hace browse de los servicios anunciados y resuelve hostnames.

LLDP solo se usa para identificar routers/switches vecinos, no dispositivos finales. El descubrimiento requiere que el agente de NetPulse esté corriendo en el router que ve a los clientes; una instalación nueva con solo routers dados de alta manualmente y sin agente en el gateway mostrará una lista de dispositivos vacía hasta que se instale el agente.

El tráfico por cliente prefiere `nlbwmon` para los clientes por cable (contadores por MAC) y cae a los contadores de bytes de hostapd para los WiFi. `nlbwmon` no viene en todos los builds de OpenWrt y puede ser pesado en memoria en routers con poca RAM; `vnstat` mide totales por interfaz, no tráfico por MAC, así que no es un sustituto. Si un cliente por cable no muestra serie de tráfico, instala `nlbwmon` en ese router (o confía en el fallback WiFi).

## Capturas

**Topología: inferida en vivo del FDB del bridge, túneles incluidos**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-topology-es-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-topology-es-light.png">
  <img alt="Mapa de topología con el gateway en el centro, tres puntos de acceso, clientes cableados e inalámbricos, un switch inferido y el túnel WireGuard a Internet" src="assets/screenshot-topology-es-light.png" width="800">
</picture>

**Dispositivos: cada cliente clasificado, con banda y señal**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-devices-es-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-devices-es-light.png">
  <img alt="Lista de dispositivos con iconos por tipo, hostname, IP, banda, intensidad de señal y el router al que está asociado cada cliente" src="assets/screenshot-devices-es-light.png" width="800">
</picture>

**Routers: salud por router de un vistazo**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-router-es-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-router-es-light.png">
  <img alt="Vista de routers con tarjetas por router mostrando modelo, firmware, CPU, memoria, temperatura y uptime" src="assets/screenshot-router-es-light.png" width="800">
</picture>

## Qué debes esperar

NetPulse es un proyecto personal, construido para mi propia red y
publicado como software libre (AGPL-3.0). Es y será siempre libre. Trabajo
en él en mi tiempo libre: hay muchas ideas para mejoras, pero poco tiempo,
y evoluciona siguiendo primero mis propias necesidades. Con colaboraciones
o apoyo quizás podría crecer más rápido, pero no puedo prometer nada.
**Nota honesta de alcance**: de momento solo se ha probado con mi propio
hardware (un gateway GL.iNet Flint 2 y tres puntos de acceso Xiaomi AX6
con OpenWrt), además de WireGuard y AdGuard Home. Otros dispositivos
OpenWrt deberían funcionar, pero el tuyo sería el primero en contarlo.

## Roadmap

| Fase | Estado | Highlights |
|---|---|---|
| **1 — Topología v5** | ✅ | Mapa semántico real: FDB + LLDP en vivo, backhaul, switches gestionados vs. inferidos, hipervisores con sus CTs anidados, collector de series temporales |
| **2 — Alertas, Push y agente (piloto)** | ✅ | Alertas con 6 categorías y config por categoría, Web Push nativo (VAPID), refresco bajo demanda, agente OpenWrt piloto (ingesta con tokens, fallback SSH, procd) |
| **3 — Base read-only + Node→Go** | ✅ | PWA en React, sondeo SSH de solo lectura (ubus, /proc, iwinfo), AdGuard Home + WireGuard, auth multi-usuario, backend migrado a un binario Go único |
| **4 — View-model + Ajustes remodel** | ✅ | API como view-model de presentación (`vm: 1`), canon demo single-source, topología semántica en el snapshot |
| **5 — Resiliencia del agente** | ✅ | Watchdog + heartbeat en el router, rearme manual desde el servidor, auto-rearme tras TTL, rutas de mutación solo-admin |
| **6 — Seguridad del agente** | ✅ | HMAC-SHA256 en la ingesta del agente, binario del agente servido desde el propio servidor |
| **7 — Agente a fondo** | ✅ | Eventos wifi en tiempo real (`iw event`), SSE bidireccional, paquete `.ipk`, profiling (RSS 11-12 MB, CPU <1%) |
| **8 — Consolidación** | ✅ | Métricas operativas en `/api/health`, registry de agentes persistente, escalera de retención (raw 7d → buckets 5min 1año → daily ∞), recharts v3, webhooks salientes |
| **9 — On-box** | ✅ | Config UCI, bootstrap AUTH_PASS, TLS autofirmado + SPKI pinning, token de pairing, paquete server OpenWrt |
| **10 — Orquestación** | ✅ | Motor plan→apply→state + executor sandboxeado (10.1), módulos AdGuard (10.2), WiFi guest, DDNS, SQM y usteer. WireGuard aplazado a la Fase 17 |
| **11 — Paquete LuCI** | ✅ | `luci-app-netpulse`: estado local del agente (procd, UCI, logs, restart/rearm) + test connection + puente a la webapp |
| **12 — Auditoría de seguridad** | ✅ | TRUST_PROXY, anti-replay en ingesta, body cap, password mínima 10 |
| **13 — Auditoría de robustez** | ✅ | Single-flight GetOverview, SSE write deadline, race en sshpool.dial, %w wrapping |
| **14 — Visibilidad WiFi/roaming** | ✅ | Matriz de señal DAWN, estado 802.11r por SSID, utilización por canal survey, feed persistente de eventos de roaming (30 días). Polish pendiente: claridad de la gráfica del análisis de canales ([#618](https://github.com/gnacho/netpulse/issues/618)) |
| **15 — Informes** | 🔄 | Disponibilidad día/semana/mes. Pendiente: tráfico, actividad, resumen de alertas, exportación, e insights de red por cliente/VLAN ([#588](https://github.com/gnacho/netpulse/issues/588)) |
| **16 — Alertas avanzadas** | 🔮 | Reglas custom por umbral, tipos nuevos (fallo de roaming, congestión de canal), silencio programado, email |
| **17 — Escribir en routers** | 🔄 | Ownership UCI + apply seguro con rollback (#451), planificación de canales (#452), firmware upgrades (#453); índice completo de módulos (AdGuard full, WiFi guest, DDNS, QoS, WireGuard, OpenVPN, Tailscale, Batman, DPI) |
| **18-20 — Programa de beta-testing** | 🔮 | Grupos de módulos por riesgo (bajo / medio / alto) con canales stable + unstable y beta-testers externos |

Detalle completo en [docs/ROADMAP.md](docs/ROADMAP.md).

## Instalación

Requisitos: Linux (x86_64, arm64 o armv7) con systemd.

```bash
curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install.sh | sh   # (recomendado)
```

El instalador es shell plano y legible: [inspecciónalo primero](install.sh).
Detecta tu distro y arquitectura, descarga la release verificada (sha256
contra `checksums.txt`), crea un servicio systemd `netpulse` enjaulado y
muestra la contraseña inicial de admin una sola vez. Actualiza
re-ejecutando la misma línea; desinstala con `sh install.sh --uninstall`.

Sidecar de latencia opcional (series temporales de sondeo TCP por router):

```bash
curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install-collector.sh | sh
```

Los binarios estables se publican por tag `v*` (goreleaser); los builds
rolling por commit viven en la prerelease `go-latest` para el updater
in-app.

## Conectar tus routers

El servidor genera su propio par de claves ed25519 y muestra la clave
pública en Ajustes. Autorízala en cada router que quieras monitorizar
(`/etc/dropbear/authorized_keys`). El gateway se autodetecta en el primer
arranque por descubrimiento LAN (barrido TCP :22, fingerprint ubus/GL-UI);
el resto se añade desde Ajustes. El sondeo es estrictamente de solo
lectura.

## Desarrollo

```bash
# Backend (Go; sirve app/dist vía go:embed)
cd server-go
cp ../app/dist internal/staticspa/dist -r   # el dist embebido nunca se trackea
go build -o netpulse ./cmd/netpulse && DEMO_MODE=1 ./netpulse

# Frontend (dev server con proxy)
cd app
npm install
npm run dev
```

El backend Node legado fue eliminado (decisión 5-Ago-2026: no se despliega ni se
actualiza); su historial git se conserva. La migración desde su base de datos
ocurre automáticamente en el primer arranque Go.

## Tests

```bash
cd server-go && go test ./...
```

## Gracias enormes

NetPulse no existiría sin [OpenWrt](https://openwrt.org/). Toda la premisa
del proyecto, que el hardware de tu red sea realmente tuyo y no dependa del
firmware cerrado de un fabricante, solo es posible porque OpenWrt existe. Si
NetPulse te sirve, el mérito real es de la comunidad de OpenWrt.

## Licencia

[AGPL-3.0](LICENSE)
