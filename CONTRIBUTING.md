# Contributing

Thanks for spending time on NetPulse. This is how changes land.

## Before you start

- For anything non-trivial, open an issue first so we can agree on the approach before you write code.
- Work on your own fork and open a pull request against `main`. Never push to this repository directly.
- Issues and pull requests are written in English.
- For vendor-specific router integrations, read [docs/external-integrations.md](docs/external-integrations.md). The default architecture is a separate Go pusher maintained with the hardware owner, not another vendor API in the server.

## Delivery format

Every pull request runs `pr-checks` (server build with the embedded agents, tests for both Go modules, frontend build with its type check) and `gitleaks`. The PR must be green before review.

Run `make check` before pushing to reproduce `pr-checks` locally. It installs the frontend dependencies, lints and builds the app, refreshes the embedded SPA, cross-compiles the agent binaries and runs the Go tests for both modules.

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

## New hardware support

Supporting new hardware is a longer-lived commitment than a regular fix, because the maintainer cannot test what they do not own. The bar depends on how new the hardware is.

**New family (adapter, protocol, vendor API):** ten days of soak.

- Run the branch as your daily driver in your own network for at least ten days.
- Keep the branch public in your fork during that period, so the commit history shows the soak window.
- Include a short soak log in the PR: what you tested, what broke, what you fixed.
- Real hardware only: developed and tested against the actual device, read-only first (sessions, polling, parsing), no writes.

**New model in a family already supported (same adapter, different box):** no mandatory soak.

- A real device fixture for the parser tests is mandatory, plus a screenshot of the model showing up in NetPulse.
- The maintainer may still ask for a few days of use depending on what the change touches.

**Evidence we ask for, in all cases:**

- Device model and exact firmware version.
- Real fixtures for the tests, with serial numbers, MACs, public IPs and credentials redacted.
- Screenshots of the screens the integration feeds: overview, fleet, port detail, topology, as applicable.
- A short note of known gaps and what is still unverified.

A draft without that evidence can still be shared as an issue (dumps, API notes, a branch link) so someone else with the hardware can pick it up. It just does not land as support.

## Agent and collector changes

If you touch `agent/`, bump `agent/VERSION` in the same pull request. The release gate fails when `agent/` changes without a version bump.

## Releases

A release is cut deliberately by a maintainer, never as a side effect of merging a pull
request. Do not push a tag until the recap below exists and the maintainer has agreed to it.

1. **Recap first.** List every open issue and every open pull request and say, in one line
   each, what goes into this release and what waits for the next one. A change that is
   already merged is not automatically in: the recap decides. Skip this and features that
   were reported before the cut end up shipping one release late for no reason.
2. **Check the public documentation** against what the release contains: `README.md`,
   `README.es.md`, `docs/manual.en.md`, `docs/manual.es.md` and `CHANGELOG.md`.
3. **Choose the version honestly.** Patch for fixes, minor for features, major for breaking
   changes. Round milestone numbers (`x.Y.0` with a round `Y`) are reserved for announced
   releases with the README, the landing page, the manuals and the memory up to date. They
   are never spent on a routine cut.
4. **Run the gate.** `AGENT_VERSION=<version> make check`. If `agent/` changed, bump
   `agent/VERSION` in the same release.
5. **Publish.** Commit `chore(release): <version>`, create an annotated tag, push `main` and
   the tag. Wait for the release workflow and verify the assets and their checksums.
6. **Verify what people actually get.** Deploy and confirm the health endpoint reports the
   new version, then check that the demo, the landing page and any generated assets follow.

## Security

Do not open a public issue for a vulnerability. Use GitHub private vulnerability reporting instead (see [SECURITY.md](SECURITY.md)).
