---
type: planning
entity: phase
plan: netpulse-v2.26
phase: 5
status: pending
created: 2026-09-02
updated: 2026-09-02
---

# Phase 5: Release v2.26.0 y deploy

> Part of [netpulse-v2.26](../plan.md)

## Objective

Empaquetar todas las fases anteriores en release v2.26.0 y desplegarlo.

## Scope

### Includes

- Actualizar `CHANGELOG.md`.
- Bump de versión en `server-go/internal/httpapi/server.go`, `app/package.json`, `app/package-lock.json`.
- Crear tag `v2.26.0` y release.
- Desplegar en CT 226 y validar `/api/health`.

### Excludes

- Cambios de última hora.

## Deliverables

- [ ] Tag `v2.26.0`.
- [ ] CT 226 actualizado con health OK.
- [ ] Todos los issues del plan cerrados.

## Acceptance Criteria

- [ ] CI verde en `main`.
- [ ] `curl http://192.168.1.226:3000/api/health` devuelve versión v2.26.0 y `ok:true`.

## Dependencies

- Fases 1-4 completadas y mergeadas.
