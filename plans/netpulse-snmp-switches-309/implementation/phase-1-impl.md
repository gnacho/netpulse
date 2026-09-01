---
type: planning
entity: implementation-plan
plan: "netpulse-snmp-switches-309"
phase: 1
status: draft
created: "2026-09-01"
updated: "2026-09-01"
---

# Implementation Plan: Phase 1 - Backend SNMP core

> Implements [Phase 1](../phases/phase-1.md) of [netpulse-snmp-switches-309](../plan.md)

## Approach

Add an SNMP v2c read-only data source for managed switches, polled on its own 60 s cadence, and surface it through the existing live adapter next to the OpenWrt SSH path. The change is four additions that each reuse existing structures:

1. **Dependency**: `github.com/gosnmp/gosnmp` (pure Go) into `server-go/go.mod`.
2. **Schema**: two tables in `internal/db/db.go` (`snmp_targets`, `switch_port_series`) plus CRUD helpers on `*db.DB`, matching the existing literal-schema + helpers pattern (e.g. `ListKnownMacs`).
3. **New package `internal/snmp`**: `Target`, `SwitchSnapshot`, a `Port` shape that maps 1:1 onto the contract `adapters.EthPort` fields, `Poll(ctx, Target)`, and a **pure** table-to-ports mapper that is unit-testable against a mocked SNMP table.
4. **Adapter integration**: a `switch.go` file in package `adapters` (methods on `*Live`) that owns the 60 s poll loop, stores the latest snapshot per router, persists per-port series, and feeds `buildOverview` / `GetRouters` / `GetRouterDetail` for routers of type `managed-switch`.

Key reuse decisions:

- **No new config system.** Per-target SNMP settings (host, community, version, port, interval) live in the gated `snmp_targets` table, exactly as the phase scope dictates. Community is stored plain-text in SQLite, consistent with the existing `kv`-based secret pattern (`adguard_pass`, `agent.token.*`); no ad-hoc encryption.
- **`Port` is `EthPort`-compatible but owned by `internal/snmp`.** `internal/adapters` will import `internal/snmp` (for the poll loop), so `snmp` must not import `adapters` (import cycle). The adapter converts `snmp.Port` -> `adapters.EthPort` with a small mapper, mirroring the existing `ethPortsToAdapter` pattern used for `probe.EthPort`.
- **bps from the previous DB sample.** Per the gated scope, per-port rates are computed as counter deltas against the previous raw `rx_bytes`/`tx_bytes` row in `switch_port_series` (64-bit `ifHCInOctets`/`ifHCOutOctets`, no wrap). First sample yields nil rates, matching the #305 per-iface convention.
- **The 60 s loop is independent of the 5 s `poller`.** It is a goroutine owned by `Live` (started in `NewLive` when `db != nil`, stopped in `Close`), reading targets from DB each tick. It never panics the server (recover + log), and `managed-switch` routers are excluded from the SSH `pollAll`.

## Affected Modules

| Module | Change Type | Description |
|--------|-------------|-------------|
| `server-go/go.mod` | modify | Add `github.com/gosnmp/gosnmp` (pure Go). |
| `server-go/internal/db/db.go` | modify | Add `snmp_targets` + `switch_port_series` to `schemaSQL`; extend `Maintenance` retention. |
| `server-go/internal/db/snmp.go` | create | CRUD helpers for `snmp_targets` and `switch_port_series` on `*db.DB`. |
| `server-go/internal/snmp/` | create | `Target`, `SwitchSnapshot`, `Port`, `Poll`, and pure ifTable mapper. |
| `server-go/internal/adapters/types.go` | modify | Update `RouterConfig.Type` comment to include `managed-switch`. |
| `server-go/internal/adapters/live.go` | modify | Skip `OpenWrtClient` for `managed-switch`; exclude from `pollAll`; wire snapshot into overview/detail. |
| `server-go/internal/adapters/switch.go` | create | 60 s switch poll loop, snapshot store, port-series persistence (methods on `*Live`). |
| `server-go/cmd/snmp-probe/main.go` | create | Manual smoke script to poll a switch and print its ports. |

## Required Context

