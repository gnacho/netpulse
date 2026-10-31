# Beacon en switches Realtek

[English version](RTL-SWITCH-BEACON.md)

NetPulse puede recibir estado de bocas, contadores y la tabla de reenvío MAC directamente desde un switch Realtek compatible que ejecute RTLPlayground. El switch envía datagramas UDP a NetPulse, sin agente Linux ni scraper intermedio.

Esta vía exige una instalación avanzada de firmware. El beacon de NetPulse se mantiene en el [fork gnacho/RTLPlayground](https://github.com/gnacho/RTLPlayground), no en el RTLPlayground original. Instalarlo sustituye el firmware del switch.

## Qué recibe NetPulse

Con un build reciente del beacon, NetPulse puede mostrar:

- Estado y velocidad negociada de cada boca cada 30 segundos.
- Contadores acumulados de tramas y bytes, con tasas calculadas por el servidor.
- Errores por boca cuando el firmware incluye los campos `ie` y `oe`.
- Tabla de reenvío MAC cada 300 segundos.
- Versión de firmware, MAC de gestión y uptime cuando el build los incluye.

El beacon no aporta CPU, RAM ni temperatura del switch. Tampoco transporta una tabla de vecinos LLDP. La emisión LLDP y el tratamiento RMA son funciones de configuración del switch documentadas en la guía del firmware.

Hoy un dispositivo alimentado por beacon no puede usar SNMP como segunda fuente en NetPulse. El SNMP opcional del firmware puede utilizarse de forma independiente, pero no añade otra fuente al mismo dispositivo de NetPulse.

## Requisitos

- Un switch web-managed RTL8372 o RTL8373 soportado y su perfil `MACHINE` exacto. Consulta la [lista de dispositivos soportados](https://github.com/logicog/RTLPlayground/blob/main/doc/supported_devices.md).
- Un método para respaldar y restaurar la flash original. RTLPlayground recomienda una pinza SOIC para recuperación. Una actualización fallida puede impedir que el switch arranque.
- Conectividad de red desde el switch hasta el listener UDP de NetPulse.
- Un slug de agente único. El slug se compila en el firmware y debe coincidir con el agente registrado en NetPulse.

Sigue la [guía de firmware y beacon](https://github.com/gnacho/RTLPlayground/blob/netpulse/beacon-octets/doc/beacon.md) para el build reproducible, la instalación del firmware, la configuración de bocas y la referencia del datagrama. La guía usa un tag fijo y distingue los campos añadidos en commits posteriores.

## 1. Activa el listener UDP

Define la dirección del listener en el entorno del servidor NetPulse y reinicia el servidor mediante tu proceso habitual de despliegue:

```sh
NETPULSE_BEACON_LISTEN=:5140
```

Un valor vacío desactiva el listener. Permite UDP 5140 entrante desde el switch en cualquier firewall intermedio.

## 2. Registra el agente

Crea un router de tipo externo en NetPulse y después genera su token de agente mediante la API de administración:

```text
POST /api/agents
Content-Type: application/json

{"slug":"edge-switch"}
```

El endpoint requiere una sesión de administrador y devuelve el token. Crear o rotar un token invalida el anterior.

Usa el mismo identificador `edge-switch` en los dos literales de slug compilados en `beacon.c` antes de construir el firmware. No reutilices el slug de otro switch.

## 3. Configura el switch

Desde la consola de comandos de RTLPlayground, define la dirección de NetPulse y el token del agente:

```text
beacon 192.0.2.10 <agent-token>
```

Sustituye `192.0.2.10` por la dirección del servidor NetPulse. Este comando inicia el envío periódico de estado y FDB. Admite dos argumentos porque el slug ya está compilado en el firmware.

Comandos útiles:

```text
beacon                 # muestra el estado
beacon fdb             # muestra si el envío FDB está activo
beacon fdb off         # desactiva FDB en runtime
beacon fdb on          # activa FDB en runtime
beacon off             # detiene el beacon
```

Para anunciar el switch por LLDP sin reenviar tramas LLDP reservadas a otras bocas, la guía del firmware documenta además:

```text
lldp on
rma 0e drop
```

Guarda la configuración de arranque con el mecanismo normal de RTLPlayground. Las ramas actuales del beacon incluyen los arreglos de guardado en flash. La guía del firmware las distingue de los builds alpha antiguos.

## Seguridad

El beacon v1 envía el token en claro y no firma los datagramas UDP. Úsalo solo en una LAN o red de gestión de confianza. No expongas UDP 5140 a redes no confiables ni a internet.

NetPulse valida el hash del token, limita peticiones por IP de origen y registra cambios de secuencia sospechosos. Estos controles no cifran el tráfico.

## Verificación y diagnóstico

1. Comprueba con `beacon` que el switch tiene la dirección esperada de NetPulse.
2. Confirma que el slug compilado coincide con el agente registrado y que el token no ha sido rotado.
3. Comprueba que UDP 5140 alcanza el listener de NetPulse.
4. Busca mensajes `[netpulse:beacon]` en el log del servidor. Ahí se registran datagramas malformados, tokens inválidos y anomalías de secuencia.
5. Espera el siguiente datagrama de estado de 30 segundos. Las actualizaciones FDB llegan cada 300 segundos cuando están activas.

El datagrama FDB tiene un presupuesto de tamaño fijo. Que falte una MAC en una actualización no demuestra que el cliente haya desaparecido.

El servidor web de RTLPlayground atiende una conexión cada vez. Evita peticiones paralelas contra su consola. Esto no afecta al envío UDP del beacon.
