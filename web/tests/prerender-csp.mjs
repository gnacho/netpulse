import assert from 'node:assert/strict'
import http from 'node:http'
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const expectedH1 = {
  es: 'Tus equipos de red, en una sola vista',
  en: 'Your network devices, in one view',
  zh: '在一个视图中查看你的网络设备',
  ar: 'أجهزة شبكتك في عرض واحد',
  hi: 'आपके नेटवर्क डिवाइस, एक ही व्यू में',
  pt: 'Os seus equipamentos de rede numa só vista',
  fr: 'Vos équipements réseau dans une seule vue',
  ja: 'ネットワーク機器を一つのビューに',
  ru: 'Все сетевые устройства в одном окне',
  de: 'Deine Netzwerkgeräte in einer Ansicht',
}
const mime = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css',
  '.js': 'text/javascript',
  '.png': 'image/png',
  '.webp': 'image/webp',
  '.svg': 'image/svg+xml',
  '.woff2': 'font/woff2',
  '.json': 'application/json',
}

const server = http.createServer(async (req, res) => {
  try {
    let pathname = decodeURIComponent(new URL(req.url, 'http://local').pathname)
    let root = web
    const localized = pathname.match(/^\/(es|en|zh|ar|hi|pt|fr|ja|ru|de)(?:\/(.*))?$/)
    if (localized) {
      root = path.join(web, 'langs', localized[1])
      pathname = `/${localized[2] || ''}`
    }
    if (pathname === '/') pathname = '/index.html'
    else if (pathname.endsWith('/')) pathname += 'index.html'
    else if (!path.extname(pathname)) pathname += '.html'
    const file = path.resolve(root, `.${pathname}`)
    if (!file.startsWith(`${path.resolve(root)}${path.sep}`)) throw new Error('unsafe path')
    const body = await readFile(file)
    res.writeHead(200, {
      'content-type': mime[path.extname(file)] || 'application/octet-stream',
      'content-security-policy': "default-src 'self'; script-src 'self' https://stats.netpulse.cloudless.club https://stat.domatix.com; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; connect-src 'self' https://api.github.com",
    })
    res.end(body)
  } catch {
    res.writeHead(404)
    res.end('not found')
  }
})

await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const base = `http://127.0.0.1:${server.address().port}`
const browser = await chromium.launch({ headless: true })

function watchErrors(page) {
  const errors = []
  page.on('pageerror', error => errors.push(`pageerror: ${error.message}`))
  page.on('console', message => {
    const text = message.text()
    const mockedAnalyticsIntegrity = text.includes('integrity') &&
      (text.includes('stat.domatix.com') || text.includes('stats.netpulse.cloudless.club'))
    if (message.type() === 'error' && !mockedAnalyticsIntegrity) errors.push(`console: ${text}`)
  })
  return errors
}

async function assertNoInlineScripts(page) {
  const count = await page.locator('script:not([src]):not([type="application/ld+json"])').count()
  assert.equal(count, 0, 'the prerender must not depend on executable inline scripts')
}

try {
  const context = await browser.newContext({ locale: 'es-ES' })
  await context.addInitScript(() => localStorage.setItem('netpulse-web-lang', 'es'))
  await context.route('https://stat.domatix.com/**', route => route.fulfill({ status: 200, contentType: 'text/javascript', body: '' }))
  await context.route('https://stats.netpulse.cloudless.club/**', route => route.fulfill({ status: 200, contentType: 'text/javascript', body: '' }))
  await context.route('https://api.github.com/**', route => route.fulfill({ status: 200, contentType: 'application/json', body: '{}' }))

  for (const [lang, h1] of Object.entries(expectedH1)) {
    const page = await context.newPage()
    const errors = watchErrors(page)
    const response = await page.goto(`${base}/${lang}/integrations/`, { waitUntil: 'networkidle' })
    assert.equal(response.status(), 200)
    assert.equal(await page.locator('html').getAttribute('lang'), lang)
    assert.equal((await page.locator('h1').innerText()).trim(), h1)
    assert.equal(await page.locator('#hardware tbody tr').count(), 6)
    await assertNoInlineScripts(page)
    assert.deepEqual(errors, [], `${lang}: browser errors`)
    await page.close()
  }

  const page = await context.newPage()
  const errors = watchErrors(page)
  await page.goto(`${base}/en/integrations/`, { waitUntil: 'networkidle' })
  await page.locator('#langSelect').selectOption('de')
  await page.waitForURL('**/de/integrations/')
  await page.waitForLoadState('networkidle')
  assert.equal(await page.locator('html').getAttribute('lang'), 'de')
  assert.equal((await page.locator('h1').innerText()).trim(), expectedH1.de)
  await assertNoInlineScripts(page)
  assert.deepEqual(errors, [], 'language selector: browser errors')
  await page.close()

  const source = await context.newPage()
  const sourceErrors = watchErrors(source)
  await source.goto(`${base}/integrations`, { waitUntil: 'networkidle' })
  await source.locator('#langSelect').selectOption('en')
  assert.equal(new URL(source.url()).pathname, '/integrations')
  assert.equal((await source.locator('h1').innerText()).trim(), expectedH1.en)
  assert.deepEqual(sourceErrors, [], 'source page: browser errors')
  await source.close()
  await context.close()

  console.log('OK: 10 localized routes, clean-URL language selection and source-page selection under CSP')
} finally {
  await browser.close()
  server.close()
}