| File | Why |
|------|-----|
| `server-go/internal/adapters/types.go` | `Router`, `RouterConfig`, `EthPort`, `RouterDetail`, `Snapshotter` contract that switch data must satisfy. |
| `server-go/internal/adapters/live.go` | `Live` struct, `SetRouters`, `pollAll`/`pollRouter`, `buildOverview`, `buildRouter`, `offlineRouter`, `GetRouterDetail`, `GetRouters`, `Close`. |
| `server-go/internal/adapters/openwrt.go` | `GetEthPorts`/`ethPortsToAdapter` pattern for port mapping; `OpenWrtClient` is SSH/ubus-only. |
| `server-go/internal/db/db.go` | `schemaSQL`, `migrate`, `Maintenance` retention, `NowMS` epoch convention, helper style. |
| `server-go/internal/db/snmp.go` (new) | CRUD helpers consumed by the switch loop. |
| `server-go/internal/routerstore/routerstore.go` | `ListRouters`, `AddRouter`, `UpdateRouter` (`type` already stored/validated as `managed-switch`). |
| `server-go/internal/httpapi/config.go` | Router CRUD already validates `managed-switch` in `type` enum (Phase 2 reuses; confirms enum value is accepted). |
| `server-go/cmd/netpulse/main.go` | Wiring, `NewLive`, graceful shutdown (`adapter.Close()`), demo/live branch. |
| `agent/probe/probe.go` | `EthPort`/`IfRate` shapes reused as the mapping reference (ports + counters + rates). |

## Implementation Steps

### Step 1: Add `gosnmp` dependency

- **What**: Add `github.com/gosnmp/gosnmp` to `server-go/go.mod`/`go.sum` (`go get github.com/gosnmp/gosnmp`), then confirm the static build still succeeds.
- **Where**: `server-go/go.mod`, `server-go/go.sum`.
- **Authorized By**: Phase 1 scope "Add `gosnmp` dependency to `server-go/go.mod`"; plan risk table ("Elegir librería pura Go (gosnmp es pura Go) y verificar go build estático").
- **Why**: SNMP v2c client is the missing primitive; gosnmp is pure Go, so it does not disturb the CGO-free static build already used by `modernc.org/sqlite`.
- **Considerations**: Verify `go build ./...` (and a `CGO_ENABLED=0 go build`) succeeds. No other transitive deps expected to break.

### Step 2: DB schema + CRUD helpers

- **What**: Add `snmp_targets` and `switch_port_series` to `schemaSQL` (`CREATE TABLE IF NOT EXISTS`, epochs in ms like the rest of the DB). Add helpers on `*db.DB` in a new `internal/db/snmp.go`: list/upsert/delete `snmp_targets`; insert a port-series row; read the last raw counters per `(router_id, port_index)` for bps deltas. Extend `Maintenance` to purge `switch_port_series` older than `RetentionMS` (7 days).
- **Where**: `server-go/internal/db/db.go` (schema + retention); `server-go/internal/db/snmp.go` (new).
- **Authorized By**: Phase 1 scope schema (`snmp_targets`: host, community, version, port, interval_ms, router_id FK, created_at; `switch_port_series`: router_id, port_index, ts, rx_bps, tx_bps, rx_bytes, tx_bytes, errors_in, errors_out, up_status); functional req "Almacenar series de trafico por boca"; preserved invariant "DB growth is bounded" (existing `Maintenance` purges `metrics`/`adguard_stats` at 7 days).
- **Why**: Persist the target config and per-port series; the loop reads targets and writes series here. `switch_port_series` is dedicated because the `metrics` table is router-only (phase note: "do not overload the metrics table").
- **Considerations**: `snmp_targets` carries both `host` and `router_id` (gated schema); the loop uses `host` as the poll target and `router_id` to join the router card. `up_status` stores `ifOperStatus` (1=up). No rollup buckets in Phase 1 (plan risk: "esquema minimo ... no rehacer historial"); raw-only with 7-day purge.

### Step 3: New package `server-go/internal/snmp`

