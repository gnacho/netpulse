# Phase 1: Backend probe + desired state

## Objective

Create the `dawn` orchestration module that can read the current DAWN and roaming state from a router and compare it to a desired baseline.

## Scope

- New file `server-go/internal/orchestr/dawn.go`.
- Types: `DawnDesired`, `DawnScenario`, `DawnOps`.
- Probe command and parsers for:
  - Installed packages (`apk list --installed` / `opkg list-installed`): `dawn`, `luci-app-dawn`, `wpad-*`.
  - `/etc/config/dawn`: `kicking`, `kicking_threshold`, `duration`, `eval_auth_req`, `eval_assoc_req`, `eval_probe_req`, `network_option`, `broadcast_ip`, `tcp_port`, `shared_key`, `iv`, `use_symm_enc`, `hostapd_dir`.
  - `/etc/config/wireless` per SSID: `ieee80211k`, `ieee80211r`, `ieee80211v`, `mobility_domain`, `ft_over_ds`, `ft_psk_generate_local`, `bss_transition`, `random_bssid`.
- `DawnOps` function returns ops needed to reconcile actual → desired.
- Register in `modules.go` and add `KindDawn` method active/inactive.

## Out of Scope

- Plan builder integration (Phase 2).
- Frontend card (Phase 3).
- Real router apply.

## Deliverables

- `server-go/internal/orchestr/dawn.go`.
- `server-go/internal/orchestr/dawn_test.go`.
- Updated `server-go/internal/orchestr/modules.go`.

## Acceptance Criteria

- `go test ./internal/orchestr/...` passes.
- Probe tests cover: DAWN installed, DAWN missing, partial config.
- Diff tests cover: no-op when actual matches desired, install packages when missing, uci sets when values differ.
