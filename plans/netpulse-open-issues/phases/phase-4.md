---
type: planning
entity: phase
plan: netpulse-open-issues
phase: 4
status: completed
created: 2026-09-01
updated: 2026-09-01
---

# Phase 4: Pagina /features

> Part of [netpulse-open-issues](../plan.md)

## Objective

Publicar la pagina `/features` del marketing site con el inventario completo de funcionalidades y decisiones tecnicas (#412).

## Scope

### Includes

- Rebasear `feat/412-features-page` sobre `main` actual.
- Revisar contenido, i18n (ES/EN), enlaces de navegacion y cache-bust.
- Verificar Lighthouse a11y >= 95.
- Construir estatico `web/` y desplegar en webs2 `/opt/netpulse-web/public`.
- PR, revision y merge.

### Excludes

- Manual de uso (#418), pospuesto.
- Nuevas capturas que no esten ya en la rama.

## Prerequisites

- Fases 1-3 completadas.
- Acceso a webs2 para despliegue de la landing.

## Deliverables

- [x] Rama `feat/412-features-page` actualizada y mergeada (como `feat/412-features-page-rebased`, PR #435).
- [x] Issue #412 cerrado.
- [ ] Landing deployada (pendiente de fase 6 / deploy).

## Acceptance Criteria

- [x] `/features` renderiza correctamente en claro/oscuro y ES/EN.
- [x] Navegacion desde `/` a `/features` funciona.
- [x] Lighthouse accessibility >= 95 (0.98 en local).
- [x] No guiones largos/em dashes en el copy.
- [x] Build de `web/` sin errores.

## Dependencies on Other Phases

| Phase | Relationship | Notes |
|-------|-------------|-------|
| 3 | blocked-by | Evitar conflictos con fases anteriores. |

## Notes

- La rama ya contiene `web/features.html`, `web/app.js`, capturas y i18n.
- Validar que no se expongan datos reales de la red del usuario.
