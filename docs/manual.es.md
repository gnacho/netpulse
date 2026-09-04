# Manual de uso de NetPulse

NetPulse es una PWA (aplicación web progresiva) de solo lectura para monitorizar una red doméstica montada sobre routers OpenWrt/GL.iNet: estado de la flota, salud por router, clientes conectados, un mapa de topología en vivo, estado de itinerancia WiFi, alertas y, en modo experimental, algunas acciones de escritura (cambiar canal WiFi, actualizar firmware y orquestar servicios). Corre como un único binario Go autocontenido, con el frontend embebido, y se sirve desde una máquina Linux pequeña.

Este manual recorre todas las áreas de la aplicación: qué muestra cada pantalla, de dónde salen los datos (sondeo SSH de solo lectura, agentes, usteer, NetGrip, etc.), cómo leer cada número o color, las decisiones de diseño que conviene entender y los procedimientos paso a paso de las tareas habituales. Al final hay una sección que aclara qué vive en NetPulse y qué sigue viviendo en el panel del router (LuCI o NetGrip).

## Áreas de un vistazo

- [Resumen](#resumen)
- [Dispositivos (la flota de routers)](#dispositivos-la-flota-de-routers)
- [Detalle del dispositivo](#detalle-del-dispositivo)
- [Clientes](#clientes)
- [Topología](#topología)
- [Alertas](#alertas)
- [Itinerancia WiFi](#itinerancia-wifi)
- [Plan de canales](#plan-de-canales)
- [Actualizaciones de firmware](#actualizaciones-de-firmware)
- [Informes](#informes)
- [Orquestación](#orquestación)
- [Ayuda](#ayuda)
- [Ajustes](#ajustes)
- [Qué vive en NetPulse y qué vive en el panel del router](#qué-vive-en-netpulse-y-qué-vive-en-el-panel-del-router)

> **Aviso sobre los nombres.** La interfaz distingue dos listas que se parecen. La página de la flota (ruta `/routers`) se llama **Dispositivos** (en inglés, *Devices*) y muestra los routers y puntos de acceso. La página de clientes (ruta `/devices`) se llama **Clientes** (en inglés, *Clients*) y muestra los equipos que se conectan a la red (móviles, ordenadores, TV, cámaras...). En este manual se usan los nombres tal como aparecen en la aplicación.

---

## Primer acceso

NetPulse es multiusuario con autenticación por contraseña. El instalador imprime una contraseña inicial de administrador una sola vez; úsala para entrar por primera vez.

1. Abre la dirección de tu servidor NetPulse en el navegador.
2. Introduce el usuario y la contraseña. Si fallas varias veces, el acceso se bloquea temporalmente (cuenta atrás de unos segundos).
3. Al entrar aterrizas en el **Resumen**.
4. En **Ajustes > Mi perfil** puedes cambiar tu contraseña (mínimo 10 caracteres), editar el nombre que te saluda en el Resumen y elegir idioma (español o inglés).

Existe además un **modo demo** que se puede abrir sin contraseña ni backend: muestra una red de ejemplo con decenas de dispositivos para explorar la interfaz. En demo no hay datos reales y las funciones de escritura quedan deshabilitadas.

---

## Resumen

**Ruta:** `/`. Es la página inicial tras entrar.

### Qué muestra

- **Saludo y estado**: un saludo por franja horaria con tu nombre, y una línea que resume si hay alertas importantes.
- **Anillo de salud de la red**: una puntuación de 0 a 100 con una etiqueta cualitativa (Excelente, Bueno, Atención, Crítico).
- **Subpuntuaciones**: barras pequeñas por dimensión (por ejemplo cobertura, temperatura) que alimentan la nota global.
- **Desglose de penalizaciones**: una lista de factores concretos que restan puntos y cuánto restan.
- **Latencia** al gateway (en ms) y **total de clientes**.
- **Tráfico WAN** en vivo (descarga y subida).
- **Tarjetas de servicios**: AdGuard Home y WireGuard (solo si los has activado en Ajustes).
- **Alertas recientes** y una **fila de routers**.
- En vivo (no demo): gráficas del **collector** (latencia TCP por router) y de **SLE WiFi** (Service Level Expectations).
- En demo: **Top dispositivos** por tráfico.

### Qué datos necesita

El Resumen es un agregado de todo lo que el servidor ya ha recogido: la salud se calcula con las métricas de los routers (temperatura, señal de clientes débiles, latencia WAN), el tráfico WAN sale del gateway y los servicios (AdGuard, WireGuard) se leen del gateway en solo lectura. En modo demo los valores vienen de un dataset simulado.

### Cómo leerlo

- El **anillo** es la nota global: cuanto más cerca de 100, mejor. La etiqueta (Excelente, Bueno, Atención, Crítico) es el resumen rápido.
- Las **subpuntuaciones** usan los mismos colores que el resto de la app: verde (>= 90), ámbar (>= 70), rojo (por debajo). Te dicen qué área tira de la nota.
- El **desglose** te dice exactamente qué está penalizando (por ejemplo "Temperatura alta en el router del patio") y, con un número, el peso de cada penalización. Con esto sabes qué arreglar primero.
- La **latencia** y el **tráfico WAN** son métricas en vivo: cambian con el uso real de la red.

### Decisiones de diseño

- La salud no es un número mágico: es una nota calculada a partir de umbrales que tú puedes ajustar en **Ajustes > Datos y umbrales** (temperatura 65 °C por defecto, señal -70 dBm, latencia 50 ms). Cambiar un umbral cambia la nota.
- El anillo se actualiza en tiempo real por SSE; el servidor sondea los routers cada 5 segundos y empuja una instantánea a todos los clientes conectados.
- En demo no hay datos reales y las tarjetas de collector/SLE no aparecen (dependen del backend).

### Procedimiento: leer la salud de la red

1. Abre **Resumen**.
2. Mira el anillo: la nota global y su etiqueta.
3. Si no es Excelente, mira las **subpuntuaciones** para localizar la dimensión afectada.
4. Abre el **desglose** de penalizaciones y anota el factor con más peso.
5. Ve a la pantalla concreta (por ejemplo **Dispositivos** si es temperatura, o **Clientes** si es señal débil) para actuar.

---

## Dispositivos (la flota de routers)

**Ruta:** `/routers`. Título en la interfaz: **Dispositivos** (en inglés, *Devices*).

### Qué muestra

- **Cabecera** con el resumen del estado ("N equipos, N online, N con aviso"), un punto de color por router, botón de refrescar y, si hay agentes desactualizados, un botón para actualizarlos todos.
- **Tarjetas por router** (2 por fila): modelo, firmware, estado, CPU, RAM, temperatura, uptime, tráfico y latencia.
- **Tabla comparativa** de todos los routers.
- **Sección de agentes** al final: por cada router, si tiene agente, de qué tipo (nativo NetPulse, NetGrip o externo), su versión y su estado, con botones para instalar, reinstalar o actualizar el agente.

### Qué datos necesita

Los routers se dan de alta en **Ajustes > Dispositivos** (ver el procedimiento de alta más abajo). El servidor los sondea por SSH en solo lectura (ubus, `/proc`, iwinfo) cada 5 segundos; los routers con agente empujan además datos cada 15 segundos. Un router sin agente y sin SSH accesible no aporta métricas de CPU/RAM/temperatura.

### Cómo leerlo

- El **punto de color** de cada router: verde = online, ámbar = con aviso (por ejemplo temperatura alta), rojo = caído.
- La **tarjeta** resume las métricas clave; el icono de aviso aparece cuando la temperatura supera el umbral.
- En la **sección de agentes**, "Agente caído" significa que el agente estaba registrado pero dejó de reportar (suele resolverse reinstalando desde el detalle del router).
- "Sin métricas de sistema" indica que ese dispositivo se monitoriza por SNMP o por beacon y no reporta CPU/RAM/temperatura.

### Decisiones de diseño

- **El sondeo es de solo lectura.** El servidor genera su propia clave ed25519; tú autorizas su clave pública en cada router y este solo lee. No puede cambiar tu red.
- **Instalar el agente es opcional pero recomendado.** Sin agente en el gateway, la lista de clientes estará vacía hasta que lo instales, porque el descubrimiento de clientes (DHCP, bridge FDB, mDNS) lo hace el agente del router que "ve" a esos clientes.
- **El gateway se auto-detecta** en el primer arranque por descubrimiento de red (barrido TCP :22 y huella ubus/GL-UI); el resto se añaden a mano.
- Hay routers cuyo agente vive dentro del **panel NetGrip** (corre en el puerto 8080 del router): en ese caso "Actualizar" actualiza el propio panel, no un binario.

### Procedimiento: dar de alta un router

1. Ve a **Ajustes > Dispositivos** (visible solo para administradores).
2. Pulsa **Descubrir en la red** para que el servidor barra la red local y localice equipos, o usa el formulario de alta manual.
3. Si es el gateway, márcalo como **Gateway principal**. Elige el tipo (router, switch gestionado o externo).
4. Copia la **clave pública SSH** del servidor (se muestra en la misma sección) y autorízala en el router (`/etc/dropbear/authorized_keys`). Sin esto, el servidor no puede sondearlo.
5. Guarda. El router aparece en la flota y empieza a reportar en cuanto el sondeo lo alcance.

### Procedimiento: instalar el agente en un router

1. Con la clave SSH del servidor ya autorizada en el router, abre **Dispositivos**.
2. Baja a la **sección de agentes**.
3. En la fila del router, pulsa **Instalar agente** (o **Reinstalar** si ya había uno).
4. El servidor registra el agente (crea su token al vuelo), detecta la arquitectura, descarga el binario desde el propio servidor, verifica su SHA256, escribe la configuración, instala el arranque y reinicia el servicio.
5. El agente aparece como conectado en unos segundos.

Para routers a los que el servidor no puede acceder por SSH, usa el **token de emparejamiento** (Ajustes > Adopción de agentes) con `install-agent.sh --pairing-token`.

### Procedimiento: actualizar todos los agentes

1. En **Dispositivos**, si hay agentes desactualizados verás un aviso con el número.
2. Pulsa **Actualizar agentes** y confirma.
3. Sigue el panel de progreso: por cada agente verás el estado (esperando, descargando, instalando, actualizado o sin respuesta). Un agente que no responde en unos segundos requiere reinstalación manual.

---

## Detalle del dispositivo

**Ruta:** `/routers/:id`. Título en la interfaz: **Detalle del dispositivo**.

### Qué muestra

Al pulsar un router en la flota se abre su detalle, que cambia según el rol:

- **Cabecera**: nombre, modelo, rol (Gateway principal o AP), estado, uptime y, si procede, aviso de temperatura alta con el tiempo transcurrido.
- **Rendimiento**: gráficas de CPU, RAM y temperatura en el tiempo (si la fuente puede darlas; si no, un marcador de posición explica que no hay vitals).
- **Información**: firmware, modelo corto, enlace al panel del router (NetGrip o LuCI) cuando aplica.
- Para el **gateway**: latencia WAN, paneles de AdGuard Home y WireGuard, y los **puertos** físicos.
- Para los **APs**: panel de **backhaul** (enlace de retorno hacia el gateway) y **radios + puertos**.
- **VLANs** del bridge (solo lectura) si existen.
- **Clientes de este router**: los equipos conectados a él.

### Qué datos necesita

El detalle se alimenta del sondeo SSH de solo lectura y de los extras que devuelve el backend (radios, bocas LAN con su dispositivo, VLANs). Los paneles de AdGuard y WireGuard leen el gateway; el panel de backhaul solo aparece en routers que no son el gateway.

### Cómo leerlo

- El **banner de temperatura** aparece cuando el router está en estado "aviso" y la métrica caliente es la temperatura; incluye un consejo (ventilación).
- Las **gráficas de rendimiento** son históricas y permiten ver si un pico de CPU o de temperatura es puntual o sostenido.
- Los **puertos** muestran qué hay conectado en cada boca LAN (y enlazan a la ficha del cliente en **Clientes**).
- Los paneles de **AdGuard/WireGuard** son la vista de solo lectura de lo que se configura en el panel del router (ver la sección final).

### Decisiones de diseño

- La plantilla es distinta para el gateway y para los APs porque sus responsabilidades son distintas: el gateway concentra WAN, AdGuard y WireGuard; los APs muestran backhaul y radios.
- El detalle **no se vacía al refrescar** para evitar parpadeo en cada instantánea SSE; solo se sustituye si algo cambió.

---

## Clientes

**Ruta:** `/devices`. Título en la interfaz: **Clientes** (en inglés, *Clients*).

### Qué muestra

- **Franja de estadísticas**: online ahora, nuevos en los últimos 7 días, clientes con señal débil (< -70 dBm) y clientes protegidos por AdGuard.
- **Barra de filtros**: por router, por banda (2.4 GHz / 5 GHz / cable), por tipo de dispositivo (multiselección) y un interruptor "Solo online".
- **Lista o cuadrícula** de clientes con tipo clasificado, nombre, fabricante, IP/MAC, tiempo restante de concesión DHCP, router al que están conectados, banda y señal.
- Al expandir un cliente: MAC, concesión DHCP, primer visto, fabricante, hostname, tráfico 24 h, tráfico actual con minigráfica, estado AdGuard y acción de renombrar (cambiar icono/nombre local).

### Qué datos necesita

Los clientes se descubren desde tres fuentes que corren en los routers monitorizados: (1) la tabla de concesiones DHCP, (2) el bridge FDB para clientes por cable y (3) mDNS/Bonjour cuando `umdns` está instalado. La clasificación por tipo usa patrones de hostname y OUI del fabricante. El descubrimiento exige que el agente corra en el router que ve a los clientes; sin agente en el gateway, la lista estará vacía.

### Cómo leerlo

- La **señal** sigue la escala habitual: verde por encima de -55 dBm, cian entre -55 y -70, ámbar por debajo de -70 (se considera débil).
- **"Nuevo"** marca a los vistos por primera vez en los últimos 7 días; **"Offline"** atenuado marca a los conocidos pero desconectados.
- Las **concesiones DHCP** pueden ser "IP fija (reserva)", "renueva en Xh Ymin" o "Expirada".
- Los **badges de infraestructura** (Hipervisor, CT, Switch gestionado) identifican equipos de red detectados vía LLDP o sellados por el servidor.

### Decisiones de diseño

- La vista por defecto ordena online primero (por tráfico) y deja los offline al final.
- NetPulse es de solo lectura por diseño, con excepciones opt-in para administradores: la ficha de un cliente permite **reservar su IP** (lease estático en el DHCP del gateway) y **bloquear/desbloquear su acceso** (regla de firewall en el router al que está conectado). Son las únicas escrituras sobre el dispositivo; el resto de la configuración del router se hace en su panel.
- También permite **marcar un MAC como de confianza** en Ajustes para que deje de avisar como "desconocido" y **renombrar/recambiar el icono** de un cliente.

### Procedimiento: renombrar o cambiar el icono de un cliente

1. En **Clientes**, localiza el equipo (busca por nombre, IP o MAC con la caja de búsqueda).
2. Expande su ficha y pulsa **Editar** (o el icono de lápiz en la fila).
3. Elige otro icono o nombre y guarda. En vivo el cambio se guarda en el servidor; en demo, solo en tu navegador.

### Procedimiento: marcar un dispositivo como de confianza

1. Ve a **Ajustes > Dispositivos de confianza** (solo administradores).
2. Añade la MAC del equipo. Dejará de avisar como "desconocido" y su nombre se usará como alias.

### Procedimiento: reservar una IP (lease DHCP estático)

Disponible solo para administradores y en modo live. La reserva se escribe en el DHCP del gateway (el router que sirve las concesiones), no en el router al que el cliente está conectado.

1. En **Clientes**, expande la ficha del equipo y ábrela en modo edición.
2. En **Reservar IP**, marca **Usar IP actual** o escribe otra IP (debe estar libre: si otra MAC ya la tiene reservada, el servidor lo rechaza con un aviso de conflicto).
3. Pulsa **Guardar IP**. La ficha pasa a mostrar **Reservado como \<IP\>**.
4. Para liberarla, borra el contenido de la IP y guarda (o elimina la reserva desde el mismo campo).

Notas: es idempotente (reservar la misma IP dos veces no duplica), y si el reinicio de dnsmasq falla tras escribir, el servidor revierte el cambio para no dejar la configuración a medias.

### Procedimiento: bloquear o desbloquear el acceso de un dispositivo

Disponible solo para administradores. **Bloquear** añade una regla de firewall (DROP) para la MAC del equipo en el router al que está conectado, de modo que pierde el acceso a la red; el bloqueo no afecta a su concesión DHCP.

1. En **Clientes**, expande la ficha del equipo y ábrela en modo edición.
2. En **Bloquear dispositivo**, pulsa **Bloquear**. La ficha muestra el estado **Bloqueado**.
3. Para revertirlo, pulsa de nuevo (desbloquear). El estado se consulta en vivo, así que si bloqueas el equipo desde el panel del router, NetPulse lo refleja.

---

## Topología

**Ruta:** `/topology`. Título en la interfaz: **Topología**.

### Qué muestra

Un mapa SVG interactivo (con pan y zoom) de la red, inferido en vivo: el gateway en el centro, los puntos de acceso, los clientes por cable e inalámbricos, los switches (gestionados o inferidos), los hipervisores con sus contenedores y los túneles WireGuard dibujados hacia Internet. Debajo, una **leyenda** y una **tabla de enlaces**.

### Qué datos necesita

El mapa se construye con el bridge FDB (para clientes por cable), LLDP cuando está disponible (para identificar routers y switches vecinos), los datos de WireGuard del gateway y las señales WiFi. Los clientes inalámbricos se asocian al AP que los ve.

### Cómo leerlo

- Cada nodo tiene forma y color según su papel: routers, clientes, switches, hipervisores, contenedores y túneles tienen representaciones distintas (la leyenda lo aclara).
- Las **líneas** son los enlaces (cable, WiFi o túnel VPN); al pasar el ratón sobre un enlace, la tabla de enlaces resalta la fila correspondiente.
- Un cliente con **señal débil** se marca con el color de aviso.
- Los **túneles WireGuard** se dibujan desde el peer hacia Internet, con su IP.

### Decisiones de diseño

- La topología es **inferida**, no configurada: se deduce de las tablas del router, por eso a veces aparece un switch "inferido" que no has dado de alta.
- LLDP solo identifica routers y switches vecinos, no a los clientes finales.
- El botón **Refrescar** fuerza un sondeo inmediato en el backend (POST /api/refresh) y el mapa se actualiza con la instantánea que llega por SSE, sin recargar la página.

### Procedimiento: usar el mapa de topología

1. Abre **Topología**.
2. Usa los controles de zoom (**Acercar / Alejar / Centrar**) y arrastra para mover el lienzo.
3. Activa/desactiva **Etiquetas** (nombres) y **Flujo** (animación de paquetes) según necesites.
4. Pasa el ratón sobre un nodo o enlace para ver el detalle; pulsa para abrir la ficha.
5. Usa la **tabla de enlaces** para listar todas las conexiones y localizar una concreta.
6. (Solo administradores, en vivo) Pulsa **Etiquetar dispositivos** para marcar un equipo como hipervisor/switch o asignarlo a un host.

---

## Alertas

**Ruta:** `/alerts`. Título en la interfaz: **Alertas**.

### Qué muestra

- **Resumen**: tres contadores (avisos sin leer, alertas de hoy, críticos) con un desglose por causa.
- **Configuración de alertas**: por categoría, el nivel deseado (Solo urgentes / Todo / Nada) y un gestor de reglas personalizadas.
- **Filtros**: por severidad, por tipo, por categoría y "Solo no leídas".
- **Feed en línea de tiempo** agrupado por día, con severidad por color y contexto expandible (minigráfica con umbral, datos del peer WireGuard o del dispositivo afectado).

### Qué datos necesita

Las alertas las genera el servidor a partir de lo que ya monitoriza: temperatura, firmware disponible, dispositivos nuevos, handshakes de WireGuard, caídas de WAN, señal débil, etc. Hay seis categorías (Router, Internet, Clientes, Señal, VPN, Sistema) y cuatro severidades (aviso, crítico, informativa, resuelta). En modo demo el feed es un mockup enriquecido; en vivo solo se muestran alertas reales.

### Cómo leerlo

- El **color de la franja izquierda** y el icono indican la severidad: ámbar = aviso, rojo = crítico, azul = informativa, verde = resuelta.
- El **punto de color** marca las no leídas. El estado "leído/no leído" lo guarda el servidor (fuente de verdad).
- Al expandir una alerta ves su **contexto**: una minigráfica con la línea de umbral (para métricas), o los datos del peer/dispositivo implicado, y un enlace directo al router afectado.
- El badge **"urgente"** marca alertas que requieren atención inmediata.

### Decisiones de diseño

- La configuración es **por categoría y por nivel**, no alerta a alerta: puedes silenciar toda la categoría "Clientes" sin tocar el resto.
- Puedes **silenciar** una alerta concreta 1 hora, 24 horas o para siempre.
- El **feed usa scroll infinito simulado**: al llegar abajo se muestra el fin del historial.

### Procedimiento: configurar qué alertas quieres

1. Abre **Alertas** y pulsa **Configuración**.
2. Para cada categoría (Router, Internet, Clientes, Señal, VPN, Sistema) elige el nivel: **Solo urgentes**, **Todo** o **Nada**.
3. Si necesitas reglas propias (umbrales a medida), usa el gestor de **reglas** al final del panel.
4. Los cambios se guardan al instante (verás la confirmación).

### Procedimiento: marcar alertas como leídas

1. Pulsa una alerta para expandirla; se marca como leída automáticamente.
2. Para todas, usa **Marcar todo como leído**.

---

## Itinerancia WiFi

**Ruta:** `/roaming`. Título en la interfaz: **Itinerancia WiFi**.

### Qué muestra

Cinco pestañas:

- **Matriz**: la señal de cada cliente vista por cada punto de acceso (el "hearing map" de usteer). Una celda por cliente/AP con el valor en dBm y color.
- **802.11r**: estado de Fast BSS Transition por SSID y por router, con las banderas 11r/11k/11v/PMF y las anomalías detectadas.
- **Survey**: utilización de cada canal WiFi (ruido, porcentaje de ocupación, RX/TX) por router y radio.
- **Eventos**: conexiones, desconexiones y decisiones de itinerancia por cliente, con histórico de 30 días.
- **Reanclar**: clientes que estarían mejor en otro AP, con una acción para moverlos.

### Qué datos necesita

- La **Matriz** y **Reanclar** dependen de **usteer** (o de DAWN, que está en desuso y muestra un aviso). Sin un daemon de itinerancia activo en los routers, estas pestañas no tienen datos.
- **802.11r** lee `uci show wireless` de cada router con WiFi (un SSH por router).
- **Survey** usa `iw survey dump` (un SSH por router con WiFi).
- **Eventos** llegan por ingesta continua del agente a la base SQLite (30 días).

### Cómo leerlo

- En la **Matriz**, cada celda es la señal de un cliente vista por un AP: verde >= -65 dBm, ámbar entre -65 y -80, rojo por debajo de -80. Los clientes que ven varios APs con señal similar son candidatos a itinerar.
- En **802.11r**, el estado global resume si el roaming rápido está activado en todos, en parte o en ningún SSID; las anomalías (por ejemplo dominios de movilidad distintos) se marcan en rojo.
- En **Survey**, un canal con ocupación >= 70 % aparece en rojo (congestionado); >= 40 % en ámbar. El ruido más cercano a 0 dBm es peor (-90 es óptimo, -70 malo).
- En **Eventos**, el tipo se distingue por icono (conexión, desconexión, decisión de itinerancia).

### Decisiones de diseño

- Las pestañas **802.11r**, **Survey** y **Eventos** cargan de forma perezosa (al abrirlas) porque cada una cuesta un SSH por router.
- La pestaña **Reanclar** "expulsa" a un cliente de su AP actual para forzar la reconexión; es un write que actúa sobre el daemon de itinerancia (usteer), no un cambio de configuración.

### Procedimiento: interpretar la matriz de señal

1. Abre **Itinerancia WiFi > Matriz**.
2. Filtra por banda si hace falta (2.4 GHz / 5 GHz).
3. Busca filas donde el cliente ve varios APs con señal parecida (ámbar): son candidatos a itinerar o a recolocar.
4. Activa **Solo señal débil** para ver únicamente clientes con mala señal en todos los APs.

### Procedimiento: reanclar un cliente

1. Abre **Itinerancia WiFi > Reanclar**.
2. Revisa la tabla: cliente, AP actual, AP recomendado y la ganancia estimada (+dBm).
3. Pulsa **Mover** en la fila deseada. El daemon expulsa al cliente del AP actual para que elija el recomendado.

---

## Plan de canales

**Ruta:** `/wifi/channel-plan`. Título en la interfaz: **Plan de canales**. Marca "labs" en el menú.

### Qué muestra

Por cada router, una tarjeta por radio WiFi con el **canal actual**, el **canal recomendado** (calculado a partir de scans de vecinos) y, cuando hay datos, una puntuación (actual -> mejor). Debajo, una tabla de **APs vecinos** con SSID, BSSID, banda, canal y señal.

### Qué datos necesita

El plan se calcula con los **scans de vecinos** de cada radio. Aquí hay una diferencia clave: los routers que corren el panel **NetGrip** (el gateway y los APs gestionados por su panel) **no escanean vecinos**; los routers con el **agente NetPulse** sí. Por eso a veces un router no muestra recomendación.

### Cómo leerlo

- **Canal actual** en grande, **canal recomendado** resaltado si es distinto.
- La **puntuación** (si aparece) compara la calidad del canal actual con la del mejor: "X -> Y".
- En la tabla de **APs vecinos**, la señal se muestra en dBm y la banda (2.4 / 5 / 6 GHz) se deduce de la frecuencia.

### Decisiones de diseño

- Aplicar un canal **reinicia la red WiFi de ese router** unos segundos y sus clientes se reconectan. El cambio se ejecuta como un plan (ver Orquestación) y queda registrado, con la opción de **volver al canal anterior** con un clic.
- Si un radio no tiene sección UCI editable, en lugar del botón de aplicar verás el aviso de **actualizar el agente** del router.
- Esta página es de escritura: usa el mismo motor de planes que Orquestación.

### Procedimiento: cambiar el canal WiFi de un radio

1. Abre **Plan de canales** y elige el router en el selector.
2. En la tarjeta del radio, compara canal actual y recomendado.
3. Pulsa **Aplicar canal** y confirma (te avisa del reinicio breve de esa red).
4. Sigue el estado (Aplicando... -> Canal aplicado). Si quieres deshacer, pulsa **Volver al [canal]** (muestra el canal que tenías antes).

---

## Actualizaciones de firmware

**Ruta:** `/firmware-upgrades`. Título en la interfaz: **Actualizaciones de firmware**. Marca "labs" y solo administradores.

### Qué muestra

Una tarjeta por router con los campos del objetivo de actualización (versión actual, versión objetivo, modelo/target, URL de la imagen, checksum SHA256), el estado del último intento y las acciones de actualizar ahora o programar.

### Qué datos necesita

Es un flujo administrado: hay que indicar la URL de la imagen OpenWrt, el modelo/target y, recomendablemente, el checksum SHA256. El agente detecta el sistema del router (modelo, placa, versión, target) y lo muestra como "Sistema detectado" para ayudarte a rellenar los campos.

### Cómo leerlo

- El **estado** del intento usa etiquetas de color: Solicitada, Descargando, Haciendo copia, Verificando, Flasheando, Completada, Fallida, Programada.
- Un **aviso de fallo** muestra el error y su fecha; un fallo antiguo no refleja el estado presente del agente (se puede descartar).
- La **confirmación** te recuerda que se guardará una copia de la configuración antes de flashear, que el agente verifica el SHA256 (si lo has puesto) y que el router se reinicia y queda varios minutos sin servicio.

### Decisiones de diseño

- **Verificación antes de flashear**: si hay checksum y no coincide, la actualización se detiene y el router no se toca. Sin checksum, flashea sin verificar (se te avisa).
- **Respaldo automático** de la configuración antes de flashear.
- Se puede **programar** una actualización desatendida a una hora local concreta (y cancelarla antes de que arranque).
- El objetivo que se flashea es el **guardado**, no el que tienes a medio editar (el modal resume exactamente lo que se va a flashear).

### Procedimiento: actualizar el firmware de un router

1. Ve a **Actualizaciones de firmware** (solo administradores).
2. En la tarjeta del router, rellena versión objetivo, modelo/target y URL de la imagen. Ayúdate del "Sistema detectado" que muestra el agente.
3. Pega el **checksum SHA256** de la imagen (recomendado).
4. Pulsa **Guardar**.
5. Pulsa **Actualizar ahora** (o elige fecha/hora y **Programar**) y confirma.
6. Sigue el estado hasta Completada. Si quieres una actualización desatendida, usa **Programar** y podrás **Cancelar programación** antes de que empiece.

---

## Informes

**Ruta:** `/reports`. Título en la interfaz: **Informes**.

### Qué muestra

El informe de **disponibilidad** por router: una tabla con una columna por periodo y la disponibilidad en porcentaje, más una columna de media, y un detalle por router con latencia media, tráfico total y minutos con datos.

### Qué datos necesita

Se alimenta de las series temporales que el servidor acumula. Hay tres granularidades: **Diario** (7/14/30/60 días), **Semanal** (2/4/8/12 semanas) y **Mensual** (3/6/12/24 meses). Un rollup nocturno rellena el informe cada día; el periodo en curso se muestra parcial.

### Cómo leerlo

- La **barra de disponibilidad** usa el color semántico: verde >= 99 %, ámbar >= 95 %, rojo por debajo.
- Disponibilidad = minutos con datos sobre el total del periodo.
- En el detalle por router, la **latencia media** (ms), el **tráfico total** (subida + bajada) y los **minutos con datos** por periodo.

### Decisiones de diseño

- Solo está implementado el informe de **disponibilidad**; el resto (tráfico, actividad, resumen de alertas, exportación) está en el roadmap.
- Puedes **descargar el CSV** de la vista actual.

### Procedimiento: consultar la disponibilidad

1. Abre **Informes**.
2. Elige granularidad (**Diario / Semanal / Mensual**) y el número de periodos.
3. Lee la disponibilidad por router y por periodo; abre el detalle para latencia y tráfico.
4. Si necesitas los datos fuera de la app, pulsa **CSV**.

---

## Orquestación

**Ruta:** `/orchestration`. Título en la interfaz: **Orquestación**. Marca "labs", solo administradores y **oculta por defecto** (se activa desde Ajustes).

### Qué muestra

Un selector de módulo (AdGuard, WiFi invitado, DDNS, SQM, WireGuard, usteer) con sus campos, y el flujo **plan -> aplicar -> estado**. Generas un plan de cambios UCI, revisas el diff y lo aplicas; el resultado queda registrado.

### Qué datos necesita

Cada módulo escribe sobre el router seleccionado. Por defecto solo se lista el **gateway**; un interruptor "avanzado" permite un router no-gateway (con aviso). Los cambios se aplican por SSE a través del agente conectado.

### Cómo leerlo

- El **plan** muestra las operaciones UCI (kind + descripción) que se van a ejecutar.
- El **estado** del plan: pendiente, aplicando, aplicado, fallido o con rollback (rolled_back).
- Si el backend rechaza el plan (por ejemplo el recurso está gestionado por el firmware o es gateway-only), aparece un aviso específico.

### Decisiones de diseño

- Es la funcionalidad de **escritura** más general: un motor plan -> aplicar -> estado con ejecutor aislado.
- Todos los módulos son **gateway-only por defecto** (por seguridad); el toggle avanzado es opt-in.
- El método (apk/opkg/binario/activo...) se detecta del módulo y se muestra como etiqueta.

### Procedimiento: configurar un servicio vía orquestación

1. Activa Orquestación en **Ajustes** (si no la ves en el menú).
2. Abre **Orquestación** y elige el módulo.
3. Rellena los campos y pulsa **Generar plan**.
4. Revisa el diff del plan.
5. Pulsa **Aplicar** y espera el estado final (aplicado o fallido).

---

## Ayuda

**Ruta:** `/help`. Título en la interfaz: **Ayuda**.

### Qué muestra

Una guía rápida dentro de la app con recorridos guiados para las tareas del primer día: primer arranque y contraseña, alta de un router o switch, instalación del agente, lectura del plan de canales y actualización de firmware. Cada recorrido cita los botones reales de la interfaz (renderizados como etiquetas).

### Decisiones de diseño

Los pasos referencian las **mismas etiquetas de la interfaz** (mismo i18n), de modo que la ayuda evoluciona con la app: si un botón cambia de nombre, el recorrido queda desincronizado y se detecta. No hay capturas de pantalla a propósito (envejecen mal).

### Procedimiento: usar la ayuda

1. Abre **Ayuda**.
2. Despliega el recorrido que necesites.
3. Sigue los pasos numerados, que citan los botones tal como están en la app.

---

## Ajustes

**Ruta:** `/settings`. Título en la interfaz: **Ajustes**.

Es la pantalla más grande. Secciones principales:

- **Datos y umbrales**: unidades (Mbps / MB/s), intervalo de refresco (3 s / 5 s / 10 s / pausado), y los umbrales de temperatura (50-85 °C), señal (-80 a -60 dBm) y latencia (20-200 ms) que alimentan la salud. Incluye la velocidad WAN contratada y un test de velocidad periódico.
- **Apariencia**: tema (claro/oscuro/sistema), paleta de colores, color de acento, densidad (cómoda/compacta) y reducción de animaciones.
- **Servicios**: qué servicios se muestran (AdGuard, WireGuard) y el interruptor de Orquestación.
- **Notificaciones**: badge de alertas, puntos pulsantes y sonido.
- **Administración** (solo admin, en vivo): comprobar actualizaciones, respaldos, usuarios y modo demo.
- **Dispositivos** (solo admin): alta/edición/borrado de routers, clave pública SSH, target de firmware y configuración SNMP.
- **Overrides de topología** y **Dispositivos de confianza** (solo admin).
- **Adopción de agentes**: token de emparejamiento y huella del servidor.
- **AdGuard Home**: configuración de la conexión de solo lectura al panel AdGuard del router.
- **Mi perfil**: nombre, idioma, contraseña y salir.
- **API Tokens**: tokens bearer para integraciones.
- **Acerca de**: versión, enlaces, push/PWA, Telegram y datos del sistema.

### Decisiones de diseño

- Los ajustes de **idioma, tema y umbrales** se guardan por usuario/navegador; el idioma elegido en vivo se persiste en el backend.
- En **modo demo** Ajustes queda en solo lectura (no puedes cambiar la configuración de la red).
- Las mutaciones de administración (routers, usuarios) exigen rol admin y backend en vivo.

### Procedimiento: cambiar idioma o tema

1. Abre **Ajustes**.
2. Para el idioma, usa el selector de **Mi perfil**.
3. Para el tema, en **Apariencia** elige claro, oscuro o sistema (y opcionalmente paleta y acento).

### Procedimiento: ajustar los umbrales de la salud

1. Abre **Ajustes > Datos y umbrales**.
2. Mueve los deslizadores de temperatura, señal y latencia.
3. Observa la **puntuación simulada** (el anillo pequeño) para ver el efecto inmediato en la nota global.

---

## Qué vive en NetPulse y qué vive en el panel del router

NetPulse nace como **visor de solo lectura**: su servidor genera su propia clave ed25519, autorizas su clave pública en cada router y este solo lee (ubus, `/proc`, iwinfo, `bridge fdb`, `wg show`). Eso significa que hay áreas que **no son páginas de NetPulse** y se configuran en el panel propio del router:

- **WireGuard**: NetPulse lee los peers, handshakes y transferencias (en el detalle del gateway y en Topología), pero **crear o editar túneles se hace en el panel del router** (LuCI en OpenWrt, o el panel NetGrip/GL.iNet en el puerto 8080).
- **AdGuard Home**: NetPulse muestra estadísticas de consultas y dominios bloqueados, pero **la configuración de AdGuard** (listas, reglas, DNS upstream) se hace en el panel de AdGuard Home del router.
- **Puertos y VLANs**: NetPulse muestra qué hay conectado en cada boca LAN y las VLANs del bridge (solo lectura), pero **asignar puertos/VLANs** se hace en el panel del router.

Dicho esto, NetPulse ha ido incorporando **funciones de escritura opt-in** (marcadas "labs" y, en su mayoría, solo para administradores), que sí se hacen desde la propia app:

- **Plan de canales**: cambiar el canal WiFi de un radio (con revertir).
- **Actualizaciones de firmware**: flashear una imagen OpenWrt (con verificación y respaldo).
- **Reservas de IP**: fijar la IP de un cliente como lease DHCP estático en el gateway (sección Clientes).
- **Bloqueo de dispositivos**: cortar el acceso a la red de un cliente por MAC (sección Clientes).
- **Orquestación**: aplicar cambios UCI de AdGuard, WiFi invitado, DDNS, SQM, WireGuard y usteer.
- **Gestión del agente**: instalar, reinstalar y actualizar el agente NetPulse en los routers.

Regla práctica: **para ver y monitorizar, NetPulse; para configurar la red, el panel del router**, salvo las funciones "labs" de escritura que la propia app te ofrece explícitamente.

---

*Este manual describe el comportamiento de la aplicación tal como está implementada en el código. Algunas funciones dependen de lo que corra en tus routers (agente NetPulse, usteer, DAWN, NetGrip) y del modo en que arranques NetPulse (demo o vivo); cuando una pantalla requiere una de estas piezas, se indica en su sección.*
