---
type: planning
entity: todo
plan: netpulse-snmp-switches-309
updated: 2026-09-01
---

# Todo: netpulse-snmp-switches-309

> Tracking [netpulse-snmp-switches-309](plan.md)

## Active Phase: 3 - UI/UX polish and release prep

### Phase Context

- Backend SNMP core is implemented, tested and deployed on CT 200.
- Frontend compact port panel for switches is implemented and validated with Playwright.
- Branch `feat/309-snmp-switches` pushed to origin.

### Completed

- [x] Reopened #309 and verified switch SNMP reachability.
- [x] Added `gosnmp` dependency and implemented `PollIfTable` filtering physical ports (ifType 6).
- [x] Implemented independent 60 s SNMP poll loop in `adapters/live.go` and `adapters/snmp_live.go`.
- [x] Persisted port samples to `port_series_raw` and exposed switch detail with 52 `ethPorts`.
- [x] Default `managed-switch` type when `SnmpEnabled` is true.
- [x] Hid CPU/RAM/temp and WiFi metrics in `RouterCard`/`FleetCard` for `managed-switch`/`external`.
- [x] Added compact mode to `PortPanel` so 52 ports wrap across the full width on desktop.
- [x] Deployed and validated on CT 200 (192.168.10.200:3000).
- [x] Committed and pushed `feat/309-snmp-switches`.

### Pending

- [ ] Clean any remaining debug logs (SNMP poll logs are informational; review before release).
- [ ] Open PR `feat/309-snmp-switches → main` with changelog entries and close #309.
- [ ] Tag release v2.23.5 and update app/package.json version.
- [ ] Update README/memory with final deploy notes and screenshots.

### In Progress

- [ ] User validated desktop view; handover generated.

### Blocked

- None.

## Changelog

### 2026-09-01

- Backend SNMP poller implemented and deployed on CT 200.
- Compact port panel for managed switches validated on desktop.
- Branch pushed; session ends with handover.
