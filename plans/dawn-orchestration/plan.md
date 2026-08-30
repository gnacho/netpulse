---
type: planning
entity: plan
plan: dawn-orchestration
status: active
created: 2026-08-30
updated: 2026-08-30
---

# Plan: DAWN orchestration button for NetPulse

## Problem / Context

NetPulse already reads DAWN state (Phase 14: `/api/dawn`, `/roaming`, roaming events), but it cannot install, enable or configure it. On multi-AP networks like Mandor this means DAWN is deployed and kept consistent by hand over SSH. After firmware upgrades packages can disappear while UCI config survives, hostapd parameters drift (mobility domains, 802.11k/r/v, `random_bssid`), and diagnosing drift is manual.

Issue: https://github.com/gnacho/netpulse/issues/402

## Target Outcome

A "Deploy / Fix DAWN" button in NetPulse that:

1. Detects which routers can run DAWN and whether required packages are installed.
2. Installs / re-enables DAWN and a watchdog where missing.
3. Applies a consistent roaming baseline across routers.
4. Surfaces inconsistencies and offers a one-click correction plan.
5. Verifies the result with `ubus call dawn get_network` and rolls back on failure.

## Guiding Decisions & Constraints

- Reuse the existing orchestration framework (plan → apply → state, executor allowlist, UCI snapshot + rollback).
- No changes to router firewall / nftables / iptables.
- No router reboots as part of normal flow; only service restarts (`dawn`, `network`, `wifi reload`).
- First validation on rt3 (non-gateway), then rt2/rt4, finally Flint2.
- GL.iNet and plain OpenWrt must both be supported; detect by packages / UCI shape, do not assume.
- Baseline must match the verified Mandor config: `kicking=3`, `eval_auth_req=0`, `eval_assoc_req=0`, `duration=150`, `random_bssid=0`, 802.11k/v/r enabled, common SSID, consistent mobility domain.
- Update `README.md` and `README.es.md` status tables to reflect the new DAWN-write capability.

## Requirements

### Functional

- [ ] New `dawn` orchestration module under `server-go/internal/orchestr/`.
- [ ] Probe reads installed packages, `/etc/config/dawn`, `/etc/config/wireless` roaming params and current `random_bssid`.
- [ ] Desired state defines the baseline config and flags drift.
- [ ] Module diff generates ops: package install, uci set, service enable/start, watchdog cron.
- [ ] Healthcheck verifies `ubus call dawn get_network` returns neighbors.
- [ ] Register module in `computeModuleDiff`, UI method active/inactive.
- [ ] Frontend card in `/orchestration` with Deploy button, progress and inconsistency warnings.
- [ ] Update README status tables (Fase 10 / Phase 17).

### Non-Functional

- [ ] Tests for probe parsing, diff generation, healthcheck pass/fail and rollback.
- [ ] Frontend i18n ES/EN.
- [ ] No em dashes in any committed text.

## Scope

### In Scope

- DAWN package install and watchdog restore.
- Consistent DAWN + 802.11k/v/r/FT baseline config.
- Drift detection and suggestion UI.
- Rollback on failed healthcheck.
- README status update.

### Out of Scope

- Changing firewall / QoS / SQM / DNS / SSID names unrelated to roaming.
- Firmware upgrades or sysupgrade.
- Non-OpenWrt / non-GL.iNet targets.
- Advanced per-client steering policies beyond the baseline.

## Definition of Done

- [ ] All tests pass (`go test -race ./internal/orchestr/...`, `npm run build`, `npm run lint`).
- [ ] `dawn` module appears in `/orchestration` and can generate a plan.
- [ ] Plan applied successfully on rt3; healthcheck passes; `ubus call dawn get_network` shows neighbors.
- [ ] README.es.md and README.md status tables updated.
- [ ] PR opened with `closes #402`.

## Testing Strategy

- Unit tests for probe parsers using captured router output.
- Unit tests for diff generation and healthcheck.
- Frontend build and lint as regression gate.
- Real validation on rt3 with manual rollback backup before apply.

## Phases

| Phase | Title | Contribution | Why Separate | Detail | Status |
|-------|-------|--------------|--------------|--------|--------|
| 1 | Backend probe + desired state | Model current/desired DAWN config | Stable foundation for plan generation | [Phase 1](phases/phase-1.md) | completed |
| 2 | Backend plan builder + healthcheck | Generate ops, verify DAWN mesh, rollback | Needs probe model from Phase 1 | [Phase 2](phases/phase-2.md) | completed |
| 3 | Frontend Deploy DAWN card | Button, progress, drift suggestions | Can be built against Phase 2 API | [Phase 3](phases/phase-3.md) | completed |
| 4 | Deploy and validate on rt3 | End-to-end check on real router | Must not touch production until code is ready | [Phase 4](phases/phase-4.md) | completed |
| 5 | Docs + PR + release | README update, PR, merge, release | Final delivery step | [Phase 5](phases/phase-5.md) | pending |

## Risks & Open Questions

| Risk/Question | Impact | Mitigation/Answer |
|---------------|--------|-------------------|
| `wifi reload` or `network reload` can briefly disconnect clients. | Medium | Use `wifi reload` only when wireless config changes; healthcheck waits for DAWN mesh. |
| Flint2 GL.iNet firmware may store UCI keys differently. | Medium | Probe detects shape; skip unsupported keys; test on Flint2 last. |
| DAWN depends on full wpad (not wpad-basic). | Low | Probe checks wpad variant and add install op if needed. |

## Changelog

### 2026-08-30

- Plan created and scope confirmed by user.
- Phase 4 validated on rt4 (redmi-ax6-3, native agent with SSE). rt3 (redmi-ax6-2, NetGrip) skipped because executor token not yet configured. Plan applied successfully: 13 ops, 206ms, status `applied`. Config verified via SSH: `network_option=2`, `tcp_port=1026`, `shared_key=test-shared-key`, `broadcast_ip=192.168.1.255`, `random_bssid=0` on both radios. DAWN mesh confirmed (Flint2 + 27 clients visible).
