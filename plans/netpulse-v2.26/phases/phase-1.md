---
type: planning
entity: phase
plan: netpulse-v2.26
phase: 1
status: pending
created: 2026-09-02
updated: 2026-09-02
---

# Phase 1: Bugfix agente gateway #448

> Part of [netpulse-v2.26](../plan.md)

## Objective

Diagnosticar y corregir el panic periódico en el listener de eventos `iwevents` del agente Go en el gateway.

## Scope

### Includes

- Reproducir el panic con logs reales o con un test que simule el final del loop de `iwevents`.
- Arreglar la aserción de tipo inválida sobre `cmd.Stderr`.
- Añadir test de regresión.

### Excludes

- Rediseño del listener; solo fix del panic.

## Deliverables

- [ ] PR con fix y test.
- [ ] Validación en el gateway real: 10 min sin panic.

## Acceptance Criteria

- [ ] `go test ./...` en `agent/` pasa.
- [ ] Logs del gateway no muestran panic tras el despliegue.

## Dependencies

- Ninguna.
