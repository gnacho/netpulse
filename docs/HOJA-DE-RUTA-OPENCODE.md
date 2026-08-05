# Hoja de ruta operativa para opencode — NetPulse

> Versión: 2026-08-05 (v2.4.4). Repo: `~/Documentos/Mi Nube/Proyectos/repos/netpulse-git`.
> Ejecutar desde opencode en ese repo. Antes de cada fase, leer `docs/ROADMAP.md`,
> `docs/AUDITORIA-FASE65.md` y este fichero.

## Estado de partida

- Producción en CT 226 (`192.168.1.226:3000`), versión `v2.4.4`.
- Fases 1-5 terminadas y desplegadas.
- Fase 6 incompleta: HMAC, binario del agente, HTTPS y cluster Proxmox.
- Fases 7-9 definidas en `docs/ROADMAP.md` pero no iniciadas.
- Convenciones de trabajo: `reglas.md` (opencode memory), `docs/ROADMAP.md` §Convención.

## Reglas para opencode

1. **Nunca usar `sudo` desde el agente**. Para root usar `pkexec` agrupado en
   una sola acción y explicar QUÉ y POR QUÉ.
2. **Para Docker**: `sg docker -c "..."`.
3. **Cada cambio de código** → commit convencional + push a `main`.
4. **Cada bug/feature** → issue en GitHub antes de tocar código, cerrar con el PR.
5. **Desplegar versiones nuevas** sin pedir confirmación, pero narrando los pasos.
6. **Tests obligatorios antes de push**:
   - Frontend: `cd app && npm run lint && npm run build`.
   - Backend: `export PATH=$HOME/.local/go/bin:$PATH && cd server-go && go test ./...`.
7. **Bump de versión** (cuando se va a releasear): 4 sitios
   (`server-go/internal/httpapi/server.go`, `app/package.json`,
   `app/package-lock.json`, `docs/ROADMAP.md`). Luego tag `vX.Y.Z`.
8. **Deploy** sigue `docs/ROADMAP.md` o el skill `netpulse-ops`.

---

## Fase 6 — TLS, endurecimiento y cierre de fixes

**Meta:** que el agente sea seguro, el servidor sirva HTTPS, los agentes se
reinstalen desde el propio servidor, y el cluster Proxmox se identifique.
Sin esto no se puede avanzar a Fase 7/8/9.

### 6.1 HMAC-SHA256 en la ingesta del agente

**Riesgo:** cualquiera con la URL puede inyectar datos falsos.
**Solución:** firmar el payload JSON con el token compartido.

Pasos:
1. En `server-go`, añadir middleware/validador en la ruta de ingesta del
   agente (buscar dónde recibe `POST` del agente; probablemente en
   `internal/httpapi/` o `internal/adapters/`). Rechazar si falta cabecera
   `X-Agent-Signature` o si no coincide `HMAC-SHA256(token, body)`.
2. En `agent/` (Go), calcular la firma antes de enviar y añadir la cabecera.
3. Asegurar que el token no viaje en URL (ya debería estar en env/cabecera).
4. Tests: unitario en server-go con payload válido e inválido.
5. Añadir en `docs/AGENTE-OPENWRT.md` la nueva cabecera.

Criterio de aceptación:
- Un POST sin firma devuelve 401.
- Un POST con firma correcta se ingiere.
- El agente oficial sigue funcionando tras reinstalar.

### 6.2 Servir el binario del agente desde el servidor

**Riesgo:** el `install-agent.sh` descarga de GitHub y pasa el token en argv.
**Solución:** endpoint `GET /api/agents/{slug}/binary?arch=...` sirve el
binario correcto firmado por el servidor.

Pasos:
1. Incluir los binarios del agente (o al menos los targets habituales:
   `mipsel-softfloat`, `aarch64`, `x86_64`) en el release CI o en
   `server-go/assets/agents/`.
2. Crear endpoint autenticado/admin que sirva el binario según `slug` y `arch`.
3. Generar un script de instalación reducido que:
   - Descarga el binario del servidor con autenticación de admin.
   - Lo instala en `/usr/bin/netpulse-agent`.
   - Crea procd service y UCI config.
   - Pide/adopta el token de emparejamiento.
4. Marcar `install-agent.sh` antiguo como fallback para desarrollo.
5. Actualizar `docs/AGENTE-OPENWRT.md` y el skill `netpulse-ops` si procede.

Criterio de aceptación:
- Desde un router se puede ejecutar el nuevo install y obtener el binario del
  servidor sin GitHub.
- El token no aparece en `ps` ni en argv.

### 6.3 HTTPS en CT 226

**Bloqueo:** Web Push no funciona sin secure context; los agentes no pueden
validar un cert autofirmado sin `--insecure`.

Opciones a decidir:
- **A) Caddy + cert autofirmado**: Caddy delante de server-go en CT 226.
  El navegador añade excepción la primera vez. El agente valida el cert
  con fingerprint pre-ship o usa HMAC (no hace falta TLS para la firma, pero sí
  para confidencialidad).
- **B) Tailscale Serve**: si hay salida a internet, `tailscale serve` da HTTPS
  automático. Los routers deben estar en la misma tailnet. Es más fácil pero
  añade dependencia externa.

Pasos comunes:
1. Elegir A o B y documentar la decisión en `docs/SEGURIDAD-TLS.md`.
2. Si se elige A, añadir Caddy al CT, cert autofirmado, redirección 80→443, y
   proxy inverso a `server-go` en localhost:3000.
3. Actualizar `install.sh` del servidor para generar/renovar cert.
4. Asegurar que `NETPULSE_INSECURE_TLS` pueda retirarse.
5. Deploy y verificar `/api/health` en `https://192.168.1.226`.

