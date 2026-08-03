# Auditoría NetPulse v2.2.0 — Fase 6 + 6.5
> Fecha: 2026-08-03 · Versión auditada: v2.2.0 (commit ee0b1c2, release pública
> verificada con 9 assets + checksums) · Desplegada y verificada en CT 226.
> Alcance: riesgos, fallos, mejoras, decisiones de diseño y orientación hacia
> la Fase 7 (app embebida en routers OpenWrt) y Fase 8 (escritura/orquestación).

## 0. Qué se desplegó (contexto de la auditoría)

| Bloque | Contenido | Estado verificado |
|---|---|---|
| Fase 6 — agente piloto | `netpulse-agent` (binario stateless, 6,1 MB arm64), `POST /api/ingest/agent`, tokens por equipo, badge Agente, fallback SSH | Build CI OK (amd64/arm64/armv7); `GET /api/agents` responde `[]` en prod (ningún agente adoptado aún) |
| Fase 6 — Web Push | VAPID en kv, tabla `push_subscriptions`, Notifier asíncrono, SW `injectManifest`, tarjeta Notificaciones en Ajustes | `GET /api/push/vapid-key` devuelve clave real en prod |
| Fase 6 — alertas por categorías | Motor con 6 categorías × 3 niveles (none/urgent/all), dedup 5 min, cap 100 eventos | Config por defecto servida correctamente |
| Fase 6.5 — view-model | `vm: 1` en overview, `demo-canon.json` single-source, `Device.infra`, topología semántica, `displayName`, `/api/system/info` | `vm:1` + `system/info` OK en demo y prod; `/api/devices` con `infra` poblada en demo |

Pruebas: `go vet` limpio, `go test ./...` 8 paquetes OK, agente `go test` OK,
`tsc --noEmit` 0 errores, build PWA con `injectManifest` (16 entradas precache),
E2E demo local (login → overview → agents → push → alerts config).

## 1. Incidente de seguridad durante la integración (RESUELTO)

El bundle de Kimi traía commiteada por error **una clave privada SSH**
(`app/data/.ssh/id_ed25519`, commit 5f7a74c) junto a una BD demo. Gitleaks lo
detectó al pushear el tag. Acciones tomadas:

1. `git filter-repo --invert-paths --path app/data/` → historia reescrita,
   force-push (historial del repo público purgado).
2. Verificado que la clave pública filtrada (`AcXJe...`) **NO está en
   authorized_keys de ninguno de los 4 routers** ni coincide con la clave de
   producción (`netpulse-ct226`, `KGSB/cKc...`). Impacto real: nulo, pero el
   repo es público (AGPL) y había que purgar igualmente.
3. Falso positivo (`body.Keys.P256dh` en push.go) añadido a `.gitleaksignore`.
4. CI gitleaks verde en main y v2.2.0 tras el fix.

**Lección**: los bundles de terceros se filtran SIEMPRE por gitleaks local
antes del push. El commit 5f7a74c decía "retirar artefactos de runtime
comiteados por error" pero solo retiró la BD, no la clave — la purga
parcial se quedó en el historial.

## 2. Fallos y riesgos detectados (por severidad)

### 🔴 R1 — Web Push inoperativo sin HTTPS (limitación de navegador, no del código)
El código push es correcto (RFC 8291 con descifrado aes128gcm testeado), pero
**el CT 226 sirve NetPulse en `http://192.168.1.226:3000` sin TLS**. Los
navegadores solo permiten Service Workers y Push API en *secure contexts*
(HTTPS o localhost). Consecuencia: la tarjeta Notificaciones de Ajustes podrá
guardar la preferencia, pero `pushManager.subscribe()` fallará en cualquier
cliente real de la LAN.
**Soluciones** (orden de preferencia): (a) Caddy delante con certificado
autofirmado + confianza manual, (b) Tailscale Funnel/Serve (HTTPS automático),
(c) documentar "usa localhost/127.0.0.1" como único secure context (pésima UX).
Esto condiciona la Fase 7: si la app vive en el router, el origen será
`https://192.168.x.1` con cert autofirmado → mismo problema; decidir antes.

### 🟠 R2 — La versión del agente no se inyecta en el build
`.goreleaser.yaml` usa `ldflags: [-s, -w]` sin `-X main.Version={{.Version}}`.
El agente reportará `0.1.0` en cada push para siempre, y el badge de versión
en la UI mentirá. Fix de una línea en goreleaser (solo para el build
`netpulse-agent`, que tiene su propio módulo con `main.Version`).

