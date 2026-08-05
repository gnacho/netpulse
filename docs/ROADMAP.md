# NetPulse — Hoja de ruta

> Actualizada: 2026-08-05 (v2.4.4). Fases consecutivas desde la 1, basadas
> en el histórico real de commits. Referencias: `docs/AUDITORIA-FASE65.md`
> (riesgos R1-R8), `docs/AGENTE-OPENWRT.md` (diseño del agente), ARCHITECTURE.md.

## Estado actual (v2.4.4)

Hecho y en producción (CT 226):
- Fases 1-5 completas: base read-only, topología v5, alertas/push/agente
  piloto, view-model semántico, resiliencia del agente.
- Fixes v2.4.2/v2.4.3/v2.4.4: idioma auto, BD limpia + demo desde UI,
  gl-clients por rutas SSH y agente, layout radial en topología, espaciado
  de iconos de clientes y aspect-ratio fijo en mapa (issues #3/#4/#5).

Dormido en producción (implementado pero sin efecto real):
- **Web Push**: sin HTTPS en el servidor ningún navegador puede suscribirse
  (secure context). El código es correcto; falta el despliegue TLS.
- **Agentes**: hacen push cada 15 s (sondeo local), pero sin eventos ubus no
  aportan tiempo real; y reportan versión 0.1.0 hasta reinstalarlos con la
  próxima release.

---

## Fase 1 — Base read-only (hecha, v1.x)

Monitorización SSH agentless de routers OpenWrt/GL.iNet:
- PWA en React con sondeo SSH de solo lectura (ubus, `/proc`, iwinfo).
- AdGuard Home + WireGuard (stats y estado).
- Auth multi-usuario con roles, discovery LAN.
- Backend migrado de Node a Go (binario único con la app embebida).

**Deploy**: install.sh one-liner, systemd en CT 226.

---

## Fase 2 — Topología v5 (hecha, v2.0.0)

Mapa semántico real basado en evidencia de red:
- FDB + LLDP en vivo: puertos cableados, switches gestionados vs. inferidos.
- Backhaul real (wifi/cable) detectado por sonda ubus.
- Hipervisores con sus CTs anidados (OUI de hipervisor + exactamente un host).
- Collector de series temporales (latencia TCP, heartbeat, timeseries SQLite).

**Deploy**: collector Go independiente con install-collector.sh.

---

## Fase 3 — Alertas, Push y agente piloto (hecha, v2.2.0)

- Motor de alertas con 6 categorías y config por categoría (urgente/todo/nada).
- Web Push nativo (VAPID) con SW propio (injectManifest).
- Agente OpenWrt piloto: ingesta con tokens, fallback SSH, procd con respawn.
- Refresco bajo demanda (POST /api/refresh con rate limit).

**Deploy**: agentes instalados en gateway + 3 APs vía install-agent.sh.

---

## Fase 4 — View-model versionado (hecha, v2.2.0)

API como view-model de presentación para consumo directo por clientes ligeros:
- `vm: 1` en overview, canon demo single-source en Go → JSON.
- `Device.infra` sellado server-side (hypervisor, ct, managed-switch).
- Topología semántica en el snapshot.
- Remodel de Preferencias: nombre de saludo, info de sistema real,
  AdGuard/usuarios/routers simplificados con alta colapsada.

**Deploy**: sin cambios (mismo servidor).

---

## Fase 5 — Resiliencia del agente (hecha, v2.3.0/v2.4.0/v2.4.1)

Cierre de los dos huecos de supervisión detectados en la auditoría del piloto:
1. **Plan A (auto-supervisión en el router):** watchdog cron con heartbeat.
2. **Plan B (rearme desde el servidor):** POST /api/agents/{slug}/rearm.
3. **Auto-rearme tras TTL (v2.4.0):** supervisor cada 30 s, opt-in con flag.
4. **Endurecimiento de permisos (v2.4.1):** rutas de mutación solo-admin.

**Deploy**: agentes reinstalados con watchdog incluido.

---

## Fase 6 — Fixes cosméticos y de datos (hecha parcialmente, v2.4.2/v2.4.3)

Issues #3/#4/#5 + feedback cosmético del usuario:

**Hecho:**
- Idioma por defecto "auto" (sigue al navegador, issue #3).
- Instalación con BD limpia y demo activable desde Ajustes (issue #4).
- IPs de clientes GL.iNet vía `gl-clients` donde dnsmasq no tiene lease,
  por las rutas SSH Y agente (issue #5 bug 1).
- Layout en anillos para nodos de distribución con muchos hijos (issue #5 bug 2).
- Remodel de Preferencias: routers solo-IP con alta colapsada, AdGuard
  editable/oculto según servicio, usuarios colapsados, nombre de saludo,
  info de sistema real en Acerca de (ya implementado en Fase 4).

**Hecho en v2.4.4:**
- **Espaciar más los iconos de clientes en topología**: radio de abanicos
  aumentado (ROUTER_FAN_RADIUS 134→150, DIST_FAN_RADIUS 82→96,
  HUB_FAN_RADIUS 56→68, hipervisor 300→320, anillos wifi gateway 88/118→96/130,
  AP 74/108→82/120, grid CT 42/38/56→46/44/64, grid gateway 46/40/142→56/50/160),
  y SVG con `preserveAspectRatio="xMidYMid meet"` para no distorsionar el
  espaciado al estirar el contenedor.

**Pendiente:**
- **Identificación de Proxmox en live**: hoy solo se detecta hipervisor si
  hay exactamente un host con MAC de hipervisor + VMs con OUI de hipervisor.
  En producción con cluster Proxmox (2 hosts citadel-01/02) no se identifica
  porque la regla exige "exactamente un host". Revisar si hay que relajar a
  "uno o más hosts" o si el problema es que los hosts no están en la BD.

**Deploy**: sin cambios (mismo servidor).

---

## Fase 7 — TLS y endurecimiento (bloqueante, ~1 día)

Sin esto, ni push funciona ni la Fase 9 es segura.

**Desarrollo:**
1. **HMAC-SHA256 en la ingesta del agente** (R4): firmar el payload con el
   token. ~10 líneas. Barato ahora, carísimo después de la Fase 9.
2. **Servir el binario del agente desde el propio servidor** (R3):
   `GET /api/agents/{slug}/binary?arch=...` — elimina la dependencia de
   GitHub en LANs sin salida y el `curl | sh` con token en argv.

**Deploy:**
1. **HTTPS en CT 226** (R1): Caddy delante con cert autofirmado + confianza
   manual, o Tailscale Serve (HTTPS automático). Desbloquea Web Push real y
   permite retirar `NETPULSE_INSECURE_TLS`.
   Decisión pendiente: Caddy vs Tailscale (afecta a cómo confiarán los
   routers en el cert cuando ellos también empujen con TLS).
2. **Reinstalar agentes con release nueva** para que reporten versión real
   (el fix `-X main.Version` ya está en goreleaser).

---

## Fase 8 — Agente a fondo (~1 semana)

El piloto actual ahorra SSH pero no da tiempo real. El salto es:

**Desarrollo:**
1. **Eventos ubus** (R7, el corazón): suscripción a hostapd assoc/disassoc →
   el dashboard ve clientes entrar/salir al instante.
2. **netlink/nl80211 nativo**: FDB/ARP vía rtnetlink, estaciones wifi con
   señal/tasas por cliente vía nl80211 (sin parsear CLI).
3. **`.ipk` empaquetado**: `opkg install` en vez de install-agent.sh; el
   instalador actual queda como fallback.
4. Medición en hardware real (RAM/CPU/flash) y ajuste de intervalos.

**Deploy**: reinstalar agentes con la nueva versión (`.ipk` si está listo).

---

## Fase 9 — App embebida en routers (decisión de diseño primero)

Convertir NetPulse en una app DEL router, no solo un panel externo:

**Desarrollo:**
- **Arquitectura objetivo**: el Flint 2 (gateway, 8 GB) corre
  `netpulse-agent` + `netpulse-server` reducido (modo on-box); los APs solo
  agent. La URL de la app ES la IP del router.
- **Bloqueantes previos**: decisión TLS de la Fase 7 (la app servida por el
  router necesita cert autofirmado + flujo "confiar") y DB fuera de NAND
  (SQLite WAL en flash = inviable; usar USB/eMMC/overlay).
- **Pairing cero-fricción**: token de emparejamiento mostrado en LuCI
  (patrón HomeKit/Tailscale), no curl|sh manual.
- **`luci-app-netpulse`** (opcional): gestión del agente desde LuCI.
- Presupuesto: server-go hoy = 20 MB RSS / 14,4 MB binario → sobra en el
  gateway; en APs (512 MB) nunca debe correr el server.

**Deploy**: instalar server on-box en el gateway, agentes en APs.

---

## Fase 10 — Escritura/orquestación (riesgo alto, reglas estrictas)

Pasar de leer a actuar. Reglas de diseño innegociables
(detalle en AUDITORIA-FASE65.md §5):

**Desarrollo:**
- Plan → apply → state (patrón Terraform): diff de config + confirmación.
- `uci` transaccional, nunca sed sobre ficheros; snapshot antes de aplicar;
  rollback verificable obligatorio.
- Idempotencia (estado declarado, no acumulado).
- Ejecutor con allowlist estricta de comandos (nada de shell libre).
- HMAC (Fase 7) firmado ANTES de que viaje ninguna orden.

Orden por riesgo/beneficio:
1. **AdGuard Home** (fácil: binario + YAML + DNS en dnsmasq; el módulo de
   stats ya existe).
2. **WireGuard peers** (alta de peer desde el móvil; riesgo medio: firewall).
3. **DAWN/802.11r** (riesgo alto: un error deja APs sin wifi → modo rescate
   con fallback a config previa).
4. **Batman-adv** (el último: riesgo de bucles en mesh; dry-run obligatorio
   + ventana de confirmación con auto-rollback).

**Deploy**: sin cambios hasta que haya algo que desplegar.

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
