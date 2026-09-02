---
type: planning
entity: phase
plan: netpulse-v2.26
phase: 4
status: completed
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

- [x] Backend: endpoints de firmware y flujo de upgrade.
- [x] Agente: descarga, `sysupgrade`, espera y reporte.
- [x] UI: botón "Actualizar firmware" con progreso.
- [x] Tests unitarios y manuales.

## Acceptance Criteria

- [x] Test de simulación del flujo completo.
- [ ] Validación manual en un router no crítico.

## Deployment Notes

- Desplegada preview en CT 226 (192.168.1.226) el 2026-09-02: versión `v2.26.0-preview`, health OK, endpoints de firmware responden. Pendiente probar upgrade real en router no crítico.

## Dependencies

- #451 (patrón apply/health/rollback) ya estaba implementado y se reutiliza el flujo de snapshot + health + rollback conceptual para el backup/restore de config.