### 🟠 R3 — El one-liner de instalación del agente depende de internet
`agentInstallLine()` genera `curl ... raw.githubusercontent.com ... | sh`. En
una LAN sin salida (o con la salida caída) la adopción de agentes se bloquea.
El servidor YA tiene el binario del agente embebido en la release; servirlo
desde `GET /api/agents/{slug}/binary?arch=arm64` (o un asset estático)
eliminaría la dependencia externa y sería coherente con el modelo
self-hosted. Además evitaría el `| sh` con el token en la línea de comandos
(visible en `ps` y en el historial de root del router durante segundos).

### 🟠 R4 — Ingesta sin HMAC/firma del payload
`POST /api/ingest/agent` valida Bearer + rate limit (30/min/IP) + body cap
(2 MB) + regex de slug — correcto para el piloto. Pero el payload JSON se
acepta tal cual: un agente comprometido (o cualquiera con el token) puede
inyectar topología falsa (dispositivos inventados, health falseados). Para la
Fase 8 (escritura) esto es crítico: el mismo canal de confianza se usará para
órdenes al revés. **Decisión a tomar**: firmar el payload con el propio token
(HMAC-SHA256) cuesta ~10 líneas y cierra la puerta a replay/alteración con
captura del token en tránsito (HTTP plano en LAN).

### 🟡 R5 — `NETPULSE_INSECURE_TLS=1` es un agujero documentado
Necesario hoy (no hay TLS, ver R1), pero con R4 resuelto y HTTPS en el
servidor debería desaparecer o degradarse a warning log. No dejarlo como
camino permanente: token + TLS-sin-verificar = el token viaja "protegido" por
una cifra que no verifica a nadie.

### 🟡 R6 — overview no lleva `devices` completo
`/api/overview` expone `topDevices` (subset) y `vm`, pero la lista completa
solo está en `/api/devices`. No es un bug (es diseño), pero el frontend debe
conocer el contrato: cualquier feature futura que quiera "todos los
dispositivos con infra" desde overview fallará silenciosamente. Documentar en
ARCHITECTURE.md o añadir `devices` opcional.

### 🟡 R7 — El agente es pull-disfrazado-de-push a 15 s
El intervalo por defecto es 15 s (configurable). El discurso del agente es
"eventos al instante vía ubus", pero el piloto actual es sondeo local + push
periódico: la latencia mejora de 5 s (poll SSH) a ~15 s + jitter... es decir,
**el piloto no mejora la latencia de eventos**, solo quita carga SSH. La
suscripción ubus (hostapd assoc/disassoc en tiempo real) es lo que daría el
salto real — está en el diseño (AGENTE-OPENWRT.md) pero no implementada.
Esto es el corazón de la Fase 7: sin eventos push reales, el agente es un
ahorro de CPU del servidor, no una nueva capacidad.

### 🟢 R8 — Detalles menores
- `install-agent.sh` escribe `/etc/netpulse-agent.env` chmod 600 ✓, pero el
  token también pasa por argv → `ps aux` lo ve. Preferible: `--token -` (leer
  de stdin) o fichero temporal.
- El registry de agentes (`s.agents`) es solo memoria: un reinicio del
  servidor pierde `lastSeen` hasta el siguiente push (aceptable, documentar).
- `agentSlugRe` permite crear tokens para slugs que no existen como routers →
  huérfanos posibles. La UI debería listar solo routers conocidos al adoptar.

## 3. Mejoras recomendadas (valor/esfuerzo)

1. **HTTPS en CT 226** (Caddy, 20 min) — desbloquea Web Push de verdad (R1).
2. **`-X main.Version` en goreleaser para el agente** (5 min, R2).
3. **Servir el binario del agente desde el propio servidor** (1 h, R3) —
   además es el primer paso hacia la Fase 7: el router descarga su propio
   agente del gateway sin internet.
4. **HMAC en la ingesta** (1 h, R4) — barato ahora, carísimo tras la Fase 8.
5. **Eventos ubus reales en el agente** (Fase 7 núcleo, R7) — assoc/disassoc
   instantáneos, nl80211 por estación. El probe ya existe; falta el listener.
6. **Retención de series + informe semanal de disponibilidad** (deuda ya
   anotada en la auditoría v2.1.0; sigue sin hacerse).

## 4. Orientación Fase 7 — "app embebida en routers OpenWrt"

El agente ya es la semilla correcta (6,1 MB, stateless, procd, nada en flash).
Para convertir NetPulse en una app *del router*:

