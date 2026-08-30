# Phase 4: Deploy and validate on rt3

## Objective

Build the server and frontend, deploy to CT 226, and validate the DAWN deployment end-to-end on rt3 first.

## Scope

- Local build with embedded agents (dev release).
- Backup current CT 226 binary + data.
- Deploy new build to CT 226.
- Create DAWN plan for rt3 only and apply.
- Verify `ubus call dawn get_network` on rt3 shows neighbors.
- If anything fails, restore CT 226 backup.

## Out of Scope

- Deploy to rt2/rt4/Flint2 until rt3 is clean.
- Merge or push code before validation.

## Deliverables

- CT 226 running the new build.
- Screenshot / log evidence of DAWN plan applied and healthcheck passed on rt3.
- Backup restored if validation failed.

## Acceptance Criteria

- CT 226 health endpoint returns new version.
- rt3 `get_network` shows neighbors after apply.
- No panic / error in server logs during/after apply.
