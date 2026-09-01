---
type: planning
entity: phase
plan: netpulse-snmp-switches-309
phase: 2
status: pending
created: 2026-09-01
updated: 2026-09-01
---

# Phase 2: API + UI onboarding

> Part of [netpulse-snmp-switches-309](../plan.md)

## Objective

Users can add, edit and remove managed switches from the NetPulse UI; the switch appears as a router card and its ports are visible in the router detail.

## Scope

### Includes

- Backend:
  - Extend existing `/api/config/routers` handlers to accept `type="managed-switch"` plus SNMP fields (host, community, port, intervalMs).
  - CRUD for `snmp_targets` linked to `routers` row.
  - Validation: host reachable, community non-empty, port default 161, interval >= 10 s.
- Frontend:
  - Add "Switch SNMP" option when creating a router.
  - Form fields: host, community, port (default 161), poll interval (default 60 s).
  - Router card shows model/type "Switch gestionado" and status.
  - Reuse `PortPanel` in `RouterDetail`; ensure SNMP ports render speed and traffic.
- i18n ES/EN labels for new switch type.

### Excludes

- Bulk import, discovery, or templates for switches.
- SNMP v3 fields in the form.

## Prerequisites

- [ ] Phase 1 completed and tests green.

## Deliverables

- [ ] Backend CRUD handlers + tests.
- [ ] Frontend form + router card updates.
- [ ] i18n strings.
- [ ] Playwright walkthrough script that adds the Reyee switch and checks the detail.

## Acceptance Criteria

- [ ] `npm run build` and `npm run lint` pass.
- [ ] `go test ./server-go/internal/httpapi/...` passes.
- [ ] Manual UI check: add 192.168.10.4 with community `DxN3tPulse-RO-2026`; card appears online; detail shows 48+4 ports with live traffic.

## Dependencies on Other Phases

| Phase | Relationship | Notes |
|-------|-------------|-------|
| 1 | blocked-by | Needs the SNMP backend contract. |
| 3 | blocks | Phase 3 deploys and releases this work. |

## Notes

- The Reyee switch is the canonical validation target; use its real host/community for the manual UI test.
- Keep the form minimal; advanced fields (v3, engineID, etc.) are out of scope.