- **Arquitectura objetivo**: el Flint 2 (gateway, 8 GB RAM) corre
  `netpulse-agent` + opcionalmente un `netpulse-server` reducido (modo
  "on-box": misma base de código, flag de build sin frontend pesado o con el
  dist ya embebido). Los APs solo agent. El dashboard se sirve desde el
  gateway: la URL de la app ES la IP del router.
- **Presupuesto**: el server-go hoy usa 20 MB RSS (medido en CT 226) y 14,4 MB
  de binario — en un MT6000 con 8 GB sobra; en un AP con 512 MB el server no
  debe correr nunca (solo agent).
- **Riesgo NAND**: el server con SQLite WAL en flash de router es inviable
  (escritura continua). Si el server corre en el gateway, la DB debe ir en
  un volumen montado (USB/eMMC/overlay con tmpfs para las series calientes).
  El agente ya es stateless por diseño — no heredar ese error.
- **Descubrimiento cero**: el agente debe auto-registrarse con un token de
  "pairing" mostrado en el LuCI del gateway (patrón HomeKit/Tailscale auth
  key), no con curl|sh manual.
- **Bloqueante previo**: decidir HTTPS (R1). Una app servida por el router sin
  TLS no tendrá push, y la PWA queda coja. Certificado autofirmado por LUCI al
  primer arranque + botón "confiar" = el camino.

## 5. Orientación Fase 8 — escritura/orquestación (AdGuard, DAWN, WireGuard, Batman)

Pasar de leer a *actuar* multiplica el riesgo. Reglas de diseño propuestas:

1. **Nunca escritura directa**: toda orden pasa por un "plan" (qué se va a
   ejecutar, diff de config, confirmación) + log de auditoría inmutable
   (tabla nueva, append-only). Patrón Terraform: plan → apply → state.
2. **Idempotencia obligatoria**: cada orquestador (adguard-install,
   dawn-setup, wg-peer) debe ser re-ejecutable sin efectos dobles; el estado
   se *declara* (como Ansible), no se *acumula*.
3. **Rollback primero**: antes de aplicar, snapshot de la config afectada
   (`uci export`, backup del config de AdGuard Home). Sin rollback verificable
   no se aplica. OpenWrt tiene `uci` transaccional — usarlo, no sed sobre
   ficheros.
4. **Escalón de confianza**: el canal agente debe firmar (R4) ANTES de que
   viaje ninguna orden por él. Orden sin firmar = cualquier cosa en la LAN
   con el token puede reconfigurar el gateway.
5. **Por servicio** (orden sugerido por riesgo/beneficio):
   - **AdGuard Home**: el más fácil — paquete .deb/binario + config YAML +
     DNS en dnsmasq (`uci set dhcp.@dnsmasq[0].server=...`). Ya existe el
     módulo de AdGuard en la app (stats); la escritura es el paso natural.
   - **WireGuard peers**: `wg` + `uci network` + firewall zones. Beneficio
     alto (alta de peer desde el móvil), riesgo medio (tocar firewall).
   - **DAWN/roaming**: instalar 802.11r + dawn en los APs y coordinar
     `umdns`/key. Riesgo alto: un error deja los APs sin wifi → exigir
     "modo rescate" (fallback a config previa si dawn no arranca en N s).
   - **Batman-adv**: el más peligroso (mesh sobre la LAN física puede crear
     bucles). Dejarlo para el final con dry-run obligatorio y ventana de
     confirmación de 60 s con auto-rollback.
6. **Permisos**: el agente en el router corre como root hoy (necesario para
   leer netlink/ubus). Para escritura, separar: agente-lector (root mínimo) +
   un ejecutor de planes con allowlist estricta de comandos (nada de shell
   libre). El servidor manda el plan; el router solo ejecuta lo allowlisteado.

## 6. Veredicto

v2.2.0 es una release sólida: tests verdes, paridad live verificada, y las dos
piezas grandes (agente + push) están bien diseñadas aunque **dormidas en
producción** (ningún agente adoptado; push sin TLS no puede suscribirse).
El incidente de la clave filtrada se cerró con impacto nulo pero obliga a la
regla de gitleaks-pre-push para bundles externos.

Prioridades inmediatas: HTTPS (desbloquea push), versión del agente en
goreleaser, binario del agente servido localmente. El resto es Fase 7/8, donde
las decisiones de R1 y R4 son bloqueantes y deben tomarse antes de escribir
una sola línea de orquestación.
