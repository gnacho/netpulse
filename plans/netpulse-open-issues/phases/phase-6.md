---
type: planning
entity: phase
plan: netpulse-open-issues
phase: 6
status: pending
created: 2026-09-01
updated: 2026-09-01
---

# Phase 6: Release v2.24.0 y deploy

> Part of [netpulse-open-issues](../plan.md)

## Objective

Empaquetar todos los cambios en release v2.24.0, actualizar CT 226 y la demo online.

## Scope

### Includes

- Actualizar `CHANGELOG.md` con los cambios de las fases 1-5.
- Bump de version a v2.24.0 en `server-go/internal/httpapi/server.go`, `app/package.json`, `app/package-lock.json`.
- Crear tag v2.24.0 y esperar CI.
- Actualizar CT 226 via updater in-app.
- Verificar `/api/health`.
- Actualizar demo online (`web/` y/o app demo segun corresponda).
- Cerrar issues de seguimiento/release si existen.

### Excludes

- Cambios de ultima hora no incluidos en las fases anteriores.

## Prerequisites

- Fases 1-5 completadas y mergeadas.
- CI verde en `main`.

## Deliverables

- [ ] Tag v2.24.0.
- [ ] CT 226 en v2.24.0 con health OK.
- [ ] Demo online actualizada.

## Acceptance Criteria

- [ ] `gh release view v2.24.0` existe y CI es success.
- [ ] `curl http://192.168.1.226:3000/api/health` devuelve version v2.24.0 y ok:true.
- [ ] `curl -I https://demo.netpulse.cloudless.club/` devuelve HTTP/2 200.
- [ ] Todos los issues del plan cerrados.

## Dependencies on Other Phases

| Phase | Relationship | Notes |
|-------|-------------|-------|
| 5 | blocked-by | No se puede release hasta que todo este mergeado. |

## Notes

- Incluir en el release body el changelog para que el updater lo muestre (#404).
- Seguir la memoria de deploys anteriores para CT 226 y demo.
