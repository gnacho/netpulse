// build-langs.mjs — genera las rutas prerenderizadas por idioma de la landing
// (issue #842). Carga cada página con el runtime real (Playwright, ?hl=<lang>),
// aplica las traducciones igual que en el navegador, y congela el HTML con
// lang/canonical/hreflang correctos y los enlaces internos reescritos a la
// ruta del idioma. El selector de idioma de las páginas generadas navega a la
// ruta del idioma elegido en vez de recargar con ?hl=.
//
// Uso (desde web/):  npm install && npm run build:langs
// Salida:            web/langs/<lang>/{index,features,home-assistant}.html
// Deploy:            subir web/langs/* a la raíz del docroot (/<lang>/...).
import { chromium } from 'playwright'
import { mkdir, writeFile, readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const WEB = path.dirname(fileURLToPath(import.meta.url))
const OUT = path.join(WEB, 'langs')
const BASE = 'https://netpulse.cloudless.club'
const LANGS = ['es', 'en', 'zh', 'ar', 'hi', 'pt', 'fr', 'ja', 'ru', 'de']
// slug = ruta limpia del idioma (con / final); esSlug = ruta raíz en nginx
// (sin / final: /features tiene location exacta propia); outDir = destino.
const PAGES = [
  { file: 'index.html', slug: '', esSlug: '', outDir: '' },
  { file: 'features.html', slug: 'features/', esSlug: 'features', outDir: 'features' },
  { file: 'home-assistant.html', slug: 'home-assistant/', esSlug: 'home-assistant', outDir: 'home-assistant' },
]

// URL canónica absoluta de una página de idioma. El español es el x-default:
// vive en la raíz (/) y /es/ canonicaliza a ella para no duplicar contenido.
const urlFor = (lang, slug, esSlug) =>
  lang === 'es' ? `${BASE}/${esSlug}` : `${BASE}/${lang}/${slug}`

// Los src/href relativos (app.js, assets, styles.css, i18n.js) se absolutizan
// a la raíz: en /<lang>/ un "app.js" relativo apuntaría a /<lang>/app.js (404).
function fixAssets(html) {
  return html
    .replace(/src="(?!\/|https?:|data:)([^"]*)"/g, 'src="/$1"')
    .replace(/href="(?!\/|https?:|data:|#)([^"]*)"/g, 'href="/$1"')
}

// Sustituye el bloque canonical/hreflang por el de la URL limpia del idioma.
function fixHead(html, lang, slug, esSlug) {
  const canonical = `<link rel="canonical" href="${urlFor(lang, slug, esSlug)}" />`
  const alternates = [
    ...LANGS.map(
      (l) => `<link rel="alternate" hreflang="${l}" href="${urlFor(l, slug, esSlug)}" />`,
    ),
    `<link rel="alternate" hreflang="x-default" href="${BASE}/${esSlug}" />`,
  ].join('\n  ')
  const block = new RegExp(
    '<link rel="canonical"[^>]*>\\s*(<link rel="alternate"[^>]*>\\s*)+',
    'g',
  )
  if (!block.test(html)) throw new Error('bloque canonical/hreflang no encontrado')
  html = html.replace(block, `${canonical}\n  ${alternates}\n  `)
  html = html.replace(
    /<meta property="og:url" content="[^"]*"\s*\/?>/,
    `<meta property="og:url" content="${urlFor(lang, slug, esSlug)}" />`,
  )
  return html
}

// Enlaces internos de página apuntan a la ruta del mismo idioma. Solo se
// tocan las tres páginas conocidas; assets, styles.css, i18n.js, og y demás
// quedan en la raíz compartida.
function fixLinks(html, lang) {
  return html
    .replace(/href="\/home-assistant/g, `href="/${lang}/home-assistant`)
    .replace(/href="\/features/g, `href="/${lang}/features`)
    .replace(/href="\/"\/?/g, `href="/${lang}/"`)
    .replace(/href="\/#faq/g, `href="/${lang}/#faq`)
    .replace(/href="\/#free/g, `href="/${lang}/#free`)
    .replace(/href="\/#install/g, `href="/${lang}/#install`)
    .replace(new RegExp(`href="/${lang}/features"`, 'g'), `href="/${lang}/features/"`)
    .replace(new RegExp(`href="/${lang}/home-assistant"`, 'g'), `href="/${lang}/home-assistant/"`)
}

async function main() {
  const browser = await chromium.launch()
  const errors = []
  for (const lang of LANGS) {
    for (const page of PAGES) {
      const ctx = await browser.newContext()
      const pg = await ctx.newPage()
      pg.on('pageerror', (e) => errors.push(`${lang}/${page.file}: ${e}`))
      const fileUrl = `file://${path.join(WEB, page.file)}?hl=${lang}`
      await pg.goto(fileUrl, { waitUntil: 'networkidle' })
      await pg.waitForTimeout(700) // renders dinámicos (ticker, shots, contadores)
      let html = await pg.evaluate(() => '<!DOCTYPE html>\n' + document.documentElement.outerHTML)
      await ctx.close()

      if (!html.includes(`lang="${lang}"`)) {
        throw new Error(`lang attr no aplicado en ${lang}/${page.file}`)
      }
      html = fixHead(html, lang, page.slug, page.esSlug)
      html = fixLinks(html, lang)
      html = html.replace(`/${lang}/features#`, `/${lang}/features/#`)
      html = fixAssets(html)
      // La página prerenderizada fija su idioma: detectLang() respeta la clave
      // localStorage antes que navigator, sin tocar el runtime.
      html = html.replace(
        '<script src="/i18n.js',
        `<script>try{localStorage.setItem('netpulse-web-lang','${lang}')}catch{}</` + `script>\n  <script src="/i18n.js`,
      )
      // El selector de idioma navega a la ruta limpia del idioma elegido.
      const slugEsc = page.slug
      html = html.replace(
        '</body>',
        `<script>
document.getElementById('langSelect')?.addEventListener('change',(e)=>{
  const v=e.target.value; if(v) location.href='/'+v+'/${slugEsc}'
})
</script>
</body>`,
      )
      const outDir = path.join(OUT, lang, page.outDir)
      await mkdir(outDir, { recursive: true })
      await writeFile(path.join(outDir, 'index.html'), html)
      process.stdout.write(`${lang}/${page.file} `)
    }
    process.stdout.write('\n')
  }
  await browser.close()
  if (errors.length) {
    console.error('ERRORES DE PÁGINA:')
    errors.forEach((e) => console.error(' -', e))
    process.exit(1)
  }
  console.log(`OK: ${LANGS.length * PAGES.length} páginas en ${OUT}`)
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})
