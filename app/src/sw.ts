/// <reference lib="webworker" />
/**
 * NetPulse — Service Worker propio (vite-plugin-pwa `injectManifest`).
 *
 * ¿Por qué injectManifest y no generateSW + importScripts?
 * generateSW no admite handlers custom (Web Push exige `push` y
 * `notificationclick`). La alternativa "mínima" (importScripts de un
 * push-sw.js estático en public/) dejaba la lógica push fuera del build TS
 * (sin type-check, sin minificar, archivo separado que versionar a mano) y
 * además ese archivo caía dentro del propio glob de precache. Con
 * injectManifest el SW es un único bundle TS versionado y el precache
 * app-shell se inyecta en self.__WB_MANIFEST con los mismos globPatterns
 * de siempre (16 entradas).
 */
import { clientsClaim } from 'workbox-core'
import { cleanupOutdatedCaches, createHandlerBoundToURL, precacheAndRoute } from 'workbox-precaching'
import { NavigationRoute, registerRoute } from 'workbox-routing'
import { localizePush, pushLang, type PushCatalog } from '@/lib/push-i18n'

declare let self: ServiceWorkerGlobalScope

// registerType: 'autoUpdate' — el SW nuevo toma el control sin esperar al
// cierre de pestañas (equivale al comportamiento anterior con generateSW).
self.skipWaiting()
clientsClaim()

// Punto de inyección de workbox-build: el literal `self.__WB_MANIFEST` se
// sustituye por el manifiesto de precache en el build (no refactorizar).
precacheAndRoute(self.__WB_MANIFEST)
cleanupOutdatedCaches()

// App-shell offline para navegaciones (equivale a `navigateFallback:
// 'index.html'` de generateSW). En dev NO se registra: el SW de desarrollo
// no precachea index.html y esta ruta serviría un HTML congelado.
if (!import.meta.env.DEV) {
  registerRoute(new NavigationRoute(createHandlerBoundToURL('/index.html')))
}

// ---------------------------------------------------------------------------
// Web Push (SPEC-PUSH §2)
// Payload: {title, body, type, vars, category, severity, url:"/alerts", tag}
// ---------------------------------------------------------------------------

interface PushPayload {
  title?: string
  body?: string
  /** slug estable del tipo de alerta (#689); permite traducir en el SW */
  type?: string
  /** variables de interpolación del tipo (#689) */
  vars?: Record<string, string>
  category?: string
  severity?: string
  url?: string
  tag?: string
  hint?: string
}

const I18N_CACHE = 'netpulse-i18n-v1'

/**
 * Catálogo de traducciones del idioma dado, cache-first (#689). Los locales
 * viven en public/ y se sirven como estáticos; el SW los cachea la primera vez
 * para no depender de la red en cada push.
 */
async function pushCatalog(lang: 'es' | 'en'): Promise<PushCatalog | null> {
  const url = `/locales/${lang}/translation.json`
  try {
    const cache = await caches.open(I18N_CACHE)
    let res = await cache.match(url)
    if (!res) {
      res = await fetch(url)
      if (res.ok) await cache.put(url, res.clone())
    }
    if (!res || !res.ok) return null
    return (await res.json()) as PushCatalog
  } catch {
    return null
  }
}

self.addEventListener('push', (event: PushEvent) => {
  event.waitUntil(onPush(event))
})

async function onPush(event: PushEvent): Promise<void> {
  let payload: PushPayload = {}
  try {
    if (event.data) payload = event.data.json() as PushPayload
  } catch {
    // Payload no-JSON: notificación genérica (nunca romper el handler)
    payload = {}
  }
  let title = payload.title || 'NetPulse'
  let body = [payload.body, payload.hint].filter(Boolean).join('\n')
  // #689: si el evento trae el slug del tipo, traduce al idioma del dispositivo
  // (catálogo de la app); si falla, se conservan los literales del server.
  if (payload.type) {
    const catalog = await pushCatalog(pushLang(navigator.language))
    const loc = localizePush(catalog, payload.type, payload.vars, {
      title: payload.title,
      body: payload.body,
      hint: payload.hint,
    })
    title = loc.title
    body = [loc.body, loc.hint].filter(Boolean).join('\n')
  }
  await self.registration.showNotification(title, {
    body,
    icon: '/icon-192.png',
    badge: '/icon-192.png',
    // tag = dedup nativo del navegador (SPEC: tag = id del evento)
    tag: payload.tag ?? 'netpulse-alert',
    data: { url: payload.url ?? '/alerts' },
  })
}

self.addEventListener('notificationclick', (event: NotificationEvent) => {
  event.notification.close()
  const url = (event.notification.data as { url?: string } | undefined)?.url ?? '/alerts'
  event.waitUntil(focusOrOpen(url))
})

/** Foco a una ventana ya abierta de la app (navegándola a `url`) o abre una nueva. */
async function focusOrOpen(url: string): Promise<void> {
  const target = new URL(url, self.location.origin).pathname
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  for (const client of windows) {
    if (new URL(client.url).origin !== self.location.origin) continue
    await client.focus()
    if (new URL(client.url).pathname !== target) {
      await client.navigate(target)
    }
    return
  }
  await self.clients.openWindow(target)
}
