# Discovery zero-touch v1 - servidor y autoenroll (#367)

Mecanismo por el que un NetGrip recién instalado (agente NetPulse embebido,
sin config) encuentra un servidor NetPulse en la LAN y se da de alta solo.
Reutiliza el mismo socket UDP del listener de beacons (`:5140` en el server,
ver BEACON.md), así que no abre puertos nuevos.

## 1. Probe (broadcast, desde NetGrip)

NetGrip emite este datagrama en broadcast (por interfaz, `.255` de cada
subred + `255.255.255.255`) cuando su agente NO está conectado (sin config
válida, o server caído y sin pushes aceptados en 10 min). Cadencia 30 s.

```json
{"v":1,"type":"netgrip-probe"}
```

- Puerto destino: el del listener de beacons del server (5140 por defecto).
- El lado NetGrip puede overridearlo con `NETPULSE_DISCOVERY_PORT` en
  `/etc/netgrip/netpulse.env` (deployes con el beacon en otro puerto).
- Servers viejos sin discovery ignoran el probe en silencio (su parser de
  beacons no conoce el campo `type`).

## 2. Respuesta (unicast, desde el server)

El server contesta UNICAST a la IP del prober con su URL HTTP real. La IP
sale de un socket UDP conectado hacia el prober (en hosts multi-homed es la
IP de la interfaz correcta); el puerto es el HTTP de la config (3000).

```json
{"v":1,"type":"netpulse-server","url":"http://192.168.1.226:3000",
 "autoenroll":true,"pairing_token":"<uuid>"}
```

- `autoenroll`: refleja el flag `AGENT_AUTOENROLL` del server (default off).
- `pairing_token`: SOLO presente con `autoenroll:true`. Es el token de alta
  de red, distinto del pairing token de admin.
- Rate limit por IP origen, igual que la ingesta.

## 3. Autoenroll (alta sin intervención)

Con el token de alta de red, NetGrip llama al pairing existente:

```
POST /api/agents/pair   {"pairing_token":"<uuid>","slug":"<hostname>"}
```

- Slug: hostname del router saneado (`^[a-z0-9][a-z0-9-]{0,63}$`, fallback
  `netgrip`). Si el slug existe (409 slug_taken) reintenta con sufijos
  `-2`..`-5` antes de rendirse hasta el siguiente ciclo.
- El token de red SOLO puede crear agentes NUEVOS: nunca rota ni suplanta
  el token de un slug existente (409 si está ocupado).
- Respuesta 201: `{slug, token, server_fp}`; NetGrip persiste la config en
  `/etc/netgrip/netpulse.env` y el agente empieza a pushear.
- NetGrip NO se re-enrolla si ya está conectado al mismo server que
  descubre; contra un server DISTINTO solo migra si el configurado lleva
  10 min sin aceptar pushes (caso IP cambiada).

## 4. Flags de config (server)

| Variable | Default | Efecto |
|---|---|---|
| `AGENT_AUTOENROLL` | `0` | `1` = el responder incluye token de alta y `/api/agents/pair` lo acepta para slugs nuevos. |
| `NETPULSE_BEACON_LISTEN` | `:5140` | Socket UDP compartido beacons + discovery. |

El token de alta rota solo a las 24 h (rotación perezosa en lectura: el
token previo muere en el instante del cambio).

## 5. Modelo de seguridad

LAN de confianza (misma decisión consciente que el beacon #291): el token
viaja en claro, solo hacia la IP del prober, y únicamente autoriza crear
agentes nuevos. Un atacante de la LAN puede auto-dar de alta routers, no
suplantar los existentes; la rotación de 24 h acota la ventana de un token
filtrado.
