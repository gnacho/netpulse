# SPEC — Fase 8: App on-box (servidor en el router gateway)

> Estado: borrador de diseño (issue #17). Actualizada: 2026-08-06.
> Objetivo: NetPulse corre en el propio router gateway, sin dependencia de
> infraestructura externa (CT, servidor). Solo lectura; la escritura queda en
> la Fase 9.

## 0. Contexto verificado (no supuestos)

- **Gateway real**: GL.iNet (firmware GL sobre OpenWrt 24.10), 1 GB RAM
  (~344 MB libres), overlay 7.2 GB con ~6.9 GB libres. El server-go pesa
  ~22 MB RSS; el agente ~12 MB. Presupuesto del nodo ≈ 35 MB de ~344 MB ✅.
- **Binario del servidor**: `server-go` ya es un binario único con la app
  embebida (`go:embed` del dist), config por `.env` (fail-fast) y sondeo SSH
  por pool. Goreleaser ya emite amd64/arm64/armv7 → el build on-box no
  requiere cross-compile nuevo, solo un target de empaquetado OpenWrt.
- **Agente**: ya empaquetado como `.ipk` (opkg, gateway) y manual en los APs
  (apk 25.12 pendiente). Lleva UCI config (`/etc/config/netpulse-agent`),
  procd init y watchdog. Su cliente TLS usa `NETPULSE_INSECURE_TLS=1` →
  `InsecureSkipVerify` (R5 del auditoría, pendiente de retirar).
- **Nada on-box existe hoy en server-go** (grep `onbox|procd|uci` no
  encuentra código de servidor).

## 1. Retos de diseño (los que el ROADMAP no decide)

### R1 — Actualización on-box
El patrón actual es `git pull` + `update.sh` (script que vive en el CT con
git, CI de assets y swap atómico). En un router no hay git ni CI local.

**Decisión**: empaquetar el servidor como paquete OpenWrt (`.ipk` 24.10 y
`.apk` 25.12) con el **mismo mecanismo de assets que el agente**: el binario
viene dentro del paquete; las actualizaciones son `opkg/apk upgrade` del
paquete. La app NO necesita conocer su propia versión desplegada más allá de
lo que ya expone `/api/health`; el updater in-app de la webapp queda **no
funcional on-box** (fuera de alcance: las actualizaciones las gestiona el
gestor de paquetes del router, no la app).

Alternativa descartada: binario + flag `.restart-me` como en el CT. Requeriría
distribuir el binario por HTTP desde GitHub y validar checksums en el router,
duplicando infraestructura que ya resuelve opkg/apk.

### R2 — TLS autofirmado validado sin `--insecure`
El servidor on-box escucha HTTPS con un cert autofirmado generado en primer
arranque. El agente debe hablarle **sin** `InsecureSkipVerify`.

**Decisión**: pinning por fingerprint (SPKI hash) del cert del servidor,
configurado en UCI del agente durante el pairing. Sustituye a
`NETPULSE_INSECURE_TLS`: 
- `netpulse-agent.main.server` sigue siendo la URL (`https://192.168.1.1:3000`).
- Nueva opción `netpulse-agent.main.server_fp` = SHA-256 del DER de la clave
  pública (SPKI) del cert del servidor, en hex (`sha256sum` estándar).
- El agente configura `tls.Config{InsecureSkipVerify:false}` +
  `RootCAs`/`VerifyPeerCertificate` que valida el SPKI contra `server_fp`.
  Sin fingerprint configurado → el agente **falla** (fail-closed), nunca
  degrada a insecure.
- Se elimina `NETPULSE_INSECURE_TLS` en esta fase.

Cómo se obtiene el fingerprint: el servidor lo expone en un endpoint sin
auth (`GET /fingerprint` → hex SPKI) y en el log de primer arranque. El
pairing (R3) lo entrega automáticamente al agente.

### R3 — Pairing cero-fricción
Hoy el agente se instala con `--clave=valor` (token en argv, R8 de la
auditoría). On-box el servidor y el agente viven en la misma máquina y hay
que poder adoptar APs nuevos desde el móvil.

**Decisión**:
- El servidor on-box tiene un **token de pairing** de un solo uso (UUID
  regenerado en cada instalación, guardado en UCI `/etc/config/netpulse`,
  sección `server`). Se muestra en `logread` al primer arranque y en el
  paquete LuCI de la Fase 10.
- Al añadir un AP: se introduce ese token en la UI (o en el `.config` del
  agente) → el servidor valida el token, genera `server`+`server_fp`+`token`
  de agente y los escribe en el UCI del agente vía SSH (canal existente) o
  vía el asistente de adopción.
- Fuera de alcance on-box: el pairing automático por mDNS/LLDP (queda para
  fase futura).

### R4 — Bootstrap AUTH_PASS sin fail-fast en router
`config.go` exige `AUTH_PASS` (fail-fast). En un router no se puede pedir una
contraseña por consola.

**Decisión**: en modo on-box (`NETPULSE_ONBOX=1` en UCI del servidor), si no
hay `AUTH_PASS` se genera una aleatoria (26 chars) en el primer arranque, se
guarda en UCI (`netpulse.server.auth_pass`, 0600 vía uci) y se imprime una
sola vez en `logread`. El bootstrap del admin (`EnsureUsers`) la usa igual que
hoy. El cambio en `config.go` es: `AUTH_PASS` opcional si `ONBOX` y valor
presente en UCI; el loader UCI (R5) la inyecta en el entorno antes de `Load`.

### R5 — Config por UCI en el servidor
Hoy `config.go` lee entorno/`.env`. On-box se gestiona con UCI.

**Decisión**: pequeño cargador `internal/config/uci.go` que traduce la
sección `/etc/config/netpulse` (`config server`) a variables de entorno con
las mismas claves (`port`, `data_dir`, `auth_user`, `auth_pass`,
`demo_mode`, `cookie_secure`, `github_repo`…) antes de `Load`. El resto del
código no cambia. En el CT (no on-box) se sigue usando `.env` como hoy.

### R6 — Persistencia fuera de NAND
- `data_dir` apunta a **overlay** por defecto (el gateway tiene 6.9 GB
  libres; no hay USB conectado de serie). `journal_mode=DELETE` (no WAL) para
  no desgastar flash: se activa con `PRAGMA journal_mode=DELETE` al abrir
  (hoy usa WAL por defecto en modernc/sqlite).
- Si se detecta un USB montado (`/mnt/netpulse-data`), `uci set
  netpulse.server.data_dir=/mnt/netpulse-data` lo prioriza. La DB es un
  fichero normal; migrar = copiar fichero + chown.
- El resto de datos (estado del agente local, SSE) vive en `/tmp` y se
  reconstruye en arranque.

## 2. Modos de operación

`NETPULSE_ONBOX=1` (UCI) activa: config UCI (R5), bootstrap AUTH_PASS (R4),
TLS autofirmado (R2). Sin la variable, el binario se comporta exactamente
como hoy (CT): `.env`, `:3000` http, sin pairing.

Un AP (sin WAN) solo corre el agente (ya es así hoy). El gateway corre
server + agente. Detección automática del rol: si la sección UCI `server`
existe y está habilitada → hub; si no → solo agente. No hay heurística de
interfaces (demasiado frágil en firmware GL).

## 3. Despliegue y empaquetado

- Nuevo `deploy/openwrt/netpulse-server/` (espejo del de `netpulse-agent`):
  Makefile + `netpulse.init` (procd) + `netpulse.config` (UCI `config server`)
  + `netpulse.defaults` (1er arranque: fingerprint, AUTH_PASS, cert).
- El binario del paquete = build arm64 de goreleaser (o build onbox con
  `NETPULSE_ONBOX` embebido — ver §4). ~14 MB.
- SDK: 24.10 para `.ipk` (gateway); 25.12 para `.apk` (futuro, misma deuda
  que el agente).
- Reutilizar `package.sh` del agente parametrizado con el paquete nuevo.

## 4. Cambios de código resumen

| Área | Cambio |
|---|---|
| `internal/config` | `AUTH_PASS` opcional si ONBOX; `uci.go` loader R5 |
| `cmd/netpulse/main.go` | detectar ONBOX; TLS server con cert autofirmado; `/fingerprint`; pairing |
| `internal/staticspa` | servir HTTPS tras TLS (sin cambio de lógica) |
| `agent/internal/push` + `sseclient` | validación SPKI por `server_fp`; eliminar `INSECURE_TLS` |
| `deploy/openwrt/netpulse-server/` | paquete nuevo (init/config/defaults) |
| `.goreleaser.yaml` / CI | target on-box arm64 (mismo binario; ONBOX por UCI, no por build) |

**Decisión de build**: un único binario; `NETPULSE_ONBOX` es configuración,
no compilación. Evita duplicar builds en CI y permite probar on-box en el CT
(pasar el flag).

## 5. Criterios de aceptación (verificables)

1. Con el CT apagado: `https://<IP-del-router>:3000` sirve la app y hace
   login (usuario admin con la AUTH_PASS generada).
2. El agente del gateway y los APs reportan al servidor on-box **con TLS
   validado por fingerprint** (sin `INSECURE_TLS`). Verificación:
   `logread | grep netpulse-agent` sin errores de cert; `iwinfo`/heartbeat
   frescos en `/api/agents`.
3. Reinicio del gateway: server y agente arrancan solos (procd), DB y últimas
   24 h intactas, `journal_mode=DELETE` confirmado (`PRAGMA journal_mode`).
4. Adopción de un AP nuevo con el token de pairing sin escribir tokens en
   argv.
5. Sin ONBOX: el binario en el CT funciona idéntico a hoy (regresión: suite
   de tests + smoke local).

## 6. Fuera de alcance (explícito)

- Web Push (SW del navegador no valida cert autofirmado).
- Pairing automático por mDNS/LLDP.
- Actualización in-app on-box (la gestiona opkg/apk).
- Escritura en red (Fase 9).
- El paquete LuCI (Fase 10) — aunque R3/R4 diseñan su superficie.
- `.apk` del servidor para los APs 25.12 (misma deuda que el agente; los APs
  solo corren el agente, no el servidor).

## 7. Orden de implementación sugerido

1. `internal/config/uci.go` + AUTH_PASS opcional ONBOX (testeable en el CT).
2. TLS autofirmado + `/fingerprint` + pinning en el agente (sustituye
   INSECURE_TLS).
3. Pairing por token (server) + superficie en webapp (botón "Adoptar AP").
4. Paquete `netpulse-server` (Makefile/init/config/defaults) + build arm64.
5. Prueba real en el gateway y validación de criterios 1-5.
6. Rollout: los APs apuntan `server` al gateway (solo UCI).
