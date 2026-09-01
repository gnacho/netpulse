---
type: planning
entity: phase
plan: netpulse-snmp-switches-309
phase: 3
status: pending
created: 2026-09-01
updated: 2026-09-01
---

# Phase 3: Deploy, validation and release

> Part of [netpulse-snmp-switches-309](../plan.md)

## Objective

The feature is running in CT 200 oficina, verified against the Reyee switch, and shipped as v2.23.5.

## Scope

### Includes

- Merge latest `main` into feature branch, resolve conflicts.
- Full build pipeline: `go test ./...`, `npm run build`, `npm run lint`.
- Build server binary with embedded agent binaries.
- Deploy to CT 200 oficina (192.168.10.200) using project install pattern / backup swap.
- Add the switch via UI in CT 200 and verify live data.
- Smoke test existing OpenWrt routers remain online.
- Create PR to `main` with `Closes #309`.
- After merge: tag release v2.23.5, CI builds assets and demo online.
- Update memory files: `netpulse.md`, `red-ofi.md`.

### Excludes

- Deploying to CT 226 casa (will happen via updater in-app after release).
- Releasing extra agent versions.

## Prerequisites

- [ ] Phase 2 completed and green.
- [ ] User available for a deploy window if needed.

## Deliverables

- [ ] PR `feat/309-snmp-switches` ready for review.
- [ ] Release v2.23.5 published.
- [ ] Demo online updated.

## Acceptance Criteria

- [ ] `/api/health` on CT 200 reports new version and no errors.
- [ ] Switch 192.168.10.4 appears online and its ports show traffic.
- [ ] Existing routers in CT 200 unaffected.
- [ ] CI green for PR and release.
- [ ] Memory updated.

## Dependencies on Other Phases

| Phase | Relationship | Notes |
|-------|-------------|-------|
| 2 | blocked-by | Needs the full backend + UI. |

## Notes

- Current main is at v2.23.3; next release per project rule is +0.02 -> v2.23.5.
- CT 200 is in red oficina (aoostar-boss), accessible from this machine.
- Demo online update follows the established path in `netpulse.md`.