- **What**: Define `Target{Host, Community, Version, Port, Timeout}`; `Port` (ID=ifIndex string, Label=ifDescr, Iface=ifName, Up, Speed, RxBytes/TxBytes/RxErrs/TxErrs uint64, RxBps/TxBps *float64); `SwitchSnapshot{UptimeSec float64, Ports []Port}`. Implement `Poll(ctx, Target) (*SwitchSnapshot, error)` using gosnmp GET/walk for `sysUpTime`, `ifDescr`/`ifName`, `ifType`, `ifOperStatus`, `ifSpeed`, `ifHCInOctets`, `ifHCOutOctets`, `ifInErrors`, `ifOutErrors`. Implement the mapping as a **pure** function `mapInterfaces(rows) []Port` (mocked-table friendly) that filters `ifType == 6` (ethernetCsmacd) and formats speed/status.
- **Where**: `server-go/internal/snmp/` (`target.go`, `snapshot.go`, `poll.go`, `mapper.go`, `mapper_test.go`).
- **Authorized By**: Phase 1 scope "New package `server-go/internal/snmp`: `Target` struct, `Poll(ctx, Target) -> (*SwitchSnapshot, error)`, mapping from ifTable/ifXTable to a list of EthPort-compatible structs"; functional req (OID list); acceptance "`go test ./server-go/internal/snmp/...` passes with mocked SNMP table"; plan risk "Filtrar por ifType" (VLANs/LAGs out of the physical port view).
- **Why**: Isolates the SNMP client and the OID mapping so both are testable without a live switch; the pure mapper is the unit-test surface for the DoD's "48 Gi + 4 SFP" mapping.
- **Considerations**: OIDs: sysUpTime `1.3.6.1.2.1.1.3.0` (TimeTicks -> seconds); ifTable `1.3.6.1.2.1.2.2.1.{2 descr,3 type,5 speed,8 operStatus,14 inErrors,20 outErrors}`; ifXTable `1.3.6.1.2.1.31.1.1.1.{1 name,6 HCInOctets,10 HCOutOctets}`. Filter `ifType==6` (ethernetCsmacd) to exclude `l2vlan`/`ieee8023adLag`. Speed formatting reuses the existing `"1 Gbps"`/`"100 Mbps"`/`"10 Gbps"` strings from `EthPort.Speed`. `Port` must NOT import `adapters` (cycle).

### Step 4: `managed-switch` in RouterConfig + skip SSH client

- **What**: Update the `RouterConfig.Type` comment to `"glinet"|"openwrt"|"managed-switch"|"external"`. In `SetRouters`, skip `NewOpenWrtClient` for `cfg.Type == "managed-switch"` (no SSH/ubus client); prune any `switchSnapshots` entry for removed IDs.
- **Where**: `server-go/internal/adapters/types.go`, `server-go/internal/adapters/live.go` (`SetRouters`).
- **Authorized By**: Phase note "`RouterConfig.Type` currently only documents `glinet|openwrt` ... update comments"; plan functional req "Soporte de tipo de router `managed-switch` en `RouterConfig`"; preserved invariant "`OpenWrtClient` is SSH/ubus-only" (`pollRouter` would return `router X sin cliente` and mark the switch offline).
- **Why**: `managed-switch` has no SSH/ubus surface; creating a client would both waste resources and make `pollRouter` fail. Routing the type at the client-construction boundary is the smallest correct change.
- **Considerations**: `external` and `AgentOnly` behavior stay untouched. The `Router` struct already documents `managed-switch` (no change needed there beyond the config comment).

### Step 5: 60 s switch poll loop + snapshot store + persistence

- **What**: Add to `Live` (new file `switch.go`): fields `switchSnapshots map[string]*snmp.SwitchSnapshot`, `switchFailCount map[string]int`, `switchLastErr map[string]error`, and a `stopSwitch` channel. Implement `pollSwitchesOnce(ctx)`: list routers where `type='managed-switch'` + their `snmp_targets`, `snmp.Poll` each, compute per-port bps from the previous DB counters, `INSERT` one `switch_port_series` row per port, and update `switchSnapshots[id]`. Start a `time.Ticker(60 * time.Second)` goroutine in `NewLive` (only when `db != nil`), stopped by `Close`.
- **Where**: `server-go/internal/adapters/switch.go` (new), plus a start/stop hook in `live.go` (`NewLive`, `Close`).
- **Authorized By**: Phase 1 scope "Extend `adapters/live.go` to spawn a switch poller loop (60 s) and merge its results"; plan guiding decision "Intervalo de sondeo 60 s ... independiente del poller general de 5 s"; functional req "Compute per-port bps using the previous raw sample stored in DB".
- **Why**: Switches need a cadence decoupled from the 5 s SSH poller and must persist per-port traffic; this is the single place SNMP targets are polled.
- **Considerations**: Use a fixed 60 s ticker (gated default). `interval_ms` is stored (gated schema) but not variably scheduled in Phase 1 (avoid speculative per-target scheduling; Phase 2 API can honor it). Each poll recovers from panics and logs; a failed poll updates `switchFailCount`/`switchLastErr` for the offline alerting parity used by `buildOverview`.

### Step 6: Merge switch data into overview / routers / detail

