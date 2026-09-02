---
type: planning
entity: phase
plan: netpulse-v2.26
phase: 4
status: pending
created: 2026-09-02
updated: 2026-09-02
---

# Phase 4: Firmware upgrades de routers OpenWrt (#453)

> Part of [netpulse-v2.26](../plan.md)

## Objective

Permitir actualizar el firmware de routers OpenWrt desde la interfaz de NetPulse, con verificación de compatibilidad y health post-upgrade.

## Scope

### Includes

- Modelo de datos de firmware soportado por router (modelo, release, URL/binario).
- Endpoint backend para lanzar upgrade con checksum y espera de reinicio.
- UI con confirmación, progreso y resultado.
- Health check post-reinicio.
- Copia de seguridad de config antes del upgrade.

### Excludes

- Builds personalizados de OpenWrt.
- Upgrades masivos sin confirmación.
- Migración de config entre versiones mayores.

## Deliverables

- [ ] Backend: endpoints de firmware y flujo de upgrade.
- [ ] Agente: descarga, `sysupgrade`, espera y reporte.
- [ ] UI: botón "Actualizar firmware" con progreso.
- [ ] Tests unitarios y manuales.

## Acceptance Criteria

- [ ] Test de simulación del flujo completo.
- [ ] Validación manual en un router no crítico.

## Dependencies

- Recomendable tener #451 para reutilizar patrón de apply/health, aunque el firmware puede tener su propio flujo.
