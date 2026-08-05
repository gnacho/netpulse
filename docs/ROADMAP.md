# NetPulse — Hoja de ruta

> Actualizada: 2026-08-05 (v2.6.0-dev). Fases reordenadas por peso/entregable, no
> estrictamente por orden cronológico. El refactor Node → Go sigue siendo la
> base, pero se sitúa como Fase 3 porque las Fases 1 y 2 son los hitos visibles
> que hoy definen el producto. Referencias: `docs/AUDITORIA-FASE65.md`
> (riesgos R1-R8), `docs/AGENTE-OPENWRT.md` (diseño del agente), ARCHITECTURE.md.

## Estado actual (v2.5.0)

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
- **Fase 6 — Seguridad del agente** (v2.5.0): **HMAC-SHA256 en la ingesta**
  (firma obligatoria del payload con el token), **binario del agente servido
  desde el propio servidor** (`GET /api/agents/{slug}/binary`) eliminando la
  dependencia de GitHub y el token en argv.
- **Cierre de fixes cosméticos/de datos** (v2.4.2/v2.4.3/v2.4.4): idioma
  "auto", BD limpia + demo desde UI, clientes GL.iNet vía `gl-clients`, layout
  radial y espaciado de iconos en topología (issues #3/#4/#5).

Dormido en producción (implementado pero sin efecto real):
- **Web Push**: sin HTTPS en el servidor ningún navegador puede suscribirse
  (secure context). El código es correcto; falta el despliegue TLS.
- **Agentes**: hacen push cada 15 s (sondeo local), pero sin eventos ubus no
  aportan tiempo real; y reportan versión 0.1.0 hasta reinstalarlos con la
  próxima release.

Pendiente inmediato (Fase 7):
- Eventos ubus en tiempo real (hostapd assoc/disassoc).
- netlink/nl80211 nativo (FDB/ARP/stations sin parsear CLI).
- Comunicación bidireccional (WebSocket/SSE agente↔servidor).
- Empaquetado `.ipk` para `opkg install`.
- Profiling en hardware real (Flint 2 + APs).

Deuda sin fase:
- Web Push dormido (sin HTTPS en servidor; decisión de despliegue del usuario).
- Agentes reportan versión 0.1.0 (reinstalar con release nueva para el fix de goreleaser `-X main.Version`).
- Collector sidecar sin integrar con server-go.
- recharts v2 deprecated.

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

## Fase 6 — Seguridad del agente (hecha, v2.5.0)

Cierre de los huecos de seguridad detectados en la auditoría del piloto:

1. **HMAC-SHA256 en la ingesta** (R4): el agente firma cada payload con
   `HMAC-SHA256(token, body)` en la cabecera `X-Agent-Signature`. El servidor
   rechaza peticiones sin firma o con firma inválida (401).
2. **Binario del agente desde el servidor** (R3): endpoint
   `GET /api/agents/{slug}/binary?arch=...` con auth por token de agente
   (Bearer). Los binarios van embebidos en el servidor vía `go:embed`
   (construidos por CI para amd64/arm64/armv7). El one-liner de instalación
   descarga el binario del servidor en vez de GitHub.

**Deploy**: reinstalar agentes con la nueva release para que usen HMAC.

---

## Fase 7 — Agente a fondo (en curso, v2.6.0-dev)

Objetivo: pasar del agente "sondista" actual a un componente de red local de
verdad, con eventos en tiempo real, comunicación bidireccional y empaquetado
oficial de OpenWrt.

Contexto del piloto (v2.2.0):
- El agente ejecuta comandos shell localmente cada 15 s y hace push al servidor.
- Ahorra SSH, pero no es tiempo real y parsea texto (`iwinfo`, `ip neigh`,
  `ubus call`) que varía entre versiones de OpenWrt.
- Se instala con `install-agent.sh` (scp + procd); no es un paquete mantenible.

**Desarrollo (en curso):**

1. **✅ Eventos nl80211 en tiempo real** (7.1): `iw event -t` como proceso hijo
   persistente; assoc/disassoc de clientes wifi disparan un push inmediato
   (wireless + DHCP, min gap 3s). Ya no hay que esperar 30s para ver cambios
   en el dashboard.

2. **⏸️ Netlink/nl80211 nativo** (7.2): APLAZADO. Sustituiría `brctl`, `iwinfo`
   e `ip neigh` por rtnetlink y nl80211 (ABI del kernel, parseo estructurado,
   sin fork/exec). Añadiría ~1 MB al binario. Pendiente para después del .ipk.

3. **✅ Comunicación bidireccional SSE** (7.3): endpoint
   `GET /api/agents/{slug}/stream` en el servidor (auth Bearer, misma que la
   ingesta). El agente mantiene una conexión SSE abierta y recibe comandos
   (`refresh`, `connected`, `bye`). El servidor puede enviar comandos al
   agente sin esperar al próximo tick de sondeo. Infraestructura lista para
   Fase 9 (escritura/orquestación).

4. **🔄 Empaquetado `.ipk`** (7.4): Makefile OpenWrt + procd init script +
   UCI config (`/etc/config/netpulse-agent`) + uci-defaults (migración desde
   instalación manual previa + watchdog cron). `opkg install netpulse-agent`
   funcional. `install-agent.sh` actual pasa a fallback para desarrollo.

5. **⏳ Profiling en hardware real** (7.5): pendiente medir RSS, CPU y
   escritura a flash en el Flint 2 y en los APs.

**Criterios de aceptación:**
- ✅ Un cliente wifi desconectado/reconectado dispara un push en < 3s.
- ⏳ El agente consume < 5% CPU en un AP de 4 núcleos ARM y < 10 MB RSS.
- ⏳ Se instala/desinstala con `opkg` sin dejar archivos huérfanos.
- ⏳ Tests de compatibilidad con OpenWrt 23.05 y 24.10.

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
   1. **AdGuard Home**: **despliegue one-click** (descarga binario + YAML base +
      reenvío DNS desde dnsmasq + enable + firewall) + stats ya existentes.
      Riesgo bajo.
   2. **WireGuard**: **alta de peers desde la app** (clave + IP + allowed_ips +
      ajustar firewall). El túnel ya existe; solo se añaden/quitan peers.
      Riesgo medio.
   3. **DAWN / 802.11r**: roaming enterprise. Riesgo alto; un error deja APs sin
      wifi → modo rescate con fallback a config previa.
   4. **Batman-adv**: mesh L2. Riesgo muy alto; dry-run obligatorio + ventana
      de confirmación con auto-rollback si se pierde conectividad.
5. **Despliegue de servicios desde la app**: además de configurar, poder
   **instalar/activar** servicios sin tocar LuCI:
   - AdGuard Home: instalar, activar, desactivar, actualizar.
   - WireGuard: crear túnel nuevo (cliente o sitio-a-sitio), añadir/quitar peers.
   - Posibles futuros: VLAN simple, WiFi guest, QoS básico.
   - Cada despliegue es atómico: o se completa entero o se revierte.
   - La app deja de ser "solo lectura" para estos flujos asistidos.

**Criterios de aceptación:**
- Cualquier apply muestra diff y pide confirmación antes de ejecutar.
- Un apply fallido deja la red en el estado anterior en < 60 s.
- Solo comandos de la allowlist pueden ejecutarse; shell libre está bloqueado.
- Auditoría: quién, qué y cuándo se aplicó cada cambio.
- Activar AdGuard desde la app sin abrir LuCI ni editar un solo fichero a mano.

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
