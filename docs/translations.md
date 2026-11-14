# Translating NetPulse

NetPulse has two surfaces with separate string catalogs. You can translate one
of them and leave the other for later: they do not share files.

| Surface | Languages today | Source of truth | What it takes |
|---|---|---|---|
| Landing site, `netpulse.cloudless.club` | Spanish, English, Chinese, Arabic, Hindi, Portuguese, French, Japanese, Russian and German | `web/src/i18n.ts` | Editing TypeScript data, no logic |
| App UI, the panel itself | Spanish and English | `app/public/locales/<code>/translation.json` | A JSON file plus a few code lines |

Open an issue first, with a title like `Translations: Romanian`, and say which
surface you are taking. Two people translating the same language into two
branches is the only way to lose this work.

## Rules that apply to every language

- **Translate meaning, not words.** The English text is direct and free of
  marketing. Keep that tone instead of making it more formal.
- **Do not translate** product names (NetPulse, OpenWrt, GL.iNet, OPNsense,
  RouterOS, UniFi, Bbox, RTLPlayground, Home Assistant, MQTT, SNMP), file
  paths, commands, environment variables, API field names, endpoints, port
  numbers or units (`dBm`, `Mbps`, `KiB`, `°C`).
- **Leave placeholders untouched.** `{n}`, `{count}` and `{{name}}` are
  replaced at runtime. Same name, same place, same surrounding spaces.
- **No em dashes and no en dashes** in any language (U+2014 and U+2013). Use a
  comma, a period or a colon. This is a house rule for the whole project.
- **No semicolons in prose.** Split the sentence instead.
- **One language per pull request.** It keeps review and revert simple.
- **Machine translation on its own is not accepted.** Use it as a draft if you
  want, then read every line as a native or fluent speaker. In the pull request
  say who reviewed it.
- **Partial work is welcome.** The landing falls back to English key by key, so
  a language can land with half the page translated. Say in the pull request
  what you left out.

## Landing site

Files, all of them under `web/`:

1. **`src/i18n.ts`** holds everything.
   - Add the language to `LANGS` with its native name, direction and flag, for
     example `ro: { name: 'Română', dir: 'ltr', flag: '🇷🇴' }`. Use `dir: 'rtl'`
     for right to left languages. This entry is what puts the language in the
     selector of every page.
   - Add a block for the language inside `I18N`, mirroring the sections of
     `I18N.en` (`nav`, `hero`, `featuresPage`, `integrationsPage`, and so on).
     You can translate only the sections you care about.
   - Add the code to the `SecLang` union near the bottom of the file. That union
     is what makes `npm run build` fail when a language is declared but its
     block is missing, with an error that reads `Type 'false' does not satisfy
     the constraint 'true'`. It does not validate individual key names: a typo
     does not break the build, it just leaves the English string on the page,
     so read your page next to the English one.
   - `I18N_FILL` holds top-ups for the languages whose main block covers only
     part of the catalog. If you prefer not to touch an existing block, put
     your strings there.
   - English is the reference catalog. Spanish is checked key by key against
     it, so `npm run build` fails when Spanish misses a key that English has.
     Secondary languages are free to be partial.
2. **`build-langs.mjs`** prerenders one route per language. Add the code to
   `LANGS` and to `OG_LOCALE`, both at the top of the file.
3. **The four pages**, `index.html`, `features.html`, `home-assistant.html`
   and `integrations.html`, carry the `hreflang` alternates in their head. Add
   one `<link rel="alternate" hreflang="ro" href="https://netpulse.cloudless.club/ro/..." />`
   line per page, next to the existing ones.
4. **`sitemap.xml`** lists four URLs per language. Add yours.
5. **`tests/prerender-csp.mjs`** has an `expectedH1` map with the first heading
   of the integrations page in every language. Add the code and your
   translation. The test also asserts that the integrations table keeps its
   seven rows, so do not delete rows to make a translation fit.

Never edit `i18n.js` or `app.js` in `web/`. They are built from `src/` with
`npm run build`.

### Check it

```bash
cd web
npm install
npx playwright install chromium
npm run test:langs
```

That compiles the catalog, prerenders every route (four pages times the number
of languages) and runs the localization and CSP assertions. After a successful
run, open `web/langs/<code>/integrations/index.html` to read your text in
context. `web/langs/` is generated and git ignored: do not commit it.

If a key is missing in your language, the page shows the English one. That is
the intended fallback, not an error.

## App UI

The panel loads its strings at runtime from `app/public/locales/<code>/translation.json`.
`en` is the reference: copy that file, translate it, and keep the same nesting
and the same keys. The file has 33 top level sections and around 150 KB, so
expect a few evenings of work.

Adding a language also needs a few lines outside the JSON, which is why we ask
you to open the issue first:

- `app/src/i18n.ts`: `supportedLngs`, the `applyDocLang` mapping (it switches
  `document.documentElement.lang` between `en` and `es`) and `numLocale()`.
- `app/src/pages/Settings.tsx`: the language selector, both its options and the
  `'auto' | 'es' | 'en'` union used by the state, plus the date and number
  formatting that currently hardcodes `en-GB` and `es-ES`.

Send the JSON and we will do the plumbing with you, or send both in one pull
request and we will review it together.

### Check it

```bash
cd app
npm install
npm run build
npm run lint
```

Then run the panel in development (`npm run dev`), switch to your language in
Settings and walk the menus that you translated. A key you have not translated
yet shows the English string. When both catalogs miss it, the key path itself
is rendered, which is how you spot a typo.

## What we check before merging

| Check | How |
|---|---|
| Placeholders intact | Compare against the English string, same tokens and order |
| No em dashes, no semicolons in prose | `grep -nP "\x{2014}\|\x{2013}" <file>` and a read through |
| Product names, paths and units untouched | Read through |
| Landing: the language appears in the selector and in the prerendered routes | `npm run test:langs` |
| Landing: `hreflang` and sitemap updated | Diff of the four HTML files and `sitemap.xml` |
| App: JSON parses and the build passes | `npm run build` and `npm run lint` |
| A fluent speaker reviewed it | Stated in the pull request |

## Keeping a language alive

The landing grows with each release. When a new English string lands, a
language that has not translated it shows English in that spot instead of
breaking the page, so nothing blocks a release. If you want to keep a language
current, say so in your issue and we will ask you before releases that touch
many strings. There is no automatic translation on top of your work.
