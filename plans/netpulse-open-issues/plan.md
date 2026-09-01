---
type: planning
entity: plan
plan: netpulse-open-issues
status: active
created: 2026-09-01
updated: 2026-09-01
---

# Plan: netpulse-open-issues

## Problem / Context

Tras cerrar v2.23.5 quedan 9 issues abiertos en `gnacho/netpulse`. Algunos son bugs concretos de deteccion de puertos/descubrimiento de dispositivos, otros son mejoras de UX (#404), funcionalidad WiFi (#403) y contenido web (#412/#418). El objetivo es cerrarlos de forma coordinada, minimizando rebases y despliegues parciales.

## Target Outcome

Todos los issues del alcance cerrados y mergeados en `main`, con tests verdes, build correcta y despliegue validado en CT 226 y demo online.

## Guiding Decisions & Constraints

- **No despliegues manuales en produccion salvo validacion explicita**: CT 226 se actualiza via updater in-app; la demo se despliega con build estatico.
- **Issue → rama → PR → merge**: cada cambio sigue el flujo estandar del repo.
- **v2.23.5 acaba de salir**: la siguiente release que agrupe estos cambios sera **v2.24.0** (salvo que el usuario decida otra cosa).
- **#403**: migrar/redisenar para soportar DAWN y usteer, priorizando usteer cuando este disponible, ya que la infraestructura real del usuario usa usteer.
- **#418**: pospuesto explicitamente por el usuario; no entra en este plan.
- **#309**: ya esta implementado en `main`; se cierra como completado sin mas codigo.

## Scope-Bounding Assumptions

- Los bugs de puertos (#413, #416) y descubrimiento (#415) se podran reproducir/diagnosticar con datos del entorno real del usuario o con logs que ya existen.
- Para #414 se asume que el usuario puede confirmar el intervalo de polling deseado y validar el comportamiento en su Mikrotik CSS610.

## Requirements

### Functional

- [ ] Cerrar #309 como completado.
- [ ] #404: el asistente de actualizacion muestra el changelog/release notes del release ofrecido.
- [x] #403: endpoint backend que recomienda re-anchor de clientes WiFi; accion manual de aprobacion; compatible con DAWN y usteer.
- [ ] #412: pagina `/features` publicada, con contenido completo y enlaces de navegacion.
- [ ] #413, #415, #416: corregir deteccion de puertos WAN/SFP+ y dispositivos descubiertos por IP.
- [ ] #414: permitir configurar el intervalo de polling SNMP y reducir falsos positivos ghost-port en switches SNMP.

### Non-Functional

- [ ] `go test ./...`, `npm run build` y `npm run lint` verdes antes de cada merge.
- [ ] Build de landing (`web/`) sin regresiones ni em/en dashes.
- [ ] Actualizar `CHANGELOG.md` y version bump para la release agrupada.

## Scope

### In Scope

- #309 (cierre administrativo), #403, #404, #412, #413, #414, #415, #416.
- Rama `feat/412-features-page`: rebase, ajustes y PR.
- Rama `feat/403-wifi-reanchor`: rebase, adaptacion a usteer, PR.

### Out of Scope

- #418 (manual how-to): pospuesto por el usuario.
- Cambios en el agente OpenWrt salvo los necesarios para los issues del alcance.
- Mejoras no relacionadas con los issues abiertos.

## Definition of Done

- [ ] Todos los issues del alcance estan cerrados en GitHub.
- [ ] Todos los PRs estan mergeados en `main`.
- [ ] `CHANGELOG.md` refleja los cambios agrupados.
- [ ] Tag v2.24.0 creado y CI verde.
- [ ] CT 226 actualizado a v2.24.0 y `/api/health` OK.
- [ ] Demo online (`demo.netpulse.cloudless.club`) actualizada y responde 200.

## Testing Strategy

- Tests automaticos: `go test ./...` (backend/agente), `npm run build` y `npm run lint` (frontend), build estatico de `web/`.
- Validacion manual en CT 226 para #403, #413, #415, #416, #414.
- Validacion visual/demo para #412.
- Verificacion del updater in-app para #404 en CT 226 tras el release.

## Phases

| Phase | Title | Contribution | Why Separate | Detail | Status |
|-------|-------|--------------|--------------|--------|--------|
| 1 | Quick wins y changelog en updater | Cerrar #309 e implementar #404 | Cambios pequenos y listos para merge inmediato | [Phase 1](phases/phase-1.md) | completed |
| 2 | Bugs de deteccion de puertos y dispositivos | Resolver #413, #415, #416 | Requieren diagnostico y posiblemente logs del entorno real | [Phase 2](phases/phase-2.md) | completed |
| 3 | Tuning de SNMP y ghost ports | Resolver #414 | Depende de la funcionalidad SNMP ya en main | [Phase 3](phases/phase-3.md) | completed |
| 4 | Pagina /features | Resolver #412 | Trabajo de marketing site separado del backend | [Phase 4](phases/phase-4.md) | completed |
| 5 | Asesor de re-anchor WiFi | Resolver #403 | Mayor complejidad y rebase de rama existente | [Phase 5](phases/phase-5.md) | completed |
| 6 | Release v2.24.0 y deploy | Empaquetar todo y actualizar CT/demo | Gate final de entrega | [Phase 6](phases/phase-6.md) | in_progress |

## Risks & Open Questions

| Risk/Question | Impact | Mitigation/Answer |
|---------------|--------|-------------------|
| Rama `feat/403-wifi-reanchor` es antigua y usa DAWN; adaptar a usteer puede requerir refactor notable. | Retraso en fase 5 | Hacer primero las fases mas pequenas; rebase temprano. |
| Diagnostico de #413/#415/#416 puede depender de datos especificos de routers/switches del usuario. | Bloqueo si no hay acceso/logs | Pedir al usuario capturas de `ubus`/`ip`/`swconfig` o logs de NetPulse. |
| #414 requiere validar en Mikrotik CSS610 para ajustar thresholds. | Falsos positivos persistentes | Dejar intervalo/threshold configurables y pedir feedback. |

## Changelog

### 2026-09-01

- Plan creado con 6 fases y alcance confirmado por el usuario.
- Fase 1 completada: #309 cerrado, #404 mergeado en PR #432.
- Fase 2 completada: #413, #415, #416 cerrados y mergeados en PR #433.
- Fase 3 iniciada: tuning SNMP y ghost ports (#414).
