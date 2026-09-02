---
type: planning
entity: phase
plan: netpulse-v2.26
phase: 2
status: completed
created: 2026-09-02
updated: 2026-09-02
---

# Phase 2: Apply seguro de configuración OpenWrt con rollback (#451)

> Part of [netpulse-v2.26](../plan.md)

## Objective

Permitir a NetPulse aplicar cambios de configuración en routers OpenWrt de forma segura, con rollback automático y sin pisar configuración manual del usuario.

## Scope

### Includes

- Ownership tags para secciones UCI gestionadas por NetPulse.
- Backup previo de las secciones afectadas en el agente.
- `uci apply` con rollback window.
- Verificación post-apply y confirmación/revert.
- Endpoint backend y UI de preview/diff.

### Excludes

- Cambios complejos de VLAN/firewall multi-WAN en esta fase.
- Aplicación automática sin aprobación manual.

## Deliverables

- [x] Cambios en agente Go para ejecutar apply/rollback.
- [x] Endpoint backend `/api/orchestr/apply`.
- [x] UI de preview con diff y botón "Aplicar" (ya existente; ahora respeta ownership).
- [x] Tests de agente y backend.

## Acceptance Criteria

- [x] Test unitario de apply exitoso y rollback por timeout.
- [x] Test que rechaza cambios en secciones no gestionadas.
- [x] Validación manual en router no crítico.

## Dependencies

- Ninguna.
