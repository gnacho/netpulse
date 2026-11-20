# Release notes

Each release gets two artefacts here: the notes themselves (`<version>.md`) and
the media that goes with them (`<version>/`).

Both are written in English, in plain language, whatever the language of the
conversation that produced the change. Sections released before 2.54.0 keep the
language they were written in, and that is fine: they are history.

## The notes

`<version>.md` is the body of the GitHub release, not a copy of the changelog.
The shape that works:

1. A short lead, three or four lines, saying what the release is about. No
   marketing, no "we are excited".
2. One section per headline change, each with a capture and two or three
   sentences: what it does and why it matters for the person running it.
3. A compact list for everything else, and a `Fixed` list that names the
   symptom, not the commit.
4. A `Thanks` section that credits whoever reported or built each thing. People
   answer again when they see their name.

The language rules are the house ones: no em dashes, no semicolons, short
sentences, concrete nouns. If a sentence needs a second read, it is wrong.

## The media

Do not record the screen by hand. The captures are generated from the app, so
they follow the theme, the density and the current UI:

```
make frontend                      # or npm --prefix app run build
npm --prefix app run preview -- --port 4180
python3 scripts/release_media.py --version 2.54.0
```

`scripts/release_media.py` opens the app with a demo session, goes to
`/whatsnew`, and exports each card's demo (`[data-demo-id]`) to a GIF at 10 fps
for four seconds, with a palette that keeps it around 50 KiB. It needs the
Playwright tool from uv and `ffmpeg` in the path. The cards are the animated
demos from `docs/whatsnew.md`, which is why a feature worth a capture also
needs a card: one piece of work feeds both surfaces.

Adding a capture for something that is not a What's new card is a two-step job:
add the card (the animation is the honest way to show an interaction) or, for a
still, take a screenshot with the same Playwright session and save it in
`<version>/`.

## At the cut

The notes use absolute raw links to the tag:

```
https://raw.githubusercontent.com/gnacho/netpulse/v<version>/docs/releases/<version>/<name>.gif
```

They work because the tag points at the release commit, which contains the
media. Until the tag exists they render broken in the repository view, which is
expected: the file is reviewed as a diff, not as a page.

Then:

```
gh release create v2.54.0 --notes-file docs/releases/2.54.0.md
```

Do not use goreleaser's generated notes as the release body. GoReleaser still
writes its changelog from commit messages and that is fine for its own output,
but the release body is the curated one: commits say "stop emitting the cable
twice", the notes say what the operator was seeing.

## Checklist

- [ ] One capture per headline change, generated from the app, not recorded.
- [ ] English, plain, no em dash, no semicolon.
- [ ] Every fixed entry names the symptom, with the issue number.
- [ ] Thanks section with the reporters.
- [ ] `CHANGELOG.md` `[Unreleased]` complete for the same set.
- [ ] Links use the final tag, and the tag exists before the release is published.
