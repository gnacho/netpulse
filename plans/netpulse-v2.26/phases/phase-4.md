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
- [x] Validación manual en un router no crítico (RT4 / redmi-ax6-3, 2026-09-02).

## Deployment Notes

- Desplegada preview en CT 226 (192.168.1.226) el 2026-09-02: versión `v2.26.0-preview`, health OK, endpoints de firmware responden.
- Validación manual completada en RT4 (redmi-ax6-3):
  - Dummy E2E (URL 404): comando SSE recibido, descarga fallida y resultado reportado/persistido como `failed`.
  - Upgrade REAL con imagen ASU oficial 25.12.5 (rebuild con paquetes actualizados, sha256 verificado): descarga → verify → `sysupgrade -b` → flash → reboot → reporte post-arranque → `status=done` en BD y `currentVersion` actualizada. Sin intervención manual en el flujo del agente.

### Aprendizajes de la validación (2026-09-02)

- `/etc/sysupgrade.conf` SOLO conserva ficheros dentro de `/etc`: listar `/usr/sbin/...` no sirve, sysupgrade los ignora. Tras un flash, el binario del agente y el watchdog se pierden y procd entra en crash loop (init y `.env` en `/etc` sí sobreviven).
- Recuperación post-flash verificada: `GET /api/agents/{slug}/binary?arch=arm64` (auth Bearer con el token del propio agente, que sobrevive en el `.env`) sirve el binario 2.26.0-preview embebido; tras copiarlo a `/usr/sbin` el agente arranca, consume `.firmware-upgrade-pending` y reporta el resultado diferido. Hueco de producto encolado como issue.
- Bug del watchdog corregido: BusyBox `pgrep -x` compara la línea de comando completa, no el nombre base; con el binario en `/usr/sbin/netpulse-agent` nunca matcheaba y el cron lo reiniciaba cada 2 min. Sustituido por `pidof` (commit en la rama del PR #456).
- Fix server-side necesarios encontrados en la validación (ya en el PR #456): `FinishedAt` nullable en store, inyección de `now` en `NewLive`, comando SSE como objeto JSON (no base64) y exención Bearer para `firmware-progress`/`firmware-result`.

## Dependencies

- #451 (patrón apply/health/rollback) ya estaba implementado y se reutiliza el flujo de snapshot + health + rollback conceptual para el backup/restore de config.
