# Agente beacon en switches RTL8372/8373 (`beacon` embebido)

> Estado: **en producción en un equipo** (2026-08-27): keepLink KP-9000-9XHML-X
> flasheado con [RTLPlayground](https://github.com/logicog/RTLPlayground) + parche
> beacon, empujando estado cada 30 s y FDB cada 300 s al listener UDP de #291.
> El switch pasa a ser un agente más (`kind: external`), sin SSH, sin SNMP y sin
> máquina intermedia que lo scrapear.

## Por qué este agente

Estos switches (2.5G web-managed de ~40-60 €) llevan un RTL8372/RTL8373 con MCU
8051: **no hay SO**, no correrán nunca un agente Go ni OpenWrt. El firmware
libre RTLPlayground los convierte en switches gestionables de verdad (VLANs,
LAG, mirror, rate-limit, telemetría SFP) y su código C es lo bastante pequeño
para añadirle un beacon UDP a medida: el switch **empuja** su propio estado.

| | Scraper HTTP (fallback) | Beacon embebido |
|---|---|---|
| Requiere máquina intermedia | sí (cron/timer con curl) | no |
| Frescura | cada 5 min | **cada 30 s** |
| Datos | FDB + up/down de puertos | + contadores TX/RX y velocidad por puerto |
| Token | en un script del servidor | en la flash del switch (un solo consumidor) |

## Hardware soportado

La lista oficial vive en
[RTLPlayground: supported devices](https://github.com/logicog/RTLPlayground/blob/main/doc/supported_devices.md)
(Ampcom, Davuaz, FOXNEO, Hisource, Horaco, keepLink, LIANGUO, Mokerlink,
Ruiying, Sodola, Steamemo, TrendNet, Xikestore, Ztyuav...). Requisitos:

- SoC **RTL8372/RTL8373** (familia 2.5G; los RTL838x/839x de 1G van por
  OpenWrt y no necesitan esta guía).
- Versión **web-managed** (la "unmanaged" no tiene interfaz a la que flashear
  por red; requeriría clip SOIC-8).
- Idealmente un modelo ya probado en la lista; si no, identificar la revisión
  de PCB (serigrafía) y crear una `MACHINE_*` en `machine.c`.

## Flasheo (vía web, sin abrir la caja)

1. **Compilar la imagen** con el parche beacon:
   ```sh
   git clone https://github.com/gnacho/RTLPlayground -b netpulse-beacon
   cd RTLPlayground
   # config.txt: ip/gw/netmask del switch (se incrusta en la imagen)
   make MACHINE=<TU_MAQUINA>          # p.ej. KP_9000_9XHML_X_V3_1
   cd installer && make               # imagen de upgrade para web OEM
   ```
   Requiere `sdcc` >= 4.5, `gcc`, `xxd`, `json-c`. El resultado es
   `installer/output/rtlplayground_oem_upgrade.bin`.

2. **Subirla desde la web del firmware OEM**: pestaña de actualización de
   firmware, seleccionar el `.bin`, subir y esperar el reinicio (~2 min).
   Tras el arranque: IP la de `config.txt`, contraseña web `1234`.

3. **Configurar** (consola de comandos de la web, sección System):
   ```
   passwd <tu-contraseña>
   port N name <etiqueta>        # opcional, etiquetas de puertos
   beacon <ip-del-server-netpulse> <token>
   ```
   El token se genera al adoptar el agente en NetPulse (ver abajo). El comando
   `beacon` persiste en flash y auto-arranca en cada boot.

⚠️ **NO guardar la configuración con el botón "Save Settings to Flash"** en la
base v0.1-alpha de RTLPlayground: el path de guardado (`POST /config`) ha
matado el firmware en un equipo real y sigue bajo investigación (upstream). Los
comandos `beacon`/`passwd`/`port name` persisten por sí mismos; el resto de
cambios viven en RAM hasta reinicio (aceptable en esta clase de equipo).

## Lado NetPulse

1. Listener activo: `NETPULSE_BEACON_LISTEN=:5140` en el env del server.
2. Adoptar el agente en la UI/API (`POST /api/agents {slug}`) → token.
3. El ingest valida `sha256(token)` contra la kv, aplica rate-limit por IP y
   detecta saltos de `seq` (pérdida/reorden/replay).
4. El server reinyecta las labels del último push del scraper si el datagrama
   no las lleva (el beacon viaja mínimo a propósito).

## Contrato del datagrama (spec v1)

Un datagrama UDP por evento, JSON de una línea, sin timestamp (el server
estampa a la llegada; el 8051 no tiene RTC):

```json
{"v":1,"seq":4271,"slug":"switch16","token":"<token>",
 "ports":[{"n":1,"l":3,"tx":12345,"rx":678}, ...todos los puertos...]}
```

- Estado **cada 30 s**: `l` = código de link (0 down, 1 10M, 2 100M, 3 1G,
  4 500M, 5 10G, 6 2.5G, 7 5G), `tx`/`rx` = contadores acumulados de tramas
  good (uint32 decimal).
- FDB **cada 300 s** (experimental): `"fdb":{"AABBCCDDEEFF":"3", ...}` con MAC
  en uppercase sin separadores y **puerto 1-based** (mismo convenio que el
  scraper HTTP). ~26 B por entrada; tope del buffer a ~1800 B.
- `seq` es un contador compartido por ambos tipos con wrap uint16; **seq=1
  tras un salto = firma de reboot del switch** (útil como evento).

## Gotchas operativos

- **Web de 1 conexión**: el httpd del firmware sirve una petición HTTP a la
  vez (uIP + 8051). No martillear en paralelo; el beacon UDP no se ve afectado.
- **MAC de fábrica**: la definición de máquina debe llevar
  `mac_flash_offset = 0x1FC000`; sin él el firmware genera una MAC LAA del
  chip-UUID (funcional y estable, pero no la de la pegatina). Diagnóstico y
  restauración: comandos `flashdump 1FC000` y `setmac <mac>` del parche.
- **Si el firmware cuelga** (alpha): power-cycle físico. La config persiste;
  tras el arranque hay ventana amplia para reflashear por web si hiciera falta.
- El upload por curl a `/upload` devuelve conexión cerrada (`000`): es normal,
  el server corta al empezar a escribir flash y el proceso sigue.
- **STP**: la base lo incluye (`stp on`) pero la topología recomendada es árbol
  (un solo uplink). Sin STP, ningún camino redundante.

## Diagnóstico rápido

| Síntoma | Causa probable | Check |
|---|---|---|
| Agente no `fresh` | beacon sin configurar / token rotado | `beacon` en la consola web; journal del listener |
| `seq 1 tras N` en el log | reboot del switch | correlacionar hora; investigar corte de alimentación |
| Web no carga | navegador con 6-8 conexiones vs server de 1 | recargar; cerrar pestañas duplicadas |
| MAC distinta a la de la pegatina | `mac_flash_offset` sin definir | `flashdump 1FC000`, fix en `machine.c` |
