// build-changelog.mjs — genera web/changelog.html con todas las releases.
//
// Fuente: docs/releases/<version>.md (curadas) y docs/releases/archive/*.md
// (histórico, cuerpo verbatim). Los cuerpos están en inglés, que es la norma
// de las notas desde 2.54.0; la página y su navegación van traducidas por el
// runtime de la landing como cualquier otra.
//
// Uso (desde web/):  node build-changelog.mjs
// Después:           npm run build:langs   (prerender es/en/zh/ru)
import { copyFile, mkdir, readFile, writeFile, readdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const WEB = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.dirname(WEB)
const ARCHIVE = path.join(ROOT, 'docs', 'releases', 'archive')
const CURATED = path.join(ROOT, 'docs', 'releases')
const CARD_LANGS = ['es', 'en', 'zh', 'ru']

const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')

/** Markdown mínimo para el subconjunto que usan las notas. */
function md(src) {
  const lines = src.replace(/\r\n/g, '\n').split('\n')
  const out = []
  let list = null
  let code = false
  const flush = () => {
    if (list) {
      out.push(`<${list.tag}>${list.items.join('')}</${list.tag}>`)
      list = null
    }
  }
  const IMG = /^!\[([^\]]*)\]\(([^)]+)\)$/
  // Las capturas del repo se sirven desde el propio sitio: la CSP del dominio
  // es img-src 'self' y un GIF en raw.githubusercontent.com no carga.
  const localMedia = (src) => {
    const m = src.match(
      /^https:\/\/raw\.githubusercontent\.com\/gnacho\/netpulse\/v[^/]+\/docs\/releases\/([^/]+\/[^/]+\.(?:gif|png|jpe?g|webp))$/,
    )
    return m ? `/assets/releases/${m[1]}` : src
  }
  const img = (alt, src) => `<img src="${localMedia(src)}" alt="${alt}" loading="lazy" decoding="async" />`
  const inline = (t) =>
    esc(t)
      .replace(/`([^`]+)`/g, '<code>$1</code>')
      .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
      // Las capturas de las notas son la mitad del contenido: antes se
      // sustituían por su texto alternativo y la página las perdía.
      .replace(/!\[([^\]]*)\]\(([^)]+)\)/g, (_m, alt, src) => img(alt, src))
      .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2">$1</a>')

  for (const raw of lines) {
    const line = raw.replace(/\s+$/, '')
    if (line.startsWith('```')) {
      flush()
      out.push(code ? '</pre>' : '<pre>')
      code = !code
      continue
    }
    if (code) {
      out.push(esc(raw))
      continue
    }
    if (!line.trim()) {
      flush()
      continue
    }
    const h = line.match(/^(#{1,4})\s+(.*)$/)
    if (h) {
      flush()
      const level = Math.min(h[1].length + 2, 6)
      out.push(`<h${level}>${inline(h[2])}</h${level}>`)
      continue
    }
    if (/^---+$/.test(line.trim())) {
      flush()
      out.push('<hr />')
      continue
    }
    const ul = line.match(/^\s*[-*]\s+(.*)$/)
    const ol = line.match(/^\s*\d+\.\s+(.*)$/)
    if (ul || ol) {
      const tag = ul ? 'ul' : 'ol'
      if (!list || list.tag !== tag) {
        flush()
        list = { tag, items: [] }
      }
      list.items.push(`<li>${inline((ul ?? ol)[1])}</li>`)
      continue
    }
    flush()
    const onlyImg = line.trim().match(IMG)
    if (onlyImg) {
      out.push(`<p class="changelog-figure">${img(onlyImg[1], onlyImg[2])}</p>`)
      continue
    }
    out.push(`<p>${inline(line)}</p>`)
  }
  flush()
  if (code) out.push('</pre>')
  return out.join('\n')
}

async function load() {
  const releases = []
  const seen = new Set()

  // Curadas primero (las que llevan notas nuevas): <version>.md sin la v.
  for (const f of await readdir(CURATED)) {
    if (!/^\d+\.\d+\.\d+\.md$/.test(f)) continue
    const version = f.replace(/\.md$/, '')
    const body = await readFile(path.join(CURATED, f), 'utf8')
    releases.push({ tag: `v${version}`, version, body, curated: true })
    seen.add(version)
  }

  // Histórico: nombres vX.Y.Z.md y go-latest/dist-latest.
  for (const f of await readdir(ARCHIVE)) {
    if (!f.endsWith('.md') || f === 'INDEX.md') continue
    const tag = f.replace(/\.md$/, '')
    const version = tag.replace(/^v/, '').replace(/-\d+$/, '')
    if (seen.has(version)) continue
    const body = await readFile(path.join(ARCHIVE, f), 'utf8')
    releases.push({ tag, version, body, curated: false })
  }

  const key = (v) => v.split('.').map((n) => Number.parseInt(n, 10) || 0)
  releases.sort((a, b) => {
    const ka = key(a.version)
    const kb = key(b.version)
    for (let i = 0; i < Math.max(ka.length, kb.length); i++) {
      const d = (kb[i] ?? 0) - (ka[i] ?? 0)
      if (d !== 0) return d
    }
    return 0
  })
  return releases
}

const list = await load()
const template = await readFile(path.join(WEB, 'changelog.template.html'), 'utf8')

const html = list
  .map((r, i) => {
    const open = i === 0 ? ' open' : ''
    return `<details class="changelog-entry"${open}>
  <summary><span class="changelog-tag">${esc(r.tag)}</span><span class="changelog-arrow" aria-hidden="true">▾</span></summary>
  <div class="changelog-body">
${md(r.body)}
  </div>
</details>`
  })
  .join('\n')

const out = template
  .replace('<!--RELEASES-->', html)
  .replace('<html lang="es">', `<html lang="es" data-changelog-langs="${CARD_LANGS.join(',')}">`)

await writeFile(path.join(WEB, 'changelog.html'), out)
console.log(`changelog.html: ${list.length} releases, ${(out.length / 1024).toFixed(0)} KiB`)

// Las capturas de las notas viven en docs/releases/<version>/ y la página las
// pide en /assets/releases/<version>/<file>. Se copian aquí (#2207): si el
// media no viaja con la página, el deploy publica imágenes 404 y el prerender
// no lo nota porque solo necesita el HTML.
const MEDIA = /\.(gif|png|jpe?g|webp)$/i
let copied = 0
for (const version of list.map((r) => r.version)) {
  const dir = path.join(CURATED, version)
  let entries
  try {
    entries = await readdir(dir, { withFileTypes: true })
  } catch {
    continue
  }
  const media = entries.filter((e) => e.isFile() && MEDIA.test(e.name))
  if (!media.length) continue
  const dst = path.join(WEB, 'assets', 'releases', version)
  await mkdir(dst, { recursive: true })
  for (const f of media) {
    await copyFile(path.join(dir, f.name), path.join(dst, f.name))
    copied += 1
  }
}
console.log(`assets/releases: ${copied} capture file(s)`)

