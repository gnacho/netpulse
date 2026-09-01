---
type: planning
entity: todo
plan: netpulse-snmp-switches-309
updated: 2026-09-01
---

# Todo: netpulse-snmp-switches-309

> Tracking [netpulse-snmp-switches-309](plan.md)

## Active Phase: 1 - Backend SNMP core

### Phase Context

- **Scope**: [Phase 1](phases/phase-1.md)
- **Implementation**: Not authored yet
- **Latest Handover**: None
- **Relevant Docs**: `server-go/internal/adapters/types.go`, `server-go/internal/db/db.go`, `server-go/internal/poller/poller.go`

### Pending

- [ ] Add `gosnmp` to `server-go/go.mod` and verify static build.
- [ ] Add `snmp_targets` and `switch_port_series` schema in `server-go/internal/db/db.go`.
- [ ] Create `server-go/internal/snmp` package with `Target`, `Poll`, `SwitchSnapshot` and mapper to ports.
- [ ] Add `managed-switch` to `RouterConfig.Type` and `Router.Type` contract.
- [ ] Integrate SNMP poller loop into `adapters/live.go` (60 s interval, separate from 5 s poller).
- [ ] Write unit tests for mapper and DB helpers.
- [ ] Manual smoke test against 192.168.10.4.

### In Progress

- [ ] Plan created and approved.

### Completed

- [x] Reopened #309 and verified switch SNMP reachability.

### Blocked

- [ ] Implementation plan for Phase 1 pending (use `author-and-verify-implementation-plan`).

## Changelog

### 2026-09-01

- Plan and Phase 1-3 docs created; user confirmed v2c, 60 s poll, v2.23.5 release target.
