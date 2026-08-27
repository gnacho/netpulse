# Beacon protocol v1.2 - embedded switches

Canal UDP entre switches embebidos (8051 + uIP) y NetPulse. Tres tipos de
datagrama, todos JSON ASCII de una línea, mismo socket de destino
(`:5140` en el server), autenticados por el token del agente (salvo el
announce). El emisor no tiene RTC: el server estampa la hora de llegada.

## 1. Beacon periódico (unicast, pareado)

- Cada 30 s (fijo en código) hacia `COLLECTOR_IP:5140`.
- ~400-500 B, muy por debajo de UIP_BUFSIZE (2000).

```json
{"v":1,"seq":4271,"slug":"switch16","token":"<token>",
 "ports":[{"n":1,"l":3,"tx":12345,"rx":678}, ...todas las bocas...],
 "sfp":[{"n":9,"temp":34.5,"rxp":-12.1,"txp":-13.0}]}
```

- `v`: versión de esquema (1). `seq`: uint32 incremental con wrap.
- `token`: el MISMO token del agente (kv sha256 en el server).
- `ports`: SIEMPRE todas las bocas. `l`: 0 down · 1 10M · 2 100M · 3 1G ·
  4 500M · 5 10G · 6 2.5G · 7 5G. `tx`/`rx`: tramas good acumuladas (u32).
- `sfp`: opcional, solo bocas ópticas con DOM.

## 2. Announce de descubrimiento (broadcast, sin parar)

- Hasta estar pareado: broadcast `<subnet>.255:5140` cada ~60 s.
- Token vacío + identidad. NetPulse lo lista como candidato y el admin
  lo parea (slug + token + comando).

```json
{"v":1,"seq":7,"slug":"","token":"","dev":"KP-9000-9XHML-X",
 "fw":"rtlplayground-v0.1.0","ports":[...]}
```

## 3. Eventos (inmediatos, no esperan al tick)

Datagrama pequeño enviado EN CUANTO ocurre el hecho (mismo auth que el
beacon). `ev` distingue el tipo:

```json
{"v":1,"ev":"loop","port":5,"mac":"AABBCCDDEEFF","seq":900,
 "slug":"switch16","token":"<token>"}
```

- `loop`: bucle detectado (ver guardia abajo). `port` y `mac` implicados.
- `port_down` / `port_up`: cambio de link en `port` (instantáneo, mejor
  que esperar el delta del tick).
- `port_disabled`: la guardia ha deshabilitado `port` (auto-protección).
- `port_recovered`: re-enable tras cooldown.

## Datagrama FDB (v1.2, cada 5 min)

Tabla MAC completa en UN datagrama (~600 B con 30 MACs). Sustituye al
scraper como fuente de FDB: con él, todo el switch habla por el beacon
y el token tiene un único consumidor.

```json
{"v":1,"seq":4300,"slug":"switch16","token":"<token>",
 "fdb":{"AABBCCDDEE01":"3","AABBCCDDEE02":"8"}}
```

- Puertos como número sin prefijo ("3"): el server los alinea con las
  bocas (`lan3`) al construir el estado.
- `{}` (objeto vacío presente) = sin entradas: estado real. Ausencia del
  campo = este datagrama no toca la tabla.
- El server conserva las bocas del último beacon periódico: el datagrama
  FDB no viaja con ports y no borra nada.

## Guardia de bucles - APLAZADA (decisión 27-Ago-2026)

El RTL8372 no tiene STP, pero el mac-move es comportamiento LEGÍTIMO en
redes con roaming WiFi (DAWN mueve clientes entre los puertos de los
APs): un auto-disable tumbaría un AP en producción. Si algún día se
recupera la idea, la restricción es innegociable:

- SOLO alertar (`ev:"loop"`), JAMÁS deshabilitar puertos desde el
  firmware.
- Excluir del análisis las bocas de APs y uplink.
- Prefiere la heurística de tormenta (delta de RxGood) al mac-move.

Los eventos `port_disabled`/`port_recovered` quedan en la spec por si
otras integraciones los usan, pero este firmware no los emitirá.

## Comando de configuración (consola + Advanced Settings)

```
beacon <ip-netpulse> <slug> <token>     # pareo: unicast + token
beacon off                               # apaga beacon y announce
```

Puerto destino (5140) y cadencia (30 s / 60 s announce) fijos en código.

## Modelo de seguridad (decisión consciente v1)

LAN de confianza: token en claro, sin HMAC (el 8051 no compensa SHA256
por ahora). Mitigaciones: rate-limit por IP en el server, seq con wrap
para detectar replay/reorden (se loguea), announce sin credenciales.
Si algún día se endurece: HMAC truncado en un v2 con `v:2`.
