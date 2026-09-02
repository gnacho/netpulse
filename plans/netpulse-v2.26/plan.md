---
type: planning
entity: plan
plan: netpulse-v2.26
status: active
created: 2026-09-02
updated: 2026-09-02
---

# Plan: netpulse-v2.26

## Problem / Context

Tras el release v2.25.0 quedan mejoras estructurales pendientes, algunas sugeridas tras comparar NetPulse con OpenWISP y oonfeeWRT, y un bug crítico en el agente del gateway (#448). El objetivo es avanzar hacia una gestión más completa de routers OpenWrt sin perder la ligereza de NetPulse.

## Target Outcome

- Bug #448 resuelto y mergeado.
- Tres mejoras funcionales implementadas y validadas:
  1. Apply seguro con rollback y ownership UCI (#451).
  2. Channel planning / radio inventory (#452).
  3. Firmware upgrades de routers OpenWrt (#453).
- Release v2.26.0 publicado con CHANGELOG actualizado.

## Guiding Decisions & Constraints

- **Sin reemplazar el agente Go**: las mejoras se implementan en el agente/servidor actuales.
- **Issue → rama → PR → merge** para cada cambio.
- **Preview/CT 226**: se despliega manualmente durante desarrollo; producción se actualiza vía release + updater.
- **Dependencias**: #452 puede hacerse sin #451 si la primera versión solo muestra recomendaciones; la aplicación automática requiere #451.
- **#453 firmware** es la más compleja; se prioriza tras #451 para reutilitar el patrón de apply/rollback.

## Scope

### In Scope

- #448: panic en `iwevents` del agente del gateway.
- #451: apply seguro de config OpenWrt con rollback.
- #452: channel planning y radio inventory.
- #453: firmware upgrades de routers.
- CHANGELOG y release v2.26.0.

### Out of Scope

- Integración completa con OpenWISP u oonfeeWRT (solo ideas adaptadas).
- RADIUS/captive portal, multi-tenancy o IPAM.
- Mesh dinámico (OLSR/BATMAN).

## Definition of Done

- [ ] #448 cerrado.
- [ ] #451, #452, #453 cerrados y mergeados.
- [ ] `go test ./...`, `npm run build` y `npm run lint` verdes en `main`.
- [ ] `CHANGELOG.md` refleja v2.26.0.
- [ ] Tag `v2.26.0` creado y CI verde.
- [ ] CT 226 actualizado y `/api/health` OK.

## Phases

| Phase | Title | Contribution | Detail | Status |
|-------|-------|--------------|--------|--------|
| 1 | Bugfix agente gateway #448 | Cerrar panic en `iwevents` | [Phase 1](phases/phase-1.md) | completed |
| 2 | Apply seguro con rollback | Implementar #451 | [Phase 2](phases/phase-2.md) | in_progress |
| 3 | Channel planning | Implementar #452 | [Phase 3](phases/phase-3.md) | pending |
| 4 | Firmware upgrades | Implementar #453 | [Phase 4](phases/phase-4.md) | pending |
| 5 | Release v2.26.0 | Empaquetar y desplegar | [Phase 5](phases/phase-5.md) | pending |

## Risks & Open Questions

| Risk/Question | Impact | Mitigation |
|---------------|--------|------------|
| #453 requiere hardware real para validar sysupgrade. | Bloqueo si no hay router de prueba | Reservar un router no crítico; test unitario con sysupgrade simulado. |
| #451 puede romper configuración manual del usuario. | Alto | Ownership tags + rollback + preview obligatorio. |
| #452 requiere scan WiFi que no siempre funciona en todos los drivers. | Medio | Detectar ausencia de scan y mostrar gap. |

## Changelog

### 2026-09-02

- Plan creado con 5 fases.
- Issues #451, #452, #453 creados.
