---
type: planning
entity: phase
plan: netpulse-open-issues
phase: 5
status: completed
created: 2026-09-01
updated: 2026-09-01
---

# Phase 5: Asesor de re-anchor WiFi

> Part of [netpulse-open-issues](../plan.md)

## Objective

Implementar un asesor de colocacion de clientes WiFi y accion de re-anchor manual, soportando DAWN y usteer y priorizando usteer (#403).

## Scope

### Includes

- Rebasear `feat/403-wifi-reanchor` sobre `main`.
- Extender el backend para consumir mapa de usteer (`/api/usteer`) ademas de DAWN.
- Calcular recomendaciones por cliente: AP actual, senal, mejor alternativa, delta.
- Umbral minimo de senal recomendada y delta configurables (default -65 dBm y +10 dBm).
- Endpoint para listar recomendaciones y endpoint para ejecutar `del_client` con `ban_time`.
- UI en pagina Roaming con tarjeta de recomendaciones y boton "Mover".
- Tests para logica de recomendacion y constructor del comando SSH.

### Excludes

- Re-anchor automatico (Phase 2 del issue original); se deja para release futura.
- Modificar configuracion de roaming en routers.

## Prerequisites

- Fases 1-4 completadas.
- Red con usteer (casa u oficina) para validar.

## Deliverables

- [ ] Rama `feat/403-wifi-reanchor` rebasada y actualizada.
- [ ] PR, revision y merge en `main`.
- [ ] Issue #403 cerrado.

## Acceptance Criteria

- [ ] Backend devuelve recomendaciones para clientes suboptimos cuando hay usteer o DAWN disponible.
- [ ] UI muestra recomendacion con senales y boton "Mover a <AP>".
- [ ] Al aprobar, se ejecuta `del_client` correctamente y se informa del resultado.
- [ ] Tests verdes y build correcta.
- [ ] Validacion manual en red con usteer.

## Dependencies on Other Phases

| Phase | Relationship | Notes |
|-------|-------------|-------|
| 4 | blocked-by | La rama #403 es compleja; se deja para despues de los cambios mas simples. |

## Notes

- Reutilizar `server-go/internal/ssh/pool.go` para ejecutar `ubus call hostapd.<iface> del_client`.
- Reutilizar tipos y parsers de `server-go/internal/adapters/usteer.go` y `dawn.go`.
- Si usteer y DAWN coexisten, priorizar usteer.
