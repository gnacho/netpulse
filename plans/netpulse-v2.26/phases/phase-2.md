---
type: planning
entity: phase
plan: netpulse-v2.26
phase: 2
status: in_progress
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

- [ ] Cambios en agente Go para ejecutar apply/rollback.
- [ ] Endpoint backend `/api/orchestr/apply`.
- [ ] UI de preview con diff y botón "Aplicar".
- [ ] Tests de agente y backend.

## Acceptance Criteria

- [ ] Test unitario de apply exitoso y rollback por timeout.
- [ ] Test que rechaza cambios en secciones no gestionadas.
- [ ] Validación manual en router no crítico.

## Dependencies

- Ninguna.
