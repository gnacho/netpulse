# Beacon protocol v1.1 - embedded switches (RTLPlayground)

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

## Guardia de bucles (firmware)

El RTL8372 no tiene STP; la guardia lo suplanta a nivel de firmware:

1. Cada tick (30 s), al construir el beacon, comparar el FDB con el
   snapshot del tick anterior: una MAC que cambia de puerto = mac-move.
2. TRIGGER de bucle: la misma MAC vista moviéndose entre 2 puertos
   >= 3 veces en <= 90 s, o (si el presupuesto de RAM lo permite) la
   misma MAC presente en 2 puertos en el mismo tick.
3. ACCIÓN: deshabilitar el puerto MÁS NUEVO del par (registro de enable
   del RTL8372, el mismo que usa la web), emitir `loop` + `port_disabled`,
   y programar re-enable a los 5 min. Máximo 3 auto-recuperaciones por
   puerto; a la cuarta queda down hasta intervención manual (comando
   consola/web o NetPulse).
4. RAM: el snapshot previo son ~30 MACs × 7 B ≈ 210 B __xdata. Si no
   cabe, fallback: heurística de tormenta (delta de RxGood por puerto
   por encima de un umbral en 2 ticks) con la misma ACCIÓN.

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
