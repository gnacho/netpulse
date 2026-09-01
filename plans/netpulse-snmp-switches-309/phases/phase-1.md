---
type: planning
entity: phase
plan: netpulse-snmp-switches-309
phase: 1
status: pending
created: 2026-09-01
updated: 2026-09-01
---

# Phase 1: Backend SNMP core

> Part of [netpulse-snmp-switches-309](../plan.md)

## Objective

The server can poll a managed switch over SNMP v2c every 60 seconds and expose its data through the existing live adapter alongside OpenWrt routers.

## Scope

### Includes

- Add `gosnmp` dependency to `server-go/go.mod`.
- DB schema additions in `server-go/internal/db/db.go`:
  - `snmp_targets` table (host, community, version, port, interval_ms, router_id FK, created_at).
  - `switch_port_series` table (router_id, port_index, ts, rx_bps, tx_bps, rx_bytes, tx_bytes, errors_in, errors_out, up_status) with rollup buckets if needed.
- New package `server-go/internal/snmp`:
  - `Target` struct.
  - `Poll(ctx, Target) -> (*SwitchSnapshot, error)`.
  - Mapping from ifTable/ifXTable to a list of `EthPort` compatible structs.
  - Compute per-port bps using the previous raw sample stored in DB.
- Extend `RouterConfig` and `adapters.Router` to support `type = "managed-switch"`.
- Extend `adapters/live.go` to spawn a switch poller loop (60 s) and merge its results into `GetOverview`/`GetRouterDetail`.
- Persist switch metrics and port series.

### Excludes

- UI or API for adding switches (Phase 2).
- Real deployment (Phase 3).
- v3 or traps.

## Prerequisites

- [ ] Issue #309 reopened and plan approved.
- [ ] Switch .4 SNMP reachable from CT 200.

## Deliverables

- [ ] `server-go/internal/snmp` package with tests.
- [ ] DB schema migrations and CRUD helpers.
- [ ] Adapter integration for managed-switch routers.
- [ ] Manual test script that polls 192.168.10.4 and prints the ports.

## Acceptance Criteria

- [ ] `go test ./server-go/internal/snmp/...` passes with mocked SNMP table.
- [ ] `go test ./server-go/internal/db/...` passes with new schema.
- [ ] Manual run from aoostar-boss can poll the switch via the new code path.
- [ ] No regression: `go test ./server-go/...` passes.

## Dependencies on Other Phases

| Phase | Relationship | Notes |
|-------|-------------|-------|
| 2 | blocks | Phase 2 builds the API/UI on top of this backend. |

## Notes

- `RouterConfig.Type` currently only documents `"glinet"|"openwrt"` but the `Router` struct already accepts `"managed-switch"`. Update comments.
- Keep credential storage simple for v2c: plain community string in DB; the project already stores similar low-sensitivity config in `kv`.
- Use the existing `MetricsRow` style for snapshots but do not overload the `metrics` table (it is router-only). Create a dedicated port series table.
