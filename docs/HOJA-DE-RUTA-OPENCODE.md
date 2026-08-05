# Hoja de ruta operativa para opencode — NetPulse

> Versión: 2026-08-05 (v2.5.0). Repo: `~/Documentos/Mi Nube/Proyectos/repos/netpulse-git`.
> Ejecutar desde opencode en ese repo. Antes de cada fase, leer `docs/ROADMAP.md`,
> `docs/AUDITORIA-FASE65.md` y este fichero.

## Estado de partida

- Producción en CT 226 (`192.168.1.226:3000`), versión `v2.5.0`.
- Fases 1-6 terminadas y desplegadas (Fase 6 = HMAC + binario agente desde server).
- Fase 7 definida en `docs/ROADMAP.md` pero no iniciada.
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
