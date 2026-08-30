# Phase 2: Backend plan builder + healthcheck + rollback

## Objective

Wire the `dawn` module into the orchestration engine so NetPulse can generate, apply and verify a DAWN deployment plan.

## Scope

- `computeModuleDiff` case for `dawn` in `httpapi/orchestr.go`.
- Healthcheck op `dawn_check` in `agent/executor/executor.go`.
- Healthcheck implementation: `ubus call dawn get_network` returns JSON with neighbor BSSIDs.
- Rollback uses existing UCI snapshot restore if healthcheck fails.
- Method label active/inactive for DAWN in view-model.

## Out of Scope

- Frontend card (Phase 3).
- Deploy on real router (Phase 4).

## Deliverables

- Updated `httpapi/orchestr.go`.
- Updated `agent/executor/executor.go` with `dawn_check` kind.
- Tests for plan generation and healthcheck.

## Acceptance Criteria

- `go test -race ./internal/orchestr/...` passes.
- `go test ./agent/executor/...` passes.
- POST `/api/plans` with module `dawn` returns expected ops.
- Healthcheck fails when `get_network` is empty / errors.
