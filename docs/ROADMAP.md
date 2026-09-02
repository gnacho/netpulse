# NetPulse — Hoja de ruta

> Actualizada: 2026-09-02 (**v2.25.0** publicado: acciones de router en tabla, vitales condicionales, timeline compacta de upgrades, comparación de builds; **v2.26.0** planificado: #448 bugfix, #451 apply seguro con ownership UCI, #452 channel planning, #453 firmware upgrades).

## Estado actual ✅

Hecho y en producción (CT 226 + 4 routers, agentes v2.8.0):
- **Fase 14 — Visibilidad WiFi/roaming** ✅ **COMPLETA (v2.8.0)**: matriz
  clientes×APs (14.2b), estado 802.11r por SSID (14.3), utilización por canal
  (14.4), eventos de roaming persistentes con 30 días de histórico (14.5).
- **Fase 15.1 — Informes de disponibilidad**: pestañas día/semana/mes sobre
  `metrics_daily`.
- **Fase 9 — On-box** (v2.7.2): config UCI, bootstrap AUTH_PASS, journal
  DELETE (R4/R5/R6), TLS autofirmado + SPKI pinning (R2), pairing token (R3),
  paquete server OpenWrt (R1).
- **Fase 10 — Orquestación** (fundación hecha): motor plan→apply→state + agent
  executor (10.1), módulo AdGuard Home (10.2, E2E verificado). **La Fase 17
  amplía este motor a despliegues completos (instalador + scripts + firewall)**.
- **Fase 13 — Auditoría Fable de robustez** (v2.7.2): single-flight, SSE
  deadline, sshpool race, %w wrapping.
- **Fase 12 — Auditoría de seguridad** (v2.7.0): TRUST_PROXY, anti-replay,
  body cap, password mínima 10.
- **Fase 11 — LuCI** (v2.7.2): `luci-app-netpulse` vista de nodo + test
  connection + server on-box package.
- **Fase 1-8**: topología v5, alertas, agente nativo, Node→Go, view-model,
  resiliencia, seguridad HMAC, consolidación, retención, recharts v3,
  actualización de stack (TS7/Vite8/Tailwind4/RR8).
- **Dead Man's Switch** (P6): histéresis 3 min en alertas de agente caído.
- **slog log levels** (P7): Info/Debug en agente.
- **Updater layout-aware**: rolling (git) vs stable (install.sh), release tags.
- **Tooltip fix**: topología no recorta hover cards.
- **WAN traffic header fix**: selector de rango no se amontona.
- **Editar router** (v2.8.0): `PUT /api/config/routers/{id}` + UI, evita
  borrar+recrear para cambiar host/agent_only/gateway.

Dormido en producción:
- **Web Push**: sin HTTPS en CT 226 → bloqueante para notificaciones push.
- **.apk para 25.12**: routers .2-.4 con instalación manual; CI listo.

Deuda menor:
- npm run lint roto (typescript-eslint no soporta TS7).
- Informe semanal de disponibilidad (daily ya tiene up_min/up_count).

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
   comandos del servidor. Infraestructura lista para Fase 10 (escritura).
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

## Fase 8 — Consolidación de deudas y cierre de gaps (antes del on-box)

Frente de trabajo previo al paquete del gateway (Fase 9). Cada ítem es un PR
independiente; ordenados por valor/esfuerzo.

1. **✅ Métricas operativas en `/api/health`** (barato, alto valor operativo):
   - Añadir `agentsConnected`, `sseConnections`, `devicesTotal` al health.
     Los datos ya existen (AgentRegistry, hub SSE, poller) — solo exponerlos.
     Punto de mejora nº2 de la auditoría Fase 7. **HECHO: PR #20.**

2. **✅ Persistir el registry de agentes (R8)** (prerrequisito de pairing de la
   Fase 9):
   - Hoy `AgentRegistry` es un `map` en RAM → `lastSeen`/payloads se pierden
     al reiniciar el servidor. Deuda transversal ya anotada.
   - Persistir último push por slug en SQLite (kv `agent.state.<slug>`);
     cargar al arrancar (`NewStateRestorer`). **HECHO: PR #22.** Los slugs sin
     token ya se rechazaban (401) → "solo slugs conocidos" cubierto.

3. **✅ Histórico largo en server-go (RETENCIÓN, no sidecar)** (el gap de
   histórico largo, deuda desde merge v2; **decisión 6-Ago: descartada la
   frontera sidecar-readonly** — el tráfico/cpu/ram/temp ya los captura el
   propio server-go en `metrics`, ampliar el sidecar duplicaría la adquisición,
   justo lo que evita go-collector-stack):
   - La tabla `metrics` del server-go solo conserva **7 días** (RetentionMS en
     db.go) mientras la webapp ofrece selector 1h/24h/7d/30d → el 30d está
     vacío de datos. Resolver con la escalera del skill sqlite-timeseries-daemon
     sobre la MISMA tabla: raw (7d, ya existe) → **buckets 5min (1 año)** →
     **daily (∞)**, job nocturno de rollup→purga→checkpoint (referencia
     references/jobs.md). Aditivo, sin tocar el sidecar. **HECHO: PR #24.**
   - El endpoint de histórico (`metricsHistory`) sirve corto de `metrics` y
     largo (30d) de `metrics_buckets` con downsampling (ventanas 6h).
   - ⏸️ Bonus pendiente: **informe semanal de disponibilidad** (deuda auditoría
     v2.1.0; el `daily` ya lleva `up_min`/`up_count` preparados).
   - Colateral pendiente: reinstalar el collector con release nueva (reporta
     `0.1.0`: nunca se aplicó el fix goreleaser `-X main.Version`).

4. **✅ recharts v2 → v3** (deuda de mantenimiento):
   - `app/package.json` usa `^2.15.4` (deprecated en npm). PR con verificación
     visual de las gráficas (Playwright). **HECHO: PR #29** (v3.10.1; se eliminó
     el wrapper `chart.tsx` muerto de shadcn).

