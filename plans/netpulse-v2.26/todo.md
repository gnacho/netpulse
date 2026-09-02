---
type: planning
entity: todo
plan: netpulse-v2.26
updated: 2026-09-02
---

# Todo: netpulse-v2.26

> Tracking [netpulse-v2.26](plan.md)

## Active Phase

- Ninguna: plan v2.26 completado.

## Pending

- [x] Phase 3: Channel planning (#452)
- [x] Phase 5: Release v2.26.0

## In Progress

- Nada.

## Completed

- [x] Crear plan v2.26 y issues #451, #452, #453.
- [x] Phase 1: bugfix agente gateway #448 (PR #454).
- [x] Phase 2: Apply seguro con rollback (#451).
- [x] Phase 4: Firmware upgrades (#453).
- [x] Validación manual del upgrade de firmware en RT4 (dummy 404 + upgrade ASU real E2E, 2026-09-02).
- [x] Phase 5: merge PR #456, fix CI #459, CHANGELOG, tag y release v2.26.0, deploy a producción CT 226 vía updater (health v2.26.0, 5 agentes).

## Blocked

- N/A

## Changelog

### 2026-09-02

- Plan creado.
- Issues #451, #452, #453 creados en GitHub.
- Backend, agente y UI de firmware upgrades implementados (#453).
- Desplegada preview del PR #456 en CT 226 (192.168.1.226); health OK y endpoints de firmware responden.
