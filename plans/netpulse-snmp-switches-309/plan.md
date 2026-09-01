---
type: planning
entity: plan
plan: netpulse-snmp-switches-309
status: active
created: 2026-09-01
updated: 2026-09-01
---

# Plan: netpulse-snmp-switches-309

## Problem / Context

NetPulse today ingests routers and agent-only devices over SSH/ubus or the agent push path. Managed switches (Ruijie/Reyee, Netgear, etc.) expose a standard SNMP data source - ifTable/ifXTable for per-port state, speed and counters - without needing a custom scraper or a resident agent. Issue #309 asks to onboard switches as first-class SNMP targets so NetPulse can cover third-party hardware the same way it covers OpenWrt.

Switch de oficina (Reyee RG-NBS3200-48GT4XS, 192.168.10.4) ya tiene SNMP v2c activo con community `DxN3tPulse-RO-2026` (ver `red-ofi.md`). Servira como target real de validacion.

## Target Outcome

A managed switch can be added to NetPulse with host, community and poll interval; the server polls it over SNMP v2c and shows its ports, link state and per-port traffic in the same `RouterDetail.Ports` view used for OpenWrt devices.

## Guiding Decisions & Constraints

- Version SNMP: **v2c read-only** para la primera iteracion (decision usuario). v3, traps y FDB/LLDP quedan fuera.
- Intervalo de sondeo: **60 s** por switch, independiente del poller general de 5 s (decision usuario). Se implementa con un scheduler propio.
- Release: **v2.23.5** reservada para #309 (decision usuario, +0.02 desde v2.23.3).
- Flujo de trabajo: issue #309 reabierto, rama local, tests verdes, deploy CT 200, validacion, PR a `main` con `Closes #309`, release y demo online.
- No instalar paquetes ni tocar routers/switches una vez activado SNMP (ya hecho).
- Reutilizar el shape `EthPort` existente siempre que sea posible; anadir campos solo si SNMP los provee y la UI los necesita.
- Credenciales (community) en BD: texto plano o base64, accessible solo al server. No inventar cifrado ad-hoc salvo que el proyecto ya tenga un patron para secrets.

## Requirements

### Functional

- [ ] Tabla `snmp_targets` para guardar host, community, version (v2c), port (161 default) e intervalo (60 s default) por router.
- [ ] Soporte de tipo de router `managed-switch` en `RouterConfig` y la UI.
- [ ] Paquete `server-go/internal/snmp` que use gosnmp para sondear sysUpTime, ifDescr/ifName, ifOperStatus, ifSpeed, ifHCInOctets, ifHCOutOctets, ifInErrors, ifOutErrors.
- [ ] Adapter live integra routers `managed-switch` en `GetOverview`: estado online/offline, uptime, puertos con trafico.
- [ ] Almacenar series de trafico por boca para switches (tabla `switch_port_series` o reutilizar esquema futuro de port_series; ver decisiones tecnicas).
- [ ] API CRUD para anadir/quitar/editar switches SNMP desde la UI.
- [ ] Frontend: formulario de switch SNMP; tarjeta de router muestra tipo "Switch gestionado"; detalle reutiliza `PortPanel`.
- [ ] Verificacion real contra el switch .4 en CT 200 oficina.

### Non-Functional

- [ ] `go test ./...`, `npm run build` y `npm run lint` verdes.
- [ ] Tests Go para el parser SNMP y el mapper de ifIndex a `EthPort`.
- [ ] No regresion en routers OpenWrt existentes.
- [ ] Demo online actualizada con la release.

## Scope

### In Scope

- SNMP v2c read-only.
- Sondeo periodico de switches con intervalo propio (60 s default).
- Per-port link state, speed, bytes (64-bit counters), errors.
- Onboarding via UI/API.
- Integracion en overview/router detail existentes.
- Release v2.23.5.

### Out of Scope

- SNMP v3, traps, informs.
- MAC address table (dot1dTpFdbTable) / topology enrichment por SNMP.
- Sensor/entity MIBs (temperatura, etc.).
- Soporte de multiples comunidades o ACL complejas.
- Auto-discovery de switches por SNMP.
- Implementacion en agente standalone o NetGrip.

## Definition of Done

- [ ] Switch Reyee .4 aparece como router online en NetPulse CT 200.
- [ ] Detalle del switch muestra 48 puertos Gi y 4 SFP con estado up/down y trafico real.
- [ ] `go test ./...`, `npm run build`, `npm run lint` pasan.
- [ ] PR mergeado a `main`, release v2.23.5, demo online actualizada.

## Testing Strategy

- [ ] Tests unitarios en Go para el mapper SNMP -> `EthPort` (mock de tabla OID).
- [ ] Smoke test real contra 192.168.10.4 desde CT 200.
- [ ] Playwright/browser walkthrough para anadir switch desde UI y verificar tarjeta/detalle.
- [ ] Test de no regresion: routers OpenWrt continuan apareciendo en overview.

## Phases

| Phase | Title | Contribution | Why Separate | Detail | Status |
|-------|-------|--------------|--------------|--------|--------|
| 1 | Backend SNMP core | Schema, poller Go gosnmp e integracion en adapter live | La base tecnica (DB + polling) debe ser estable antes de exponer UI | [Phase 1](phases/phase-1.md) | pending |
| 2 | API + UI onboarding | CRUD de switches SNMP y visualizacion de puertos | Depende del contrato de datos del Phase 1; puede validarse por separado | [Phase 2](phases/phase-2.md) | pending |
| 3 | Deploy, validation, release | Build, tests, CT 200 validation, PR, release v2.23.5 | Necesita hardware real y ventana de deploy | [Phase 3](phases/phase-3.md) | pending |

## Risks & Open Questions

| Risk/Question | Impact | Mitigation/Answer |
|---------------|--------|-------------------|
| `gosnmp` no esta en go.mod; puede afectar a build estatico CGO-free con modernc sqlite | Build / CI | Elegir libreria pura Go (gosnmp es pura Go) y verificar `go build` estatico |
| Series de puertos no tienen tabla existente en main; #305 se almacenaba de otra forma | Scope creep | Definir esquema minimo `switch_port_series` en Phase 1, no rehacer historial |
| Multiples ifIndex logicos (VLANs, LAGs) pueden ensuciar la UI | UX | Filtrar por `ifType` o por rango de ifIndex (fisico = 1-52 + LAG 1000+) |

## Changelog

### 2026-09-01

- Plan created and scope confirmed with user (v2c, 60 s poll, v2.23.5 release, switch .4 as validation target).