5. **✅ Limpieza de ramas** (trivial):
   - `origin/feat/go-backend`, `origin/fix/issues-3-4-5` (y `fix/netpulse-jwt-cve`
     si quedó tras el squash) — borrar las ya mergeadas. **HECHO**: ramas
     locales y remotas purgadas; 3 PRs de dependabot cerrados con motivo
     (react-router v8, hono legacy ×2).

6. **✅ Auditoría de majors de frontend** (6-Ago-2026):
   - Estado real verificado: React 19.2.8 ✅ al día; recharts 2.15.4→3.10.1
     (2 majors detrás, deprecated — PR con verificación visual); Vite 7.3.6→8.2.0,
     Tailwind 3.4→4.3.3, TypeScript 5.9→7.0.2, react-router 7.18.2→8.3.0.
   - **DECISIÓN POSTERIOR (6-Ago noche, orden del usuario)**: subir TODO el
     stack — modernc.org/sqlite 1.56, **TypeScript 7 (tsgo)**, **Vite 8**,
     **Tailwind 4**, **react-router 8.3.0** — revirtiendo la decisión previa
     de no subir estos majors. Cada uno en su PR con verificación completa
     (build + tsc + PWA + Playwright). Riesgo asumido por el usuario.
     → Ejecutado en el **ítem 7** (abajo).