Criterio de aceptación:
- Web Push se puede suscribir desde un navegador en la red local.
- El agente se comunica con el servidor sin `--insecure`.

### 6.4 Identificación de Proxmox en cluster

**Problema actual:** la regla de topología requiere exactamente un host por
puerto de switch. En un cluster Proxmox hay 2 hosts (citadel-01/02), por lo que
el puerto se etiqueta como `inferred`.

Ubicación a investigar:
- `server-go/internal/adapters/topo_semantics.go` y `topology.go`.
- Búsqueda clave: OUI de hypervisor (`BC:24:11` para Proxmox), `vmMAC`,
  `hostMACs`, `DistributionNode`, `hypervisor`.

Pasos:
1. Revisar el FDB real del switch para ver cómo se aprenden las MACs de los
   hosts Proxmox y sus CTs/VMs. Preguntar/verificar si cada host tiene su
   propio puerto o si comparten uno.
2. Si cada host está en su propio puerto, revisar por qué no se detecta
   (quizá la MAC del host no está en la base de datos o la regla falla).
3. Si comparten puerto, decidir estrategia:
   - Opción A: crear un nodo `hypervisor-cluster` que agrupa múltiples hosts y
     sus CTs/VMs (el frontend necesitaría saber pintarlo).
   - Opción B: detectar por API de Proxmox (corosync, API pve) para asignar
     cada VM/CT a su host.
   - Opción C: configuración manual del usuario: declarar los hosts del
     cluster y dejar que el servidor asigne las VMs por IP/subnet.
4. Implementar la opción elegida.
5. Añadir tests con datos de FDB simulados para cluster de 2 hosts.
6. Actualizar ROADMAP y documentar la decisión.

Criterio de aceptación:
- En producción, el mapa de topología muestra el cluster Proxmox con sus hosts
  y CTs correctamente anidados, o documenta por qué no es posible sin API externa.

### 6.5 Cierre de deuda cosmética

Si tras v2.4.4 queda algo (layout, traducciones, iconos), resolverlo aquí.
No iniciar nuevas features cosméticas.

---

## Fase 7 — Agente a fondo

**Meta:** convertir el agente de sondeo cada 15 s en componente de red a
 tiempo real, empaquetable como `.ipk`.

Orden recomendado:
1. **Eventos ubus** (`hostapd`, `netifd`, `udhcpc`) → push inmediato al servidor.
2. **netlink/nl80211** para FDB/ARP/stations sin parsear CLI.
3. **Comunicación bidireccional** (WebSocket/SSE) para que el servidor pueda
   enviar órdenes al agente (preparar Fase 9).
4. **Empaquetado `.ipk`** con Makefile OpenWrt.
5. **Profiling** en Flint 2 y APs, ajuste de intervalos.

Criterio de aceptación:
- Cliente wifi aparece en el dashboard < 3 s después de `assoc`.
- Agente consume < 5 % CPU en AP y < 10 MB RSS.
- `opkg install netpulse-agent` funciona.

---

## Fase 8 — App embebida en routers

**Meta:** NetPulse vive en el gateway (Flint 2), no en un CT externo.

Orden recomendado:
1. **Modo on-box de server-go**: build reducido, UCI config, procd service,
   detección automática gateway vs AP.
2. **Persistencia externa**: SQLite en USB/overlay, WAL desactivado, backup
   diario.
3. **TLS local**: cert autofirmado + flujo de confianza, o Tailscale funnel.
4. **Pairing cero-fricción**: token de emparejamiento visible en LuCI/log.
5. **`luci-app-netpulse`** (opcional): página de gestión básica.

Criterio de aceptación:
- Con el CT 226 apagado, `https://192.168.1.1` sirve NetPulse.
- Se puede adoptar un AP nuevo sin editar env files.
- Gateway sobrevive a reinicio con datos de 24 h.

---

## Fase 9 — Escritura/orquestación

**Meta:** pasar de leer a actuar sobre la red de forma segura y reversible.

Reglas innegociables:
- Plan → apply → state; diff antes de aplicar.
- `uci` transaccional; snapshot antes de apply; rollback si falla healthcheck.
- Ejecutor con allowlist estricta; sin shell libre.
- HMAC firmado en todas las órdenes de escritura.

Orden por riesgo/beneficio:
1. AdGuard Home (bajo riesgo).
2. WireGuard peers (riesgo medio).
3. DAWN/802.11r (riesgo alto; modo rescate obligatorio).
4. Batman-adv (riesgo muy alto; dry-run + auto-rollback).

---

## Checklist antes de empezar cada subtarea

- [ ] ¿Hay un issue de GitHub para esta subtarea?
- [ ] ¿He leído los ficheros relevantes (ROADMAP, AUDITORIA, AGENTE, código)?
- [ ] ¿He ejecutado tests/lint y pasan?
- [ ] ¿He hecho commit+push con mensaje convencional?
- [ ] ¿He desplegado en CT 226 y verificado `/api/health`?

## Comandos de referencia rápida

```bash
# Tests
export PATH=$HOME/.local/go/bin:$PATH
cd ~/Documentos/Mi\ Nube/Proyectos/repos/netpulse-git/app
npm run lint && npm run build
cd ../server-go
go test ./...

# Deploy en CT 226
ssh root@192.168.1.100 'pct exec 226 -- su -s /bin/bash netpulse -c "bash /opt/netpulse/deploy/update.sh"'

# Verificar
curl -k https://192.168.1.226/api/health | python3 -m json.tool
```
