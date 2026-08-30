# Phase 3: Frontend Deploy DAWN card

## Objective

Add a UI card in `/orchestration` to deploy or fix DAWN, showing progress and drift warnings.

## Scope

- New `DawnModuleCard` in `app/src/pages/Orchestration.tsx` (or small component file).
- Card shows:
  - Active / inactive state.
  - Drift list (inconsistent mobility domain, missing 802.11k/v/r, random_bssid=1, missing packages).
  - "Deploy / Fix DAWN" button that creates a plan and opens the existing apply dialog.
- i18n keys in ES/EN under `orchestration.dawn.*`.

## Out of Scope

- New pages or routing changes.
- Demo-only data changes.

## Deliverables

- Updated `app/src/pages/Orchestration.tsx`.
- Updated `app/public/locales/es/orchestration.json` and `app/public/locales/en/orchestration.json`.
- Frontend build passes.

## Acceptance Criteria

- `npm run build` exits 0.
- `npm run lint` exits 0 (no new errors).
- Card renders in `/orchestration` when backend exposes `dawn` module.
