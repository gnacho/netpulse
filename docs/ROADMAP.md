# NetPulse — Hoja de ruta

> Actualizada: 2026-08-05 (v2.4.4). Fases reordenadas por peso/entregable, no
> estrictamente por orden cronológico. El refactor Node → Go sigue siendo la
> base, pero se sitúa como Fase 3 porque las Fases 1 y 2 son los hitos visibles
> que hoy definen el producto. Referencias: `docs/AUDITORIA-FASE65.md`
> (riesgos R1-R8), `docs/AGENTE-OPENWRT.md` (diseño del agente), ARCHITECTURE.md.

## Estado actual (v2.4.4)

Hecho y en producción (CT 226):
- **Fase 1 — Topología v5** (v2.1.0): FDB + LLDP en vivo, backhaul real,
  switches gestionados/inferidos, hipervisores con CTs anidados, collector.
- **Fase 2 — Alertas, Push y agente piloto** (v2.2.0): motor de alertas,
  Web Push VAPID, agente OpenWrt con tokens, refresh bajo demanda.
- **Fase 3 — Base read-only y refactor Node → Go** (v2.0.0): backend Go
  (`server-go`), collector Go, PWA React, auth multi-usuario, install.sh y
  despliegue en CT 226.
- **Fase 4 — View-model semántico + Ajustes remodel** (v2.2.0): `vm: 1`,
  canon demo single-source en Go, `Device.infra` sellado server-side,
  displayName y remodel de Preferencias.
- **Fase 5 — Resiliencia del agente** (v2.3.0/v2.4.0/v2.4.1): watchdog,
  rearme desde servidor, auto-rearme TTL y endurecimiento de rutas admin.
