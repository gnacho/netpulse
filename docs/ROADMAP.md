# NetPulse — Hoja de ruta

> Actualizada: 2026-08-05 (v2.6.0, Fase 7 completada). 

## Estado actual (v2.6.0) ✅

Hecho y en producción (CT 226 + 4 routers):
- **Fase 1 — Topología v5** (v2.1.0): FDB + LLDP en vivo, backhaul real,
  switches gestionados/inferidos, hipervisores con CTs anidados, collector.
- **Fase 2 — Alertas, Push y agente piloto** (v2.2.0): motor de alertas,
  Web Push VAPID, agente OpenWrt con tokens, refresh bajo demanda.
- **Fase 3 — Base read-only y refactor Node → Go** (v2.0.0): backend Go,
  collector Go, PWA React, auth multi-usuario, install.sh.
- **Fase 4 — View-model semántico + Ajustes remodel** (v2.2.0): `vm:1`,
  canon demo Go, `Device.infra`, displayName.
- **Fase 5 — Resiliencia del agente** (v2.3.0/v2.4.0/v2.4.1): watchdog,
  rearme, auto-rearme TTL, endurecimiento rutas admin.
- **Fase 6 — Seguridad del agente** (v2.5.0): HMAC-SHA256 en ingesta,
  binario del agente servido desde el propio servidor.
- **Fase 7 — Agente a fondo** (v2.6.0, hoy): ✅ iw events en tiempo real,
  ✅ SSE bidireccional (AgentHub + refresh), ✅ .ipk vía opkg en gateway,
  ✅ profiling (RSS 11-12 MB, CPU <1%), ✅ CI de empaquetado .ipk/.apk,
  ✅ landing web con posicionamiento OpenWrt nativo.

Dormido en producción:
- **Web Push**: sin HTTPS en CT 226 → bloqueante para notificaciones.
- **.apk para 25.12**: routers .2-.4 con instalación manual; CI listo
  (workflow `openwrt-package.yml`) para la próxima release.

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

## Fase 7 — Agente a fondo ✅ COMPLETADA (v2.6.0, 5-Ago-2026)

Objetivo: pasar del agente "sondista" a un componente de red local de
verdad, con eventos en tiempo real, comunicación bidireccional y empaquetado
oficial de OpenWrt.

**Entregables:**

1. **✅ 7.1 — Eventos nl80211 en tiempo real**: `iw event -t` como proceso hijo
   persistente; assoc/disassoc disparan push inmediato (min gap 3s).
   Desplegado en 4 routers, verificado end-to-end.

2. **⏸️ 7.2 — Netlink/nl80211 nativo**: APLAZADO. Sustituiría `brctl`/`iwinfo`/
   `ip neigh` por rtnetlink/nl80211. ~1 MB extra. Para futura iteración.

3. **✅ 7.3 — Comunicación bidireccional SSE**: `GET /api/agents/{slug}/stream`
   + `POST /api/agents/{slug}/refresh`. Agente mantiene SSE abierta, recibe
   comandos del servidor. Infraestructura lista para Fase 9 (escritura).
   Verificado: `refresh → {"ok":true}` en gateway y APs.

4. **✅ 7.4 — Empaquetado `.ipk`**: Makefile + procd init + UCI config +
   uci-defaults (migración .env→UCI + watchdog). Construido con SDK 24.10.
   Instalado vía `opkg` en gateway (aarch64_cortex-a53). Routers 25.12 usan
   instalación manual (mismo binario + UCI); .apk pendiente de CI automático
   (workflow `openwrt-package.yml` creado, commit 18bf47b).

5. **✅ 7.5 — Profiling**: RSS 11.1–12.4 MB (4 routers), CPU <1% idle,
   watchdog con `pidof` (fix para BusyBox), sin errores en logs.

**Criterios de aceptación:**
- ✅ WiFi disconnect → push < 3s
- ✅ CPU < 1% idle (muy por debajo del 5%)
- ⚠️ RSS 11-12 MB (por encima del criterio de 10 MB, aceptado; dentro del
  presupuesto de 40 MB del skill go-collector-stack)
- ✅ opkg install funcional en gateway (24.10)
- ⚠️ Compatibilidad OpenWrt: probado 24.10 y 25.12, no 23.05.

**Fixes post-auditoría (5-Ago-2026):**
- `--version` flag en agente (57e6f9c) — ya no cuelga SSH
- BuildCommit en servidor (57e6f9c) — updater sin falso positivo
- CI de empaquetado .ipk/.apk (18bf47b) — automatizado para próximas releases
- Landing web actualizada (16cc4d9) — posicionamiento OpenWrt nativo

**Deploy**: servidor v2.6.0 en CT 226, agentes v2.6.0 en 4 routers, .ipk en gateway.

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
6. **Webhooks y API** (orden del usuario, 6-Ago-2026):
   - **Webhook saliente**: notificaciones de alertas a URLs externas con firma
     HMAC-SHA256, reintentos con backoff y DLQ — patrón EasyZFS v2.4.0
     (skill `email-webhook-notifications`). Complementa Web Push para
     encaminar alertas a servicios propios o de terceros.
   - **API de ingesta (webhook entrante)**: endpoint HTTP firmado para que
     sistemas externos envíen eventos a NetPulse sin agente (lista de
     orígenes permitida, anti-SSRF). Reutiliza el patrón HMAC de Fase 6.

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