- **What**: In `pollAll`, skip `managed-switch` configs (no SSH goroutine). In `buildOverview` and `GetRouters`, for `cfg.Type == "managed-switch"` build the `Router` card from `switchSnapshots[id]` (online/offline from last poll, `Type: "managed-switch"`, `Uptime` from `UptimeSec`, `Model`/`Name` from config, `Clients: 0`); offline fallback uses `offlineRouter` (extended to emit `Model: "Switch gestionado"` for that type). In `GetRouterDetail`, for `managed-switch` return `Ports` converted from the snapshot, empty `Radios`, no FDB/LLDP enrichment, and empty `Series`/`Clients`.
- **Where**: `server-go/internal/adapters/live.go` (`pollAll`, `buildOverview`, `GetRouters`, `GetRouterDetail`, `offlineRouter`).
- **Authorized By**: Phase 1 scope "Adapter live integra routers `managed-switch` en `GetOverview`: estado online/offline, uptime, puertos con trafico" and "GetRouterDetail"; plan target outcome "shows its ports ... in the same `RouterDetail.Ports` view".
- **Why**: Without this, the SNMP poll results are invisible; this is the only change that makes the DoD's "switch appears in overview + detail with ports" observable in Phase 1.
- **Considerations**: Preserve the exact contract (`Router.Type`, `Status` `online`/`offline`, `Uptime "<d>d <h>h"`, `Ports []EthPort` with `RxBps`/`TxBps`). Switch ports skip the SSH-only FDB/LLDP enrichment (`snmp` provides no MAC table in scope). `GetMetricsRows` remains router-only and untouched because `managed-switch` is absent from `lastPolled`.

### Step 7: Manual smoke script

- **What**: `server-go/cmd/snmp-probe/main.go` that builds a `snmp.Target` from flags (`-host` default `192.168.10.4`, `-community` required, `-port` default 161), calls `snmp.Poll`, and prints each port (index, label, up, speed, rx/tx bytes and bps).
- **Where**: `server-go/cmd/snmp-probe/main.go`.
- **Authorized By**: Phase 1 deliverable "Manual test script that polls 192.168.10.4 and prints the ports"; acceptance "Manual run from aoostar-boss can poll the switch via the new code path".
- **Why**: Phase 1 has no API/UI to seed targets, so the smoke validates the real switch (`.4`) through the new code path without a full server deploy.
- **Considerations**: `-community` is a flag with no committed default (the read-only community `DxN3tPulse-RO-2026` is not persisted in the repo); `-host` defaults to `192.168.10.4` per the validation target.

## Testing Plan

**Primary Verify Command**: `go test ./...`

> Run from `server-go/`. This exercises the new `internal/snmp` mapper (mocked SNMP table), the new `internal/db` schema/helpers, and the adapter wiring, and is the single gate that subsumes the phase's `go test ./server-go/internal/snmp/...` and `go test ./server-go/internal/db/...` acceptance criteria.

### Additional Checks

- `CGO_ENABLED=0 go build ./...` from `server-go/` — confirms the gosnmp addition keeps the static build green (plan risk).
- `go run ./cmd/snmp-probe -host 192.168.10.4 -community <community>` from `server-go/` — manual smoke against the real Reyee switch, prints 48 Gi + 4 SFP ports.

## Rollback Strategy

N/A for the code changes themselves (feature-additive behind a new `type` value and new tables). The DB additions are `CREATE TABLE IF NOT EXISTS` and additive `migrate()`-free, so a rollback is simply reverting the commit; no destructive migration or config change is introduced. `snmp_targets`/`switch_port_series` can be dropped manually if the feature is reverted after deployment.

## Reality Check

- **Task reference `server-go/internal/httpapi/routers.go` does not exist.** Router CRUD lives in `server-go/internal/httpapi/config.go` (routes `/api/config/routers`, enum already accepts `managed-switch`). Non-blocking: the plan above targets `config.go`/`routerstore.go` instead; flagging for the primary so the reference list is corrected.
- **`snmp_targets` stores both `host` and `router_id`.** `host` is denormalized (already present in `routers.host`). This is gated schema (phase scope lists both), so it is kept as-is; the loop uses `snmp_targets.host` as the poll target. Non-blocking, noted for Phase 2 API design (single source of truth for `host` should be decided there).
- **No switch targets can be seeded through the product in Phase 1** (API/UI is Phase 2). The overview/detail wiring and the 60 s loop are therefore unobservable via the running server until Phase 2 seeds a `managed-switch` router + `snmp_targets` row. The Phase 1 acceptance is covered by the `snmp.Poll` smoke script (Step 7), not by the HTTP surface. This is inherent to the gated phase split, not a scope change.
