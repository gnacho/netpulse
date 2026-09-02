---
type: planning
entity: phase
plan: netpulse-v2.26
phase: 3
status: completed
created: 2026-09-02
updated: 2026-09-02
---

# Phase 3: Channel planning y radio inventory (#452)

> Part of [netpulse-v2.26](../plan.md)

## Objective

Añadir una vista de planificación de canales que recomiende el canal óptimo por radio a partir de scans de vecinos.

## Scope

### Includes

- Agente recoja scan de vecinos (`iwinfo scan` o equivalente) en el payload WiFi.
- Backend exponga scans agregados.
- UI `/wifi/channel-plan` con radios propias, APs vecinos y recomendación.
- Aplicación manual de la recomendación (usando #451 si está listo, o cambio manual vía orchestración).

### Excludes

- Cambio automático de canal sin confirmación.
- Soporte de 6 GHz si el agente no lo reporta.

## Deliverables

- [x] Agente: recolección de scan en payload WiFi.
- [x] Backend: endpoints para scans y recomendaciones.
- [x] UI: tabla/visualización de channel plan.
- [x] Tests.

## Acceptance Criteria

- [x] Test del agente incluye scan simulado.
- [x] UI muestra recomendación coherente.
- [ ] Validación manual en 2 APs físicos (pendiente de laboratorio).

## Dependencies

- Opcionalmente #451 para aplicar cambios; la fase puede entregarse solo lectura primero.
