# NetPulse — Hoja de ruta

> Actualizada: 2026-08-03 (v2.3.0: Fase 6.1 resiliencia del agente — watchdog
> cron con heartbeat + rearme desde el servidor). Orden por dependencias: lo
> bloqueante primero.
> Referencias: `docs/AUDITORIA-FASE65.md` (riesgos R1-R8),
> `docs/AGENTE-OPENWRT.md` (diseño del agente), ARCHITECTURE.md.

## Estado actual (v2.2.0)

Hecho y en producción (CT 226):
- Fases 1-5: monitorización SSH agentless, AdGuard/WireGuard, topología,
  alertas, PWA, demo mode, updater con banner.
- Fase 6 (piloto): agente nativo instalado y reportando en gateway + 3 APs,
  ingesta con tokens, Web Push implementado (backend + SW), alertas por
  categorías.
- Fase 6.5: view-model semántico (`vm: 1`), `Device.infra`, displayName,
  `/api/system/info`.
- Fixes recientes: pantalla negra tras update (cache + reload racing + guard),
  `-X main.Version` en el build del agente, `scp -O` para dropbear.

Dormido en producción (implementado pero sin efecto real):
- **Web Push**: sin HTTPS en el servidor ningún navegador puede suscribirse
  (secure context). El código es correcto; falta el despliegue TLS.
- **Agentes**: hacen push cada 15 s (sondeo local), pero sin eventos ubus no
  aportan tiempo real; y reportan versión 0.1.0 hasta reinstalarlos con la
  próxima release.

## Fase 0 — TLS y endurecimiento (BLOQUEANTE, ~1 día)

Sin esto, ni push funciona ni la Fase 8 es segura.

1. **HTTPS en CT 226** (R1): Caddy delante con cert autofirmado + confianza
   manual, o Tailscale Serve (HTTPS automático). Desbloquea Web Push real y
   permite retirar `NETPULSE_INSECURE_TLS`.
   Decisión pendiente: Caddy vs Tailscale (afecta a cómo confiarán los
   routers en el cert cuando ellos también empujen con TLS).
2. **HMAC-SHA256 en la ingesta del agente** (R4): firmar el payload con el
   token. ~10 líneas. Barato ahora, carísimo después de la Fase 8.
3. **Reinstalar agentes con release nueva** para que reporten versión real
   (el fix `-X main.Version` ya está en goreleaser).
4. **Servir el binario del agente desde el propio servidor** (R3):
   `GET /api/agents/{slug}/binary?arch=...` — elimina la dependencia de
   GitHub en LANs sin salida y el `curl | sh` con token en argv.

## Fase 6.1 — resiliencia del agente (hecha, v2.3.0)

Cierre de los dos huecos de supervisión detectados en la auditoría del piloto:

1. **Plan A (auto-supervisión en el router):** el agente toca
   `/tmp/netpulse-agent.heartbeat` en cada push confirmado; un watchdog cron
   (`/usr/sbin/netpulse-watchdog`, cada 2 min) relanza el servicio si procd se
   rindió (pgrep) y lo reinicia si el proceso vive pero lleva >5 min sin
   latido. Instalado/desinstalado por `install-agent.sh`.
2. **Plan B (rearme desde el servidor):** `POST /api/agents/{slug}/rearm`
   reinicia `init.d/netpulse-agent` vía el pool SSH del sondeo, espera el push
   de vuelta (30 s) y responde con el resultado real; cooldown 60 s por slug;
   botón «Rearmar» en la cabecera del router (solo agente caído).
3. Auto-rearme tras TTL: pendiente, detrás de flag (regla Fase 8: nada
   autónomo sobre equipamiento de red sin confirmación).

## Fase 6 — incremento 2: el agente de verdad (~1 semana)

El piloto actual ahorra SSH pero no da tiempo real. El salto es:

1. **Eventos ubus** (R7, el corazón): suscripción a hostapd assoc/disassoc →
   el dashboard ve clientes entrar/salir al instante.
2. **netlink/nl80211 nativo**: FDB/ARP vía rtnetlink, estaciones wifi con
   señal/tasas por cliente vía nl80211 (sin parsear CLI).
3. **`.ipk` empaquetado**: `opkg install` en vez de install-agent.sh; el
   instalador actual queda como fallback.
4. Medición en hardware real (RAM/CPU/flash) y ajuste de intervalos.

## Fase 7 — app embebida en routers (decisión de diseño primero)

Convertir NetPulse en una app DEL router, no solo un panel externo:

- **Arquitectura objetivo**: el Flint 2 (gateway, 8 GB) corre
  `netpulse-agent` + `netpulse-server` reducido (modo on-box); los APs solo
  agent. La URL de la app ES la IP del router.
- **Bloqueantes previos**: decisión TLS de la Fase 0 (la app servida por el
  router necesita cert autofirmado + flujo "confiar") y DB fuera de NAND
  (SQLite WAL en flash = inviable; usar USB/eMMC/overlay).
- **Pairing cero-fricción**: token de emparejamiento mostrado en LuCI
  (patrón HomeKit/Tailscale), no curl|sh manual.
- **`luci-app-netpulse`** (opcional): gestión del agente desde LuCI.
- Presupuesto: server-go hoy = 20 MB RSS / 14,4 MB binario → sobra en el
  gateway; en APs (512 MB) nunca debe correr el server.

## Fase 8 — escritura/orquestación (riesgo alto, reglas estrictas)

Pasar de leer a actuar. Reglas de diseño innegociables
(detalle en AUDITORIA-FASE65.md §5):

- Plan → apply → state (patrón Terraform): diff de config + confirmación.
- `uci` transaccional, nunca sed sobre ficheros; snapshot antes de aplicar;
  rollback verificable obligatorio.
- Idempotencia (estado declarado, no acumulado).
- Ejecutor con allowlist estricta de comandos (nada de shell libre).
- HMAC (Fase 0) firmado ANTES de que viaje ninguna orden.

Orden por riesgo/beneficio:
1. **AdGuard Home** (fácil: binario + YAML + DNS en dnsmasq; el módulo de
   stats ya existe).
2. **WireGuard peers** (alta de peer desde el móvil; riesgo medio: firewall).
3. **DAWN/802.11r** (riesgo alto: un error deja APs sin wifi → modo rescate
   con fallback a config previa).
4. **Batman-adv** (el último: riesgo de bucles en mesh; dry-run obligatorio
   + ventana de confirmación con auto-rollback).

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

## Convención de trabajo

Cada ítem de esta hoja de ruta se materializa como issue en GitHub antes de
tocar código, y se cierra con el PR que lo resuelve (ver convención
issue→PR acordada el 2026-08-03).
