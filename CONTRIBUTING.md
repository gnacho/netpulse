# Contributing

Thanks for spending time on NetPulse. This is how changes land.

## Before you start

- For anything non-trivial, open an issue first so we can agree on the approach before you write code.
- Work on your own fork and open a pull request against `main`. Never push to this repository directly.
- Issues and pull requests are written in English.

## Delivery format

Every pull request runs `pr-checks` (server build with the embedded agents, tests for both Go modules, frontend build with its type check) and `gitleaks`. The PR must be green before review.

- **New server code goes in its own file by domain**, next to its test. `live.go` is wiring only; do not append features to it. This keeps review, conflict resolution and revert easy.
- **String contracts get constants.** Do not match on loose literals; if a value is part of a contract (`PeerKind` and friends), it gets a constant or a helper.
- **Every behavior change ships with a test** that would fail without it. Real device fixtures and table-driven cases are preferred; no invented readings.
- **One concern per PR**, keep diffs small.

## Style

- Go: `gofmt` and `go vet` clean. Conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `ci:`), imperative subject, one line.
- Web app: `npm run lint` and the build's type check are part of CI; keep them clean.
- Comments explain why, not what. No emoji in code or docs.

## Review and merge

- A maintainer reviews every PR and merges it squashed with `Co-authored-by`, so your work counts in the contributors graph.
- If a reviewed change needs a different shape (for example code that belongs in its own file), we may ask for a resend under this format instead of merging first and moving it later.
- Reverts can happen at any time. The format above exists so a revert is a clean, isolated operation.

## Agent and collector changes

If you touch `agent/`, bump `agent/VERSION` in the same pull request. The release gate fails when `agent/` changes without a version bump.

## Security

Do not open a public issue for a vulnerability. Use GitHub private vulnerability reporting instead (see [SECURITY.md](SECURITY.md)).
