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

<p align="center">
  <strong>El pulso de tu red doméstica. En tiempo real. Sin nube.</strong><br>
  Vigila tus routers, dibuja la topología, puntúa la salud de la red y recibe
  alertas, desde un único binario autoalojado. Nada sale de tu LAN.
</p>

<p align="center">
  <a href="https://demo.netpulse.cloudless.club"><strong>Prueba la demo en vivo</strong></a> ·
  <a href="https://netpulse.cloudless.club/features"><strong>Tour completo de funciones</strong></a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/hero-es-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/hero-es-light.png">
    <img alt="Resumen de NetPulse con puntuación de salud de red, gráfico de tráfico en vivo, estadísticas de AdGuard Home, peers WireGuard y el feed de alertas" src="assets/hero-es-light.png" width="800">
  </picture>
</p>

## Míralo antes de instalar nada

No necesitas un solo router para ver NetPulse funcionando:

- **[demo.netpulse.cloudless.club](https://demo.netpulse.cloudless.club)** es la app
  real con una red de ejemplo completa cargada, en modo de solo lectura y sin
  registro. Navega, cambia el tema, cambia de idioma, mira las
  actualizaciones en vivo.
- **[netpulse.cloudless.club/features](https://netpulse.cloudless.club/features)**
  recorre cada pantalla y cada función, con las decisiones técnicas y el
  alcance honesto de cada una.

## ¿Por qué NetPulse?

Si usas OpenWrt, tu red ya es tuya. Pero que sea tuya implica *conocerla*:
qué se conecta dónde, qué va bien, qué cambió anoche. LuCI te muestra un
router cada vez; las suites NMS tipo Zabbix o LibreNMS están pensadas para
un datacenter. Lo que faltaba es lo intermedio: un NOC doméstico que se
instala en minutos y simplemente te enseña tu red.

NetPulse es esa pieza. Nació de mi propia red: un GL.iNet Flint 2 como
gateway y tres puntos de acceso Xiaomi AX6 de segunda mano, todos con
OpenWrt, cada uno con su LuCI. Quería una sola pantalla que me dijera la
verdad sobre el conjunto, así que la construí y la uso cada mañana.

Tres reglas dan forma a todo lo que hace:

- **Solo lectura por diseño.** El servidor genera su propio par de claves
  SSH; autorizas la pública en cada router y NetPulse solo *lee* (ubus,
  `/proc`, iwinfo, `bridge fdb`, `wg show`). No puede cambiar tu red.
- **Sin nube, sin cuentas, sin telemetría.** Un único binario Go estático con
  la web embebida, corriendo en una caja pequeña dentro de tu LAN. SQLite
  para las series temporales, modo WAL, sin servicios externos.
- **Libre de verdad, para siempre.** AGPL-3.0, sin versión premium esperando
  detrás de un pago. Si te sirve, [Ko-fi](https://ko-fi.com/gnacho) es la
  forma de dar las gracias, nunca una suscripción.

## Qué te llevas

**Resumen de flota y una puntuación de salud que significa algo.** Un único
valor 0-100 condensa latencia, pérdidas, uptime y temperatura por router y
para toda la red, actualizado en vivo cada 5 segundos por SSE. Tráfico WAN
en vivo contra tu plan contratado, feed de alertas, estadísticas de AdGuard
Home y peers WireGuard en la misma pantalla (es la captura de arriba).

**Un mapa de topología que se dibuja solo.** Inferido en vivo del FDB del
bridge (y de LLDP donde está disponible): clientes cableados e inalámbricos
bajo el router y el puerto por los que hablan de verdad, switches
gestionados e inferidos, hosts de Proxmox con sus VMs anidadas y túneles
WireGuard trazados del peer a Internet. Los dispositivos callados conservan
su sitio; nada salta al refrescar. Arrastra los nodos a tu gusto y el layout
queda congelado para ti.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-topology-es-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-topology-es-light.png">
    <img alt="Mapa de topología con el gateway en el centro, tres puntos de acceso, clientes cableados e inalámbricos, un switch inferido y el túnel WireGuard a Internet" src="assets/screenshot-topology-es-light.png" width="800">
  </picture>
</p>

**Cada dispositivo, clasificado.** Hostnames, IPs, detección de tipo
(patrones de hostname + OUI), primer visto, banda y señal para clientes Wi-Fi,
y el router al que está asociado cada uno. Etiqueta dispositivos, reserva IPs
y entérate cuando aparece algo nuevo.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-devices-es-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-devices-es-light.png">
    <img alt="Lista de dispositivos con iconos por tipo, hostname, IP, banda, intensidad de señal y el router al que está asociado cada cliente" src="assets/screenshot-devices-es-light.png" width="800">
  </picture>
</p>

**Salud por router, de un vistazo.** Modelo, firmware, CPU, memoria,
temperatura, uptime y tráfico en vivo de cada router, con historial por
puerto y desglose de clientes por banda. Los switches gestionados sondeados
por SNMP también son ciudadanos de primera.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot-router-es-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/screenshot-router-es-light.png">
    <img alt="Vista de routers con tarjetas por router mostrando modelo, firmware, CPU, memoria, temperatura y uptime" src="assets/screenshot-router-es-light.png" width="800">
  </picture>
</p>

**Y el resto del tour:**

- **WireGuard**: peers con sus nombres reales, últimos handshakes,
  transferencia por peer.
- **AdGuard Home**: estadísticas de consultas, porcentaje bloqueado, dominios
  más bloqueados.
- **Wi-Fi y roaming**: matriz de señal por AP, estado 802.11r, utilización
  por canal, eventos de roaming persistentes.
- **Alertas que te encuentran**: temperatura, dispositivo nuevo, firmware
  disponible, WAN caída, problemas de agente; feed en la campana más Web Push
  nativo en el móvil.
- **Multiusuario**: contraseñas bcrypt, roles admin y viewer, idioma por
  usuario (ES/EN).
- **PWA instalable**: móvil o escritorio, en vivo por SSE, temas claro/oscuro.
- **Ciudadano OpenWrt de primera**: paquetes nativos `netpulse-agent`
  (`.ipk`/`.apk`), página `luci-app-netpulse`, config UCI, init procd y
  watchdog. O el servidor entero en modo on-box.
- **Se actualiza solo**: el updater integrado comprueba releases y las aplica
  con swap atómico.

Hay más, y cada pieza tiene su historia: **[el tour completo de funciones
vive en la web](https://netpulse.cloudless.club/features)**, y todo lo
anterior se puede tocar en la **[demo en vivo](https://demo.netpulse.cloudless.club)**.

## Cómo funciona el descubrimiento

NetPulse descubre los dispositivos cliente a partir de tres fuentes en los
routers monitorizados: leases DHCP, el FDB del bridge (clientes cableados) y
mDNS cuando `umdns` está instalado. LLDP identifica routers y switches
vecinos. El tráfico por cliente prefiere `nlbwmon` (contadores por MAC,
cable) y cae a los contadores de bytes de hostapd (Wi-Fi); si un cliente
cableado no muestra serie de tráfico, instala `nlbwmon` en ese router.
`vnstat` mide totales por interfaz, no por cliente, así que no es sustituto.

## Empieza en unos cinco minutos

Necesitas una caja Linux con systemd (x86_64, arm64 o armv7; un LXC o VM
pequeña sobra) y routers OpenWrt/GL.iNet en tu LAN.

**1. Instala el servidor** en la caja:

```bash
curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install.sh | sh
```

El instalador es shell plano y legible ([inspecciónalo primero](install.sh)):
detecta distro y arquitectura, descarga la release verificada contra
`checksums.txt`, crea un servicio systemd `netpulse` enjaulado y muestra la
contraseña inicial de admin una sola vez. Re-ejecutar la misma línea
actualiza; `sh install.sh --uninstall` desinstala.

**2. Abre la app** en `http://<ip-del-servidor>:3000`, entra con la
contraseña impresa y el gateway ya está ahí: NetPulse lo encuentra en el
primer arranque por descubrimiento LAN (barrido TCP :22 con fingerprinting
ubus/GL-UI).

**3. Autoriza la clave SSH.** El servidor generó su propio par de claves en
el primer arranque. Copia la clave pública desde **Ajustes → Red** y, en cada
router que quieras monitorizar, añádela:

```sh
echo "<clave pública de Ajustes>" >> /etc/dropbear/authorized_keys
```

**4. Instala los agentes desde la app.** Abre **Routers** y baja hasta la
tabla de agentes: cada router OpenWrt tiene su fila, y los que no tienen
agente ofrecen el botón **Instalar agente**. Un clic y el servidor registra
el agente, empuja el binario correcto para la arquitectura del router,
escribe la config y arranca el servicio; aparece como conectado en segundos.
Los routers a los que el servidor no llega por SSH usan el token de
emparejamiento (**Ajustes → Adopción de agentes**, mira la sección plegada
de abajo).

**5. Llévatela contigo.** Instala la PWA desde el navegador (Añadir a
pantalla de inicio / Instalar app) y activa Web Push en Ajustes: las alertas
llegan al móvil aunque la pestaña esté cerrada.

¿Prefieres verla primero en tu propio hardware? `DEMO_MODE=1 ./netpulse`
levanta la misma app con una red de muestra de 67 dispositivos, sin routers.

<details>
<summary><strong>Otras vías de instalación</strong></summary>

**Paquetes OpenWrt.** Cada release publica `netpulse-agent` y
`luci-app-netpulse` como paquetes instalables (`.ipk` para OpenWrt 24.10,
`.apk` para 25.12, más x86/64). Bájalos de la
[última release](https://github.com/gnacho/netpulse/releases):

```sh
# OpenWrt 24.10 (ipk)
opkg install ./netpulse-agent_*.ipk ./luci-app-netpulse_*.ipk

# OpenWrt 25.12 (apk)
apk add --allow-untrusted ./netpulse-agent-*.apk ./luci-app-netpulse-*.apk
```

El paquete trae una config vacía, así que apúntalo a tu servidor antes de
arrancar:

```sh
uci set netpulse-agent.main.server='http://<ip-del-servidor-netpulse>:3000'
uci set netpulse-agent.main.slug='<slug>'
uci set netpulse-agent.main.token='<token hex de 64>'
uci commit netpulse-agent
service netpulse-agent enable && service netpulse-agent start
```

Si el servidor ya puede hacer SSH al router, sáltate todo esto y pulsa
**Instalar agente** en la app: hace cada paso por ti.

**Token de emparejamiento.** Para routers a los que el servidor no llega por
SSH, Ajustes → Adopción de agentes muestra un token reutilizable:
`install-agent.sh --pairing-token` (ejecutado desde cualquier máquina que
alcance el router y el servidor) registra el agente en el primer contacto.

**Sidecar de latencia.** Sondeos TCP de latencia por router a largo plazo,
opcionales:

```bash
curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install-collector.sh | sh
```

**Modo on-box.** ¿Sin caja aparte? El servidor corre en el propio router
OpenWrt, con config UCI y TLS autofirmado con pinning SPKI.

</details>

## Actualizaciones e historial de versiones

Las releases van firmadas, con checksums, y llegan a tu flota por el
actualizador integrado; la app muestra un panel breve y legible de "qué ha
cambiado" antes de actualizar. El historial completo vive en
[CHANGELOG.md](CHANGELOG.md) y en la
[página de releases](https://github.com/gnacho/netpulse/releases); el plan
que viene, en [docs/ROADMAP.md](docs/ROADMAP.md).

## Qué debes esperar

NetPulse es un proyecto personal, construido para mi propia red y publicado
como software libre (AGPL-3.0). Es y será siempre libre. Trabajo en él en mi
tiempo libre y evoluciona siguiendo primero mis propias necesidades; con
colaboraciones o apoyo quizás crezca más rápido, pero no puedo prometer
nada. **Nota honesta de alcance**: de momento solo se ha probado con mi
propio hardware (un gateway GL.iNet Flint 2 y tres puntos de acceso Xiaomi
AX6 con OpenWrt), además de WireGuard y AdGuard Home. Otros dispositivos
OpenWrt deberían funcionar, pero el tuyo sería el primero en contarlo.

## Documentación

- **[Manual de usuario](docs/manual.es.md)** (también en
  [inglés](docs/manual.en.md)): cada pantalla explicada, menú a menú, con
  procedimientos paso a paso.
- **[Web del proyecto](https://netpulse.cloudless.club)**: el proyecto en
  cinco minutos, con [tour de funciones](https://netpulse.cloudless.club/features),
  FAQ y capturas.
- **[Hoja de ruta](docs/ROADMAP.md)**: qué está hecho y qué viene.
- **[Discusiones](https://github.com/gnacho/netpulse/discussions)**:
  preguntas, ideas y hablar del futuro.

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

# Tests
cd server-go && go test ./...
```

## Gracias enormes

NetPulse no existiría sin [OpenWrt](https://openwrt.org/). Toda la premisa
del proyecto, que el hardware de tu red sea realmente tuyo y no dependa del
firmware cerrado de un fabricante, solo es posible porque OpenWrt existe. Si
NetPulse te sirve, el mérito real es de la comunidad de OpenWrt.

## Licencia

[AGPL-3.0](LICENSE)
