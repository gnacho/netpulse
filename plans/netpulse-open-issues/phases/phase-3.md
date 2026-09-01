---
type: planning
entity: phase
plan: netpulse-open-issues
phase: 3
status: completed
created: 2026-09-01
updated: 2026-09-01
---

# Phase 3: Tuning de SNMP y ghost ports

> Part of [netpulse-open-issues](../plan.md)

## Objective

Reducir los falsos positivos "ghost port" en switches gestionados via SNMP (#414), permitiendo configurar el intervalo de polling SNMP y ajustar la logica de alertas.

## Scope

### Includes

- Anadir opcion configurable de intervalo de polling SNMP por router/switch (default conservador).
- Revisar logica de ghost-port para switches SNMP (contadores no siempre crecen con cada poll).
- Añadir histeresis o cooldown antes de emitir alerta ghost-port en interfaces SNMP.
- Exponer la configuracion en el formulario de edicion de router y en la API.
- Tests para la nueva logica.

### Excludes

- Reescribir el poller SNMP desde cero.
- Cambiar el comportamiento de ghost-port para routers OpenWrt (no SNMP).

## Prerequisites

- Funcionalidad SNMP ya en `main` (#309).
- Acceso al switch Mikrotik CSS610 para validar thresholds.

## Deliverables

- [x] Rama `fix/414-snmp-ghost-tuning`.
- [x] PR, revision y merge en `main` (PR #434).
- [x] Issue #414 cerrado.

## Acceptance Criteria

- [x] Se puede configurar intervalo de polling SNMP en la UI/API.
- [ ] Las alertas ghost-port en el Mikrotik CSS610 se reducen o desaparecen tras ajustar el intervalo/threshold. (pendiente de validacion en entorno real)
- [x] Tests verdes y build correcta.
- [ ] Validacion en CT 200 (donde esta el Reyee) o con el CSS610 del usuario. (pendiente de deploy)

## Dependencies on Other Phases

| Phase | Relationship | Notes |
|-------|-------------|-------|
| 2 | blocked-by | Para evitar mezclar cambios de deteccion de puertos con tuning SNMP. |

## Notes

- Revisar `server-go/internal/adapters/snmp_live.go` y `server-go/internal/snmp/ifTable.go`.
- Posible relacion con #309: asegurar que los contadores se tratan como acumulativos y que los saltos de 32/64 bits no generan spikes.
