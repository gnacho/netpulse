---
type: planning
entity: phase
plan: netpulse-open-issues
phase: 2
status: completed
created: 2026-09-01
updated: 2026-09-01
---

# Phase 2: Bugs de detección de puertos y dispositivos

> Part of [netpulse-open-issues](../plan.md)

## Objective

Diagnosticar y corregir los problemas de detección de puertos en BPI-R4 (#413), UniFi 6 Plus (#416) y dispositivos descubiertos por IP que quedan offline (#415).

## Scope

### Includes

- Análisis de logs y datos actuales de NetPulse para identificar por qué no se detectan ciertos puertos.
- Revisión del scraper/poller de puertos (`server-go/internal/adapters/ports.go` y similares).
- Correcciones para incluir WAN/SFP+ en BPI-R4 y eth0 en UniFi 6 Plus.
- Corrección del flujo "Discover on network" para que los dispositivos añadidos por IP se consideren online.
- Tests que cubran los casos corregidos.

### Excludes

- Cambios de arquitectura mayores en la detección de topología.
- Soporte para nuevos tipos de interfaz fuera de los reportados.

## Prerequisites

- Fase 1 completada y `main` estable.
- Acceso a los routers afectados o a logs/capturas del usuario.

## Deliverables

- [ ] Diagnóstico documentado en el issue o en la memoria.
- [ ] Rama `fix/ports-discovery-bugs` con los fixes.
- [ ] PR, revisión y merge en `main`.
- [ ] Issues #413, #415 y #416 cerrados.

## Acceptance Criteria

- [ ] BPI-R4 reporta correctamente puertos WAN (ETH/SFP+).
- [ ] UniFi 6 Plus reporta eth0.
- [ ] Dispositivos añadidos por "Discover on network" aparecen online en el dashboard.
- [ ] Tests verdes y build correcta.
- [ ] Validación en CT 226 (si los dispositivos están disponibles) o feedback del usuario confirmando el arreglo.

## Dependencies on Other Phases

| Phase | Relationship | Notes |
|-------|-------------|-------|
| 1 | blocked-by | Se necesita `main` estable. |

## Notes

- Los tres issues probablemente comparten raíces en la normalización de nombres de interfaces o en el filtrado de puertos "físicos" vs virtuales.
- Si no se puede reproducir en CT 226, se pedirá al usuario que ejecute comandos de diagnóstico en los routers afectados.
