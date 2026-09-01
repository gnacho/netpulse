---
type: planning
entity: phase
plan: netpulse-open-issues
phase: 1
status: completed
created: 2026-09-01
updated: 2026-09-01
---

# Phase 1: Quick wins y changelog en updater

> Part of [netpulse-open-issues](../plan.md)

## Objective

Cerrar #309 (ya implementado) e implementar #404 para que el asistente de actualizacion muestre el changelog del release antes de aplicar.

## Scope

### Includes

- Cerrar issue #309 como completado en GitHub.
- Extender el dialogo/assistante de actualizacion del frontend para mostrar el cuerpo del release de GitHub o la seccion de CHANGELOG.md correspondiente.
- Endpoint backend que sirva el changelog del release disponible (reutilizar release-check o GitHub API).
- Manejo de errores: si no se puede cargar el changelog, mostrar enlace al release y dejar continuar.

### Excludes

- Cambios en la logica de descarga/aplicacion de updates.
- Mejoras visuales del asistente mas alla del contenido del changelog.

## Prerequisites

- `main` estable en v2.23.5.
- Acceso a la API de GitHub para obtener el release body.

## Deliverables

- [ ] Issue #309 cerrado.
- [ ] Rama `feat/404-update-changelog` con backend y frontend.
- [ ] PR, revision y merge en `main`.
- [ ] Tests actualizados o nuevos para el endpoint de changelog.

## Acceptance Criteria

- [ ] Al abrir el asistente de update, se ve el cuerpo de las notas del release o un enlace si falla la carga.
- [ ] `go test ./...`, `npm run build` y `npm run lint` verdes.
- [ ] #404 cerrado con referencia al PR.

## Dependencies on Other Phases

| Phase | Relationship | Notes |
|-------|-------------|-------|
| Ninguna | - | - |

## Notes

- Se puede reutilizar `server-go/internal/updater` si ya consulta releases de GitHub.
- El release body ya esta en ingles/espanol; mostrarlo tal cual.