7. **✅ Actualización del stack a las últimas versiones** (issue #32, 6-Ago-2026,
   orden del usuario — revierte la decisión de no subir majors):
   | Componente | De → A | Estado |
   |---|---|---|
   | modernc.org/sqlite | 1.55 → **1.56.0** | ✅ HECHO (PR #33) |
   | TypeScript | 5.9.3 → **7.0.2** (tsgo) | ✅ HECHO (PR #34) |
   | Vite | 7.3.6 → **8.2.0** | ✅ HECHO (PR #36) |
   | Tailwind | 3.4.19 → **4.3.3** (CSS-first) | ✅ HECHO (PR #35) |
   | react-router | 7.18.2 → **8.3.0** | ✅ HECHO (PR #37) |
   - Cada bump en su PR con build + tsc + PWA + verificación Playwright antes
     de mergear; deploy en CT 226 tras cada merge. Riesgo asumido por el usuario.
   - **Lecciones del bloque (6-Ago)**: (1) Vite 8 (lightningcss) es incompatible
     con Tailwind 3 → orden forzado: Tailwind 4 antes; (2) react-router v8
     FUSIONA react-router-dom (el paquete @8 no existe; replace de imports +
     eliminar la dep); (3) recharts v3 necesita `react-is` explícito (lo dejaba
     fuera --legacy-peer-deps → build roto); (4) Tailwind 4: colores en @theme
     PUROS (sin `<alpha-value>`), opacidad vía color-mix automático; (5) TS7
     elimina `baseUrl` (paths relativos). **Todo desplegado en CT 226,
     producción==main verificado (binario 5ce0c4ad, bundle index-wipKAvlJ).**
   - **⚠️ DEUDA DE ECOSISTEMA**: typescript-eslint@8.66 NO soporta TS7 (peer
     `<6.1.0`) → `npm run lint` manual ROTO hasta que publiquen soporte. No
     bloquea build ni CI (el lint no corre en CI). Fix aplicado en main:
     `overrides` en app/package.json para que `npm ci` funcione.

8. **Webhooks y API de ingesta** (movido de Fase 10, 6-Ago-2026; verificada:
   **no implementados en el código**, solo planificación del commit 4a4428e):
   - **Webhook saliente** ✅ HECHO (PR #31): notificaciones de alertas a URLs
     externas con firma HMAC-SHA256, reintentos con backoff y DLQ — patrón
     EasyZFS v2.4.0, skill `email-webhook-notifications`. Endpoint admin
     `GET /api/webhook/dlq`. **INACTIVO en producción** (falta WEBHOOK_URL/
     SECRET en .env — decisión del usuario).
   - **API de ingesta (webhook entrante)** ⏸️ DIFERIDO (decisión 6-Ago):
     endpoint HTTP firmado para que sistemas externos envíen eventos a
     NetPulse sin agente (lista de orígenes permitida, anti-SSRF). Reutiliza
     el patrón HMAC de Fase 6 (el único HMAC que existe hoy es el de la
     ingesta del agente).
   - **✅ Skill `api-ingest-go` CREADA (6-Ago)**: cubre el hueco de
     conocimiento del entrante (firma, orígenes, anti-SSRF, idempotencia por
     event_id, rate limit, body cap). `api-stack` es solo Node (no aplica a
     backend Go); `email-webhook-notifications` solo cubre saliente.
   - **Prácticas del curado `learn/skills_go_api_development.md` (6-Ago)**: de
     ese catálogo solo aplica al entrante el **request ID** (UUID por request
     en headers/logs; hoy no existe `x-request-id` en server-go). Ya cubiertos:
     rate-limit por IP en ingesta (`ipRateLimit`, ventana 1 min), envelope de
     error propio con i18n (no RFC 7807), samber golang-* instalados. El resto
     del catálogo (pgx/sqlc/Redis, Gin/Echo/Fiber, gRPC/OTel) NO aplica:
     stack SQLite + stdlib net/http.

Quedan fuera de esta fase (a propósito): Web Push (requiere decisión de infra
HTTPS en CT 226, no es micromejora) y el informe semanal de disponibilidad
(cubierto en el ítem 3 como bonus pendiente).

---

## Fase 9 — App embebida en routers (on-box; antes "Fase 8")

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
5. **`luci-app-netpulse`**: ver Fase 11 (paquete LuCI dedicado, se construye
   después de la Fase 9; no reemplaza la PWA, permite diagnóstico local sin
   la app).
6. **Presupuesto y roles**:
   - Gateway (Flint 2): server (~20 MB RSS) + agent.
   - APs (Redmi AX6, 512 MB): solo agente.

**Criterios de aceptación:**
- Con el CT 226 apagado, la app es accesible desde `https://192.168.1.1`.
- Desde un móvil en la red se puede adoptar un AP nuevo sin editar env files.
- El gateway sobrevive a un reinicio conservando configuración y últimas 24 h
  de datos (en USB/overlay).

**Bloqueantes previos**: TLS resuelto en Fase 6 + agente bidireccional de
Fase 7. Ver SPEC-FASE8.md (retos R1-R6 y criterios).

**Deploy**: instalar server on-box en el gateway, agentes en APs.

---

## Fase 10 — Escritura/orquestación (riesgo alto, reglas estrictas; antes "Fase 9")

Objetivo: pasar de leer la red a actuar sobre ella de forma declarativa,
segura y reversible, con reglas que impidan quedarse sin wifi por un error.

Contexto:
- Hasta Fase 9 NetPulse es un panel de observación y alertas.
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

## Fase 11 — Paquete LuCI `luci-app-netpulse` (antes "Fase 10")

Objetivo: paquete `luci-app-netpulse` instalable junto al `.ipk`/`.apk` del
agente. **NO repite la webapp**: verificado en los feeds oficiales
(`openwrt/luci` y `openwrt/packages`) que LuCI es per-dispositivo y no existe
ningún NOC multi-router; la webapp (Go + SSE) sigue siendo el visor de red.

**Alcance acordado — vista de nodo (lo local, complementa sin solapar):**
1. **Estado y gestión del agente en ESTE router**:
   - Estado del servicio procd (vivo/muerto), versión del binario, uptime, RSS.
   - Config UCI (`/etc/config/netpulse`): server, slug, token presente,
     interval — editable.
   - Heartbeat / última conexión al servidor y si es alcanzable.
   - Logs del agente (`logread -e netpulse`) — hoy no visibles en la webapp.
   - Botones: restart del agente, rearm, ver token.
2. **Puente a la webapp central** (vista de red agregada): entrada de menú con
   enlace/iframe a la app (URL configurable en UCI). Cero UI duplicada.

**Implementación:**
- Luci modern (OpenWrt 24.10/25.x = ucode + JS/Vue, NO Luci legacy).
- El visor lee UCI + procd + logread directamente — sin listener HTTP extra
  en el agente (cero superficie/RAM).
- Build vía SDK OpenWrt; empaquetar para opkg y apk (25.12). El `.apk` del
  agente sigue pendiente de construir (Fase 7).
- Cobra pleno sentido tras la Fase 9 (server on-box en el gateway); para
  terceros con OpenWrt estándar, ya es útil sin ella.

**Criterios de aceptación:**
- `opkg install luci-app-netpulse` (24.10) y equivalente `.apk` (25.12).
- Sin HTTP extra en el agente; RSS del router no sube por el paquete.
- El paquete lee estado real del agente (procd/UCI/logread) y lo muestra
  sin login adicional de la webapp.

**Deploy**: paquete junto al agente en gateway y APs.

---

## Fase 12 — Auditoría de seguridad y robustez (release bug-hunting)

Objetivo: pasada sistemática de caza de bugs y endurecimiento de seguridad
sobre el código actual (server-go + app + agentes), materializada en una
release de mantenimiento. Cada hallazgo se abre como issue y se cierra con su
PR (convención issue→PR).

**Alcance (áreas a revisar):**
1. **Autenticación y autorización**: revisar que todas las rutas de mutación
   (crear/revocar agentes, routers, rearm) exijan `RequireAdmin`; que no haya
   rutas sensibles solo con `requireAuth`. (Relacionado con issue #6.)
2. **CSRF**: los endpoints de mutación autenticados por cookie deben proteger
   contra CSRF (SameSite + verificación de Origin cuando aplique).
3. **Cabeceras de seguridad HTTP**: `X-Content-Type-Options`, `X-Frame-Options`
   o CSP en respuestas del server y del SPA embebido.
4. **Ingesta de agentes**: firma HMAC por mensaje + anti-replay (timestamp/nonce)
   para que un token robado no baste para inyectar datos falsos.
5. **Path traversal y servido estático**: revisar rutas de estáticos, descargas
   del binario del agente y cualquier acceso a ficheros.
6. **Secretos**: confirmar que tokens/secretos no aparecen en logs ni en
   respuestas; que el `.env` de producción no contiene nada imprimible.
7. **Dependencias**: `go list -m` y `npm audit` para deps del server-go y del
   front; cerrar cualquier CVE HIGH/CRITICAL real.
8. **Bugs latentes**: revisar manejo de errores, cierres de conexiones, races
   en el poller y caminos de error poco probados.

**Criterios de aceptación:**
- Ninguna ruta de mutación sensible alcanzable con rol `user`.
- Las respuestas del server llevan cabeceras de seguridad básicas.
- 0 CVEs HIGH/CRITICAL reales en deps (audit verde o justificado).
- Tests nuevos cubriendo cada fix (test que falla primero).

**Deploy**: release + install.sh en CT 226 y agentes.

---

## Fase 13 — Auditoría de robustez de código (COMPLETA, v2.7.2)

Objetivo: auditoría dirigida de concurrencia, manejo de errores y fronteras
en server-go. Cada hallazgo con mecanismo completo, test de regresión y fix.

**Hallazgos corregidos (4 PRs → main):**

1. **Single-flight de `GetOverview`** (#66 → #69): un panic en `buildOverview`
   congelaba el poller para siempre (`sfInFlight` sin reset) y los seguidores
   2..N recibían `(nil, nil)` del canal cerrado. Reescrito con struct `sfCall`
   compartido + `close(done)` como pura señal + `recover` del líder.
2. **Cliente SSE zombi bloquea broadcast** (#67 → #70): un peer desaparecido
   sin cerrar TCP llenaba el buffer del kernel y `fmt.Fprint` bloqueaba ~15 min
   el Broadcast síncrono de todos los clientes. Write deadline 10 s vía
   `ResponseController` + `ReadHeaderTimeout` en `http.Server`.
3. **Carrera en `SSHPool.dial`** (#68 → #71): dos dialers concurrentes al mismo
   host filtraban una conexión SSH. Single-flight por host (canal `dialing`) +
   chequeo de `p.closed`.
4. **`%v` → `%w` en envolturas de error** (#72 → #73): dos sitios rompían
   `errors.Is/As` aguas arriba.

**Criterios de aceptación:**
- Tests de regresión con `-race` para cada fix (fallan contra el código original,
  pasan con el fix).
- `go vet` limpio en server-go, collector y agent.
- 0 cambios de API (retrocompatible).

**Deploy**: release v2.7.2.

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

## Fase 14 — Visibilidad WiFi y roaming ✅ COMPLETA (v2.8.0, solo lectura, cero riesgo)

Objetivo: dar visibilidad del estado del roaming y WiFi que hoy solo es
accesible por SSH. El diagnóstico del incidente DAWN (22-Jul, 30-45s de
corte) habría sido trivial con esta visibilidad.

**Todo es lectura**: no se modifica config de router. Cero riesgo.

1. **14.1 — DAWN probe en agente**: el agente añade `ubus call dawn
   get_network` + `get_hearing_map` al payload de push. Nuevo campo
   `data.dawn` con APs vistos, clientes, matriz de señal. Solo si DAWN
   está instalado (fail-soft silencioso si no).
2. **14.2 — Roaming matrix** (UI): heatmap clientes × APs con señal
   (-dBm). Verde ≤-65, ámbar -75, rojo ≥-85. Identifica clientes en
   zonas límite candidatos a roaming.
3. **14.3 — 802.11r status**: por AP: ieee80211r on/off, mobility_domain,
   ft_over_ds. Alerta visual si los APs tienen domains distintos
   (inconsistencia = roaming roto).
4. **14.4 — WiFi survey**: por radio: canal, anchura, potencia,
   utilización (`ubus call iwinfo scan` para redes vecinas). Detecta
   solapamiento de canales.
5. **14.5 — Eventos de roaming**: timeline con assoc/disassoc/roam
   (cruzando hearing maps consecutivos + iw events ya capturados).

**Deploy**: agentes actualizados (ya en v2.8.0). UI nueva sin tocar routers.

---

## Fase 15 — Informes y analítica

Objetivo: convertir los datos acumulados (7d raw + 1año buckets + ∞ daily)
en informes accionables.

1. **15.1 — Disponibilidad ampliada**: vista diaria/mensual (no solo
   semanal). % uptime por router, detalles de caídas. Datos: `metrics_daily`
   (ya tiene `up_min`/`up_count`).
2. **15.2 — Tráfico histórico**: tendencias WAN (down/up por día/semana/mes).
   Top N dispositivos por consumo. Datos: `metrics_buckets`.
3. **15.3 — Actividad de dispositivos**: cuándo conecta/desconecta cada
   dispositivo. Horas activo/día. Datos: FDB/DHCP histórico.
4. **15.4 — Resumen de alertas**: conteo por categoría/severidad/tiempo.
   Tendencia (¿va mejorando la red?).
5. **15.5 — Exportación**: CSV de tablas, PNG de gráficas.

**Deploy**: sin cambios en routers (los datos ya se colectan).

---

## Fase 16 — Alertas avanzadas

Objetivo: alertas más inteligentes y menos ruidosas.

1. **16.1 — Reglas custom**: umbrales configurables por router/dispositivo
   (CPU >80%, señal < -75, etc.).
2. **16.2 — Tipos nuevos**: fallo de roaming (cliente rebota entre APs),
   congestión de canal (utilización >70%), dispositivo offline >Xh,
   DAWN kick detectado.
3. **16.3 — Historial filtrable**: filtro por fecha, router, severidad.
   Búsqueda libre. Exportación.
4. **16.4 — Silencio programado**: "no molestar" en ventanas de
   mantenimiento.
5. **16.5 — Email**: SMTP además de webhook (skill `email-webhook-notifications`).

---

## Fase 10 — Orquestación (continuación)

La fundación (10.1) y el módulo AdGuard (10.2) están hechos y verificados
E2E. Pendiente:

3. **10.3 - WireGuard peers** ✅ (24-Ago-2026): alta/baja de peers desde la
   app. Riesgo medio. El plan crea la interfaz (network.wg0) si falta,
   reconcilia las secciones wgpeer&lt;N&gt; (public_key + allowed_ips) y termina con
   `service network reload` + healthcheck `wg show` (auto-rollback si el túnel
   que estaba arriba no levanta). Anti-lockout: el peer con el pubkey del admin
   nunca se borra. Módulo: server-go/internal/orchestr/wireguard.go.
4. **10.4 — DAWN/802.11r config** (write): modificar config de roaming
   desde la app con snapshot+rollback. **Solo tras Fase 14 estabilice**
   (necesitamos la visibilidad para diagnosticar si algo va mal).
5. **10.5 — Batman-adv** (futuro lejano): mesh sobre LAN. Dry-run obligatorio.

---

## Fase 17 — Escribir en los routers (en progreso, v2.26.0)

> **En desarrollo**: [#451](https://github.com/gnacho/netpulse/issues/451), [#452](https://github.com/gnacho/netpulse/issues/452), [#453](https://github.com/gnacho/netpulse/issues/453). Esqueleto acordado 8-Ago-2026; el detalle de cada módulo vive en su propio issue.

### v2.26.0 — Fundación segura de escritura

Antes de desplegar módulos completos se estabilizan tres primitivas de seguridad:

1. **Ownership UCI + apply seguro con rollback (#451)**: las secciones gestionadas por NetPulse se marcan con `np_managed`, el executor rechaza tocar secciones ajenas y el apply usa el rollback nativo de OpenWrt (o snapshot+revert) con healthcheck post-cambio.
2. **Channel planning (#452)**: el agente recoge scan de vecinos (`iwinfo scan`) y la UI recomienda canal óptimo por radio, aplicable vía #451.
3. **Firmware upgrades (#453)**: descarga de imagen oficial, verificación, `sysupgrade` vía agente y recuperación si se pierde conectividad. Reutiliza el motor de #451.

Estos tres issues cierran los prerrequisitos comunes listados más abajo y desbloquean los módulos de bajo riesgo (Fase 18).

| # | Módulo | Fase | Riesgo |
|---|---|---|---|
| 17.1 | AdGuard Home (full) | 18 | Bajo |
| 17.2 | WiFi guest / aislada | 18 | Bajo |
| 17.3 | DDNS (ddns-scripts) | 18 | Bajo |
| 17.4 | QoS (sqm-scripts) | 18 | Bajo |
| 17.5 | WireGuard peers | 19 | Medio |
| 17.6 | WireGuard túnel nuevo | 19 | Medio |
| 17.7 | OpenVPN | 19 | Medio |
| 17.8 | Tailscale | 19 | Medio |
| 17.9 | DAWN + 802.11r (write) | 20 | Alto |
| 17.10 | Batman-adv mesh | 20 | Muy alto |
| 17.11 | DPI (nDPI / ntopng) | 20 | Alto |
| 17.12 | **Firmware update in-app (labs)** | 20 | Alto |

### 🔥 17.12 — Firmware update in-app (labs) (23-Ago-2026)

Actualización de firmware de los routers desde la app, **como funcionalidad
labs** (mismo patrón que la orquestación: badge "labs" en el nav, opt-in vía
flag). **Activable hoy mismo** aunque aún no esté la vista completa de gestión;
el paso previo (detección de firmware desactualizado: alerta + campo en
routers + impacto leve en salud) vive en el issue #241.

- **Detección (ya modelada)**: `Router.firmware` se puebla en live desde
  `board.Release.Description` (live.go:777); la demo ya modela
  `FirmwareUpdated`/`FirmwareAvailable` (demo_extras.go:38-41) y una alerta de
  firmware (demo_dataset.go:153). Fuente del "última versión": a decidir
  (feed estable OpenWrt, API de fabricante GL.iNet o target configurable).
- **Aplicación**: reutiliza el motor plan→apply→state de la Fase 10 con
  snapshot + healthcheck + auto-rollback: descarga de imagen oficial con
  verificación de checksum, `sysupgrade` vía agente, y recuperación si se
  pierde conectividad.
- **Riesgo**: Alto (un fallo puede dejar el router incomunicado); por eso
  arranca como labs y no entra en el canal stable hasta beta-testing.

**Prerrequisitos comunes** (se estabilizan al implementar 17.1 como spike):
- Instalador de paquetes unificado: `opkg` (24.10), `apk` (25.12), binario
  oficial (AdGuard/Tailscale) o scripts firmware- específicos (GL.iNet).
- Provisioner de scripts `/etc/init.d/`, `/etc/uci-defaults/`, helpers.
- Healthcheck post-apply robusto (SSH + WAN + WiFi) con auto-rollback <60s.
- Auditoría en `orchestr_audit` + rollback manual `POST /api/plans/{id}/rollback`.
- Modo `dry_run` que muestra el plan completo sin tocar nada.

---

## Fases 18-20 — Programa de beta-testing y release escalonado

Los módulos de "escritura en router" son los más arriesgados del proyecto:
un bug puede dejar la red incomunicada, requiriendo acceso físico o consola
serie. Para llevarlos a producción segura necesitamos **beta-testers
externos** con hardware variado antes de marcarlos como estables.

### Estrategia

**Canal de release dual**:
- `stable` (tag `vX.Y.Z`): solo módulos que han pasado el ciclo completo
  de beta-testing.
- `unstable` (prerelease rolling): todos los módulos disponibles, warning
  claro en la UI antes de apply, sin garantía de no romper la red.

**Reclutamiento de beta-testers**:
- Anuncio en r/openwrt, foro GL.iNet, foro OpenWrt y Discord de self-hosted.
- Criterios: hardware variado (GL.iNet, Xiaomi, Raspberry, x86_64), red real
  en uso, dispuesto a reportar fallos con logs, idealmente con consola serie
  o acceso físico para recuperación.
- Grupo pequeño inicial (5-10 personas) con canal directo (Matrix o Discord).
- Cada beta-tester recibe release notes + checklist + procedimiento de
  rollback documentado.

**Ciclo de release por módulo**:
1. **Sandbox** (autor): development + tests en su propia red.
2. **Alpha** (1-2 beta-testers de confianza): primer deploy en hardware
   distinto al del autor. Auto-rollback obligatorio.
3. **Beta** (3-5 beta-testers): refinamiento de UX y manejo de errores.
4. **RC** (release candidate, público `unstable`): cualquier interesado.
5. **Stable**: tras 2 semanas sin incidentes en `unstable`.

### Fase 18 — Despliegues de bajo riesgo

Sandbox + alpha suficiente, sin beta-tester estricto. Estabiliza el motor
común de la Fase 17 (instalador, provisioner, healthcheck).

- **17.1 AdGuard Home (full)** — spike inicial, patrón referencia.
- **17.2 WiFi guest / aislada**.
- **17.3 DDNS**.
- **17.4 QoS (sqm-scripts)**.

### Fase 19 — Despliegues de riesgo medio

Beta obligatorio antes de `unstable`.

- **17.5 WireGuard peers**.
- **17.6 WireGuard túnel nuevo**.
- **17.7 OpenVPN**.
- **17.8 Tailscale**.

### Fase 20 — Despliegues de riesgo alto

RC + 2 semanas en `unstable` sin incidentes antes de `stable`.

- **17.9 DAWN + 802.11r (write)**.
- **17.10 Batman-adv mesh**.
- **17.11 DPI (nDPI / ntopng)**.

### Carga estimada

| Fase | Módulos | Estimación | Beta-testers |
|---|---|---|---|
| 18 | 4 | ~6-8 semanas | 1-2 (alpha) |
| 19 | 4 | ~8-10 semanas | 3-5 |
| 20 | 3 | ~10-12 semanas | 5+ (+ ciclo RC de 2 sem) |

Dentro de cada fase los módulos son independientes y pueden paralelizarse.
Conviene empezar por la Fase 18 porque sus módulos estabilizan el motor
común que las Fases 19-20 reusan.

---

## Convención de trabajo

Cada ítem de esta hoja de ruta se materializa como issue en GitHub antes de
tocar código, y se cierra con el PR que lo resuelve (ver convención
issue→PR acordada el 2026-08-03).

Separación desarrollo/deploy: cada fase indica qué es código y qué es
despliegue en el servidor/routers. El desarrollo se commitea y pushea;
el deploy se ejecuta aparte (install.sh, reinstall de agentes, etc.).
