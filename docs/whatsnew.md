# What's new in the app

The panel has its own release notes page (`/whatsnew`), and it is not a
changelog. Each release gets cards, and each card explains one change with a
short animation of the interaction it introduces: the icon picker opening and
filtering, the DNS bars filling, a stale neighbour coming back to
`REACHABLE`. The cards are localized in every UI language and follow the
theme, because they are built from the app's own components and tokens, not
recorded.

Why: the changelog answers "what changed" for someone who already knows the
app. A visual change shipped as a bullet point is invisible to the operator
who would use it.

## Adding a release

Three edits, no more:

1. **`app/src/whatsnew/registry.tsx`**: add the cards. Each one carries an
   `id`, the `version`, a Lucide `icon`, the GitHub `author`, the `pr` number
   and a `Demo` component. The `author` is only for the thanks on the card: our
   own changes carry `gnacho` and render no thanks, because crediting yourself
   for your own work is noise.
2. **The demo** in `app/src/whatsnew/cards/`. Reuse the real components and
   the real catalogue when you can (the icon picker demo filters the real icon
   labels), sample data when the screen needs a backend. Use `useDemoClock` so
   the animation loops without piling up timers, and remember that with
   `prefers-reduced-motion` the clock freezes on the last step: draw that step
   as the finished state.
3. **The four catalogs**: `whatsnew.cards.<id>.{title,body}` (and `caption`
   if the demo needs a note) in `app/public/locales/{en,es,zh,ru}`.

Nothing else. The page groups by version, the newest first, and the sidebar
version stamp shows a dot until the running version has been opened once.
`localStorage` keeps the acknowledged version, so an update turns the dot back
on by itself.

## Rules for the copy

- The title names the change, the body says what it means for the operator.
  Concrete nouns beat adjectives: "Quiet is not offline", not "Improved
  presence detection".
- One card, one change. A release with six cards is a release with six
  features, not a card with six bullets.
- No em dashes and no semicolons, in any language.
- If a change has no visible interaction (a fix, a build flag), it belongs in
  the changelog, not here. The page is for what can be shown.

## What is deliberately out of scope

- Hand-recorded screenshots or GIFs. The cards are UI, so they age with the app
  instead of against it. For the release notes, `scripts/release_media.py`
  exports the same card demos to GIF from the running app, which keeps one
  source for both surfaces (`docs/releases/README.md`).
- Publishing the same content into the GitHub release page. The registry can
  feed that later if it is worth it, but the page comes first.