- **Cierre de fixes cosméticos/de datos** (v2.4.2/v2.4.3/v2.4.4): idioma
  "auto", BD limpia + demo desde UI, clientes GL.iNet vía `gl-clients`, layout
  radial y espaciado de iconos en topología (issues #3/#4/#5).

Dormido en producción (implementado pero sin efecto real):
- **Web Push**: sin HTTPS en el servidor ningún navegador puede suscribirse
  (secure context). El código es correcto; falta el despliegue TLS.
- **Agentes**: hacen push cada 15 s (sondeo local), pero sin eventos ubus no
  aportan tiempo real; y reportan versión 0.1.0 hasta reinstalarlos con la
  próxima release.

Pendiente inmediato (Fase 6):
- HMAC-SHA256 en la ingesta del agente.
- Servir el binario del agente desde el propio servidor.
- HTTPS en CT 226.
- Identificación de Proxmox en cluster.

---

## Fase 1 — Topología v5 (hecha, v2.1.0)

Mapa semántico real basado en evidencia de red:
- FDB + LLDP en vivo: puertos cableados, switches gestionados vs. inferidos.
- Backhaul real (wifi/cable) detectado por sonda ubus.
- Hipervisores con sus CTs anidados (OUI de hipervisor + exactamente un host).
- Collector de series temporales (latencia TCP, heartbeat, timeseries SQLite).

**Deploy**: collector Go independiente con install-collector.sh.

---

## Fase 2 — Alertas, Push y agente piloto (hecha, v2.2.0)

- Motor de alertas con 6 categorías y config por categoría (urgente/todo/nada).
- Web Push nativo (VAPID) con SW propio (injectManifest).
- Agente OpenWrt piloto: ingesta con tokens, fallback SSH, procd con respawn.
- Refresco bajo demanda (POST /api/refresh con rate limit).

**Deploy**: agentes instalados en gateway + 3 APs vía install-agent.sh.

---

## Fase 3 — Base read-only y refactor Node → Go (hecha, v2.0.0)

Base funcional y migración de backend a Go. Cronológicamente fue el primer hito
mayor del proyecto, pero se ubica aquí como cimiento de las Fases 1 y 2:

- PWA en React con sondeo SSH de solo lectura (ubus, `/proc`, iwinfo).
- AdGuard Home + WireGuard (stats y estado).
- Auth multi-usuario con roles, discovery LAN.
- Backend migrado de Node a Go (`server-go`), binario único con la app embebida.
- Collector Go piloto y one-liner install.sh.

**Deploy**: install.sh one-liner, systemd en CT 226, goreleaser estable.

---

## Fase 4 — View-model semántico y Ajustes remodel (hecha, v2.2.0)

API como view-model de presentación para consumo directo por clientes ligeros:
- `vm: 1` en overview, canon demo single-source en Go → JSON.
- `Device.infra` sellado server-side (hypervisor, ct, managed-switch).
- Topología semántica en el snapshot.
- Remodel de Preferencias: nombre de saludo, info de sistema real,
  AdGuard/usuarios/routers simplificados con alta colapsada.

**Deploy**: sin cambios (mismo servidor).

---

## Fase 5 — Resiliencia del agente (hecha, v2.3.0/v2.4.0/v2.4.1)

Cierre de los huecos de supervisión detectados en la auditoría del piloto:
1. **Plan A (auto-supervisión en el router):** watchdog cron con heartbeat.
2. **Plan B (rearme desde el servidor):** POST /api/agents/{slug}/rearm.
3. **Auto-rearme tras TTL (v2.4.0):** supervisor cada 30 s, opt-in con flag.
4. **Endurecimiento de permisos (v2.4.1):** rutas de mutación solo-admin.

**Deploy**: agentes reinstalados con watchdog incluido.

---

## Fase 6 — TLS, endurecimiento y cierre de fixes (bloqueante, ~1 día + sprints)

Fusión de la deuda cosmética reciente con los bloqueantes de seguridad.
Sin esto, ni push funciona ni la Fase 8 es segura.

**Desarrollo:**
1. **HMAC-SHA256 en la ingesta del agente** (R4): firmar el payload con el
   token. ~10 líneas. Barato ahora, carísimo después de la Fase 8.
2. **Servir el binario del agente desde el propio servidor** (R3):
   `GET /api/agents/{slug}/binary?arch=...` — elimina la dependencia de
   GitHub en LANs sin salida y el `curl | sh` con token en argv.
3. **Identificación de Proxmox en live**: hoy solo se detecta hipervisor si
   hay exactamente un host con MAC de hipervisor + VMs con OUI de hipervisor.
   En producción con cluster Proxmox (2 hosts citadel-01/02) no se identifica
   porque la regla exige "exactamente un host". Revisar si hay que relajar a
   "uno o más hosts" o si el problema es que los hosts no están en la BD.
4. Cierre de deuda cosmética pendiente (si queda algo tras v2.4.4).

**Deploy:**
1. **HTTPS en CT 226** (R1): Caddy delante con cert autofirmado + confianza
   manual, o Tailscale Serve (HTTPS automático). Desbloquea Web Push real y
   permite retirar `NETPULSE_INSECURE_TLS`.
   Decisión pendiente: Caddy vs Tailscale (afecta a cómo confiarán los
   routers en el cert cuando ellos también empujen con TLS).
2. **Reinstalar agentes con release nueva** para que reporten versión real
   (el fix `-X main.Version` ya está en goreleaser).

---

## Fase 7 — Agente a fondo (~1 semana)

Objetivo: pasar del agente "sondista" actual a un componente de red local de
verdad, con eventos en tiempo real, recolección nativa del kernel y empaquetado
oficial de OpenWrt.

Contexto del piloto (v2.2.0):
- El agente ejecuta comandos shell localmente cada 15 s y hace push al servidor.
- Ahorra SSH, pero no es tiempo real y parsea texto (`iwinfo`, `ip neigh`,
  `ubus call`) que varía entre versiones de OpenWrt.
- Se instala con `install-agent.sh` (scp + procd); no es un paquete mantenible.

**Desarrollo:**
1. **Eventos ubus** (R7, el corazón): suscribirse a señales de `hostapd`
   (`assoc`/`disassoc`), `netifd` (`interface.up`/`down`) y `udhcpc`
   (`bound`/`renew`) y enviarlos al servidor inmediatamente (o con cola mínima).
   El dashboard debe ver entrar/salir clientes en el segundo.
2. **netlink/nl80211 nativo** (Go o C con bindings):
   - FDB/ARP vía `rtnetlink` (sin `bridge fdb`/`ip neigh`).
   - Estaciones wifi con RSSI, MCS, PHY rate por cliente vía `nl80211`
     (sin `iwinfo`).
   - Permite versiones de OpenWrt sin `iwinfo` y reduce CPU/memoria.
3. **Comunicación bidireccional robusta**: WebSocket o SSE desde el agente al
   servidor, con fallback a push HTTP como hoy. El server debe poder enviar
   órdenes al agente (Fase 9) sin esperar al próximo tick.
4. **Empaquetado `.ipk`**: Makefile OpenWrt, feed propio o incorporación al
   feed de paquetes del usuario; instalación con `opkg install netpulse-agent`.
   El `install-agent.sh` actual pasa a ser fallback para desarrollo.
5. **Profiling en hardware real**:
   - Medir RSS, CPU y escritura a flash en el Flint 2 y en los APs.
   - Ajustar intervalos: eventos en tiempo real, métricas de red cada 30 s,
     heartbeat cada 60 s.
   - Límite de buffer local (RAM) con drop-oldest si la conexión falla.

**Criterios de aceptación:**
- Un cliente wifi conectado aparece en el dashboard en < 3 s desde `assoc`.
- El agente consume < 5 % CPU en un AP de 4 núcleos ARM y < 10 MB RSS.
- Se instala/desinstala con `opkg` sin dejar archivos huérfanos.
- Tests de compatibilidad con OpenWrt 23.05 y 24.10.

**Deploy**: reinstalar agentes con el nuevo `.ipk` en gateway + 3 APs.

---

## Fase 8 — App embebida en routers (decisión de diseño primero)

Objetivo: NetPulse deja de ser un panel externo en un CT para convertirse en
una app que vive en el propio router, alcanzable desde la IP del gateway sin
dependencia de infraestructura externa.

Contexto:
- Hoy el servidor vive en CT 226 y los agentes le hablan por IP fija.
- Eso rompe si cambias de red, apagas el servidor o no tienes un CT disponible.
- El Flint 2 tiene 8 GB de RAM y flash eMMC; los APs de 512 MB solo pueden
  correr el agente.

**Desarrollo:**
1. **Modo on-box del servidor**:
   - Build de `server-go` con tag `onbox` o flag de compilación que excluya
     collector independiente y mantenga solo API web + ingesta de agentes.
   - Arranque vía procd con UCI config (`/etc/config/netpulse`).
   - Detección automática: si el router tiene interfaces WAN y LAN, actúa como
     hub (server + agent); si solo LAN/STA, actúa solo como agente.
2. **Persistencia fuera de NAND**:
   - SQLite en USB o overlay con WAL desactivado (`PRAGMA journal_mode=DELETE`
     o `MEMORY`) para no desgastar flash.
   - Migraciones automáticas al arrancar; backup diario de la BD.
3. **TLS local**:
   - Cert autofirmado generado en primer arranque + pantalla de "confiar" en
     el navegador, similar a Home Assistant o un NAS.
   - Alternativa: Tailscale funnel/certs para HTTPS automático si hay salida.
   - El agente debe validar el cert del servidor (Fase 6 HMAC) aunque sea
     autofirmado, sin `--insecure`.
4. **Pairing cero-fricción**:
   - Al instalar un agente, se genera un token de emparejamiento mostrado en
     LuCI o en un log accesible (`logread`).
   - El servidor on-box escanea/adopta agentes de la misma LAN con ese token.
   - No más `curl | sh` con token en argv.
5. **`luci-app-netpulse` (opcional pero recomendable)**:
   - Página en LuCI para ver estado del agente/server, último push, versión y
     botón de adoptar/desvincular.
   - No reemplaza la PWA, pero permite diagnóstico sin acceso a la app.
6. **Presupuesto y roles**:
   - Gateway (Flint 2): server (~20 MB RSS) + agent.
   - APs (Redmi AX6, 512 MB): solo agente.

**Criterios de aceptación:**
- Con el CT 226 apagado, la app es accesible desde `https://192.168.1.1`.
- Desde un móvil en la red se puede adoptar un AP nuevo sin editar env files.
- El gateway sobrevive a un reinicio conservando configuración y últimas 24 h
  de datos (en USB/overlay).

**Bloqueantes previos**: TLS resuelto en Fase 6 + agente bidireccional de
Fase 7.

**Deploy**: instalar server on-box en el gateway, agentes en APs.

---

## Fase 9 — Escritura/orquestación (riesgo alto, reglas estrictas)

Objetivo: pasar de leer la red a actuar sobre ella de forma declarativa,
segura y reversible, con reglas que impidan quedarse sin wifi por un error.

Contexto:
- Hasta Fase 8 NetPulse es un panel de observación y alertas.
- Cualquier cambio en routers (AdGuard, WireGuard, roaming) requiere LuCI/SSH.
- Un error en un script puede dejar la red inaccesible.

**Reglas de diseño innegociables** (detalle en `docs/AUDITORIA-FASE65.md` §5):
- Plan → apply → state (patrón Terraform): siempre mostrar diff antes de
  aplicar; nada se ejecuta sin confirmación explícita del usuario.
- `uci` transaccional; nunca `sed` sobre ficheros.
- Snapshot automático antes de apply; rollback si el healthcheck falla.
- Idempotencia: declarar el estado deseado, no acumular comandos.
- Allowlist estricta de operaciones; nada de shell libre.
- HMAC en todas las órdenes de escritura (Fase 6) para autenticar origen.

**Desarrollo:**
1. **Modelo de recursos**: definir recursos NetPulse (`AdGuard`, `WireGuardPeer`,
   `DawnConfig`, `BatmanMesh`) con schema versionado y validación server-side.
2. **Motor de orquestación**:
   - `POST /api/plans` → genera diff contra el estado actual del router.
   - `POST /api/apply/{planId}` → aplica tras confirmación del usuario.
   - `POST /api/rollback/{planId}` → revierte al snapshot UCI anterior.
3. **Ejecutor sandboxeado en el agente**:
   - Traduce recursos a comandos UCI/booleanos concretos de la allowlist.
   - Cada apply crea un snapshot de `/etc/config` antes de tocar nada.
   - Healthcheck post-apply: si ping/gateway/wifi falla, rollback automático.
4. **Módulos por orden de riesgo/beneficio**:
   1. **AdGuard Home**: binario + YAML + reenvío DNS desde dnsmasq. Módulo de
      stats ya existe; riesgo bajo.
   2. **WireGuard peers**: alta desde la app; ajustar firewall + allowed_ips.
      Riesgo medio.
   3. **DAWN / 802.11r**: roaming enterprise. Riesgo alto; un error deja APs sin
      wifi → modo rescate con fallback a config previa.
   4. **Batman-adv**: mesh L2. Riesgo muy alto; dry-run obligatorio + ventana
      de confirmación con auto-rollback si se pierde conectividad.

**Criterios de aceptación:**
- Cualquier apply muestra diff y pide confirmación antes de ejecutar.
- Un apply fallido deja la red en el estado anterior en < 60 s.
- Solo comandos de la allowlist pueden ejecutarse; shell libre está bloqueado.
- Auditoría: quién, qué y cuándo se aplicó cada cambio.

**Deploy**: se despliega módulo a módulo, empezando por AdGuard Home.

---

## Deudas y mejoras transversales (sin fase asignada)

- **Series del collector en server-go**: hoy son independientes
  (ARCHITECTURE.md lo recoge desde hace tiempo).
- **Retención de series + informe semanal de disponibilidad** (deuda desde
  la auditoría v2.1.0).
- **recharts v2 → v3** (la v2 está deprecated en npm; única dependencia
  caducada de verdad).
- **Retirar `NETPULSE_INSECURE_TLS`** cuando TLS esté consolidado (R5).
- Registry de agentes persistente (hoy `lastSeen` se pierde al reiniciar el
  servidor) y adopción solo de slugs conocidos (evitar huérfanos, R8).

---

## Convención de trabajo

Cada ítem de esta hoja de ruta se materializa como issue en GitHub antes de
tocar código, y se cierra con el PR que lo resuelve (ver convención
issue→PR acordada el 2026-08-03).

Separación desarrollo/deploy: cada fase indica qué es código y qué es
despliegue en el servidor/routers. El desarrollo se commitea y pushea;
el deploy se ejecuta aparte (install.sh, reinstall de agentes, etc.).
